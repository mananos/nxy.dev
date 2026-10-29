package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

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
	return s.CreateFeature(ctx, f)
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
		Store: st, Repos: repoKeys{}, Legacy: legacyPlugin{}, Viewer: planViewer{}, Exe: exePath(),
	}, st
}
