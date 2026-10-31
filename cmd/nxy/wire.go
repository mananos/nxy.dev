package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mananos/nxy.dev/internal/app"
	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/filter"
	"github.com/mananos/nxy.dev/internal/host/claudecode"
	"github.com/mananos/nxy.dev/internal/plan"
	"github.com/mananos/nxy.dev/internal/platform"
	"github.com/mananos/nxy.dev/internal/store/sqlite"
	"github.com/mananos/nxy.dev/internal/tools"
)

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

type filterConfig struct{}

func (filterConfig) Filter(projectDir string) filter.Settings {
	return platform.LoadFilterSettings(projectDir, os.Getenv)
}

type permissionRules struct{}

func (permissionRules) Rules(projectDir string) filter.Rules {
	return claudecode.LoadPermissionRules(claudecode.SettingsFiles(homeDir(), projectDir))
}

type filterEngine struct{ e tools.Engine }

func (f filterEngine) Info(s filter.Settings, projectDir string) filter.EngineInfo {
	return f.e.Info(s, func() bool {
		return filter.RtkHookIn(claudecode.PreToolUseHookCommands(claudecode.SettingsFiles(homeDir(), projectDir)))
	})
}

func (f filterEngine) Rewrite(info filter.EngineInfo, cmd string) (filter.RewriteResult, error) {
	return f.e.Rewrite(info, cmd)
}

// lazyStore opens the database on the first call that needs it, so hooks that never
// touch features (the Bash filter) do not pay for it.
type lazyStore struct {
	mu sync.Mutex
	s  *sqlite.Store
}

func (l *lazyStore) open(ctx context.Context) (*sqlite.Store, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.s != nil {
		return l.s, nil
	}
	path, err := platform.DBPath()
	if err != nil {
		return nil, err
	}
	s, err := sqlite.Open(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	l.s = s
	return s, nil
}

func (l *lazyStore) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.s == nil {
		return nil
	}
	err := l.s.Close()
	l.s = nil
	return err
}

func (l *lazyStore) CreateFeature(ctx context.Context, f domain.Feature) (string, error) {
	s, err := l.open(ctx)
	if err != nil {
		return "", err
	}
	id, err := s.CreateFeature(ctx, f)
	if errors.Is(err, sqlite.ErrActiveFeature) {
		return "", app.ErrActiveFeature
	}
	return id, err
}

func (l *lazyStore) Feature(ctx context.Context, id string) (domain.Feature, error) {
	s, err := l.open(ctx)
	if err != nil {
		return domain.Feature{}, err
	}
	return s.Feature(ctx, id)
}

func (l *lazyStore) ActiveFeature(ctx context.Context, repo string) (domain.Feature, bool, error) {
	s, err := l.open(ctx)
	if err != nil {
		return domain.Feature{}, false, err
	}
	return s.ActiveFeature(ctx, repo)
}

func (l *lazyStore) Transition(ctx context.Context, id string, fn func(domain.Feature) (domain.Feature, error)) error {
	s, err := l.open(ctx)
	if err != nil {
		return err
	}
	return s.Transition(ctx, id, fn)
}

type repoKeys struct{}

func (repoKeys) Key(dir string) string { return platform.RepoKey(dir) }

type legacyPlugin struct{}

func (legacyPlugin) Active(projectDir string) bool {
	return claudecode.LegacyActive(homeDir(), projectDir)
}

// planViewer writes the full plan to <UserDir>/plans/<id>.md and opens it.
type planViewer struct{}

func planPath(id string) (string, error) {
	u, err := platform.UserDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(u, "plans", id+".md"), nil
}

func (planViewer) Show(_ context.Context, f domain.Feature) (string, error) {
	p, err := planPath(f.ID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, []byte(plan.Markdown(f)), 0o644); err != nil {
		return "", err
	}
	if err := platform.OpenFile(p); err != nil {
		return p, err
	}
	return p, nil
}

func (planViewer) Discard(id string) error {
	p, err := planPath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

const (
	// acceptTimeout stays under the 10 minute maximum of a host Bash call, so nxy records a
	// failure instead of being killed mid-run.
	acceptTimeout = 9*time.Minute + 30*time.Second
	acceptKeep    = 64 << 10
)

type shellRunner struct{}

func (shellRunner) Run(ctx context.Context, dir, command string) (app.ShellResult, error) {
	ctx, cancel := context.WithTimeout(ctx, acceptTimeout)
	defer cancel()
	exit, out, err := platform.RunShell(ctx, dir, command, acceptKeep)
	if errors.Is(err, context.DeadlineExceeded) {
		out = strings.TrimRight(out, "\r\n") + fmt.Sprintf("\nnxy: Accept timed out after %s and was killed", acceptTimeout)
		return app.ShellResult{Exit: -1, Output: out, TimedOut: true}, nil
	}
	return app.ShellResult{Exit: exit, Output: out}, err
}

type fileProbe struct{}

func (fileProbe) NativePath(p string) string { return platform.NativePath(p) }
func (fileProbe) IsDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func exePath() string {
	e, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.ToSlash(e)
}

// newHooksStore also returns the lazy store so the caller can close it.
func newHooksStore() (app.Hooks, *lazyStore) {
	st := &lazyStore{}
	return app.Hooks{
		Config: filterConfig{}, Engine: filterEngine{}, Permissions: permissionRules{},
		Store: st, Repos: repoKeys{}, Legacy: legacyPlugin{}, Viewer: planViewer{}, Shell: shellRunner{}, Files: fileProbe{}, Exe: exePath(),
	}, st
}
