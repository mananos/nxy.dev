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

	"github.com/mananos/nxy.dev/internal/affected"
	"github.com/mananos/nxy.dev/internal/app"
	"github.com/mananos/nxy.dev/internal/configedit"
	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/filter"
	"github.com/mananos/nxy.dev/internal/host/claudecode"
	"github.com/mananos/nxy.dev/internal/memory"
	"github.com/mananos/nxy.dev/internal/plan"
	"github.com/mananos/nxy.dev/internal/platform"
	"github.com/mananos/nxy.dev/internal/review"
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

// roleConfig is the role table of the merged config (roles.<role>.model / effort).
type roleConfig struct{}

func (roleConfig) Role(projectDir, role string) (string, string) {
	return configedit.RoleOf(platform.LoadConfig(projectDir, os.Getenv), role)
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

func (l *lazyStore) LatestFeature(ctx context.Context, repo string) (domain.Feature, bool, error) {
	s, err := l.open(ctx)
	if err != nil {
		return domain.Feature{}, false, err
	}
	return s.LatestFeature(ctx, repo)
}

func (l *lazyStore) RecordPicks(ctx context.Context, featureID, repo string, picks []domain.Pick) error {
	s, err := l.open(ctx)
	if err != nil {
		return err
	}
	return s.RecordPicks(ctx, featureID, repo, picks)
}

var _ app.FlowMemory = (*lazyStore)(nil)

func (l *lazyStore) Rules(ctx context.Context, repo string) ([]memory.Rule, error) {
	s, err := l.open(ctx)
	if err != nil {
		return nil, err
	}
	return s.Rules(ctx, repo)
}

func (l *lazyStore) Related(ctx context.Context, repo string, paths []string) ([]memory.ClosedFeature, []memory.Learning, error) {
	s, err := l.open(ctx)
	if err != nil {
		return nil, nil, err
	}
	return s.Related(ctx, repo, paths)
}

func (l *lazyStore) AddLearning(ctx context.Context, x memory.Learning) (memory.Learning, error) {
	s, err := l.open(ctx)
	if err != nil {
		return memory.Learning{}, err
	}
	return s.AddLearning(ctx, x)
}

func (l *lazyStore) AddRule(ctx context.Context, r memory.Rule) (memory.Rule, error) {
	s, err := l.open(ctx)
	if err != nil {
		return memory.Rule{}, err
	}
	return s.AddRule(ctx, r)
}

func (l *lazyStore) SaveClosedFeature(ctx context.Context, f memory.ClosedFeature) (memory.ClosedFeature, error) {
	s, err := l.open(ctx)
	if err != nil {
		return memory.ClosedFeature{}, err
	}
	return s.SaveClosedFeature(ctx, f)
}

func (l *lazyStore) CountDeliveries(ctx context.Context, ids []string) error {
	s, err := l.open(ctx)
	if err != nil {
		return err
	}
	return s.CountDeliveries(ctx, ids)
}

func (l *lazyStore) Cite(ctx context.Context, repo string, shorts []string, at int64) ([]string, error) {
	s, err := l.open(ctx)
	if err != nil {
		return nil, err
	}
	return s.Cite(ctx, repo, shorts, at)
}

func (l *lazyStore) MarkObsolete(ctx context.Context, repo string, refs []string, at int64) error {
	s, err := l.open(ctx)
	if err != nil {
		return err
	}
	return s.MarkObsolete(ctx, repo, refs, at)
}

func (l *lazyStore) SimilarRules(ctx context.Context, repo, text string, limit int) ([]memory.Rule, error) {
	s, err := l.open(ctx)
	if err != nil {
		return nil, err
	}
	return s.SimilarRules(ctx, repo, text, limit)
}

func (l *lazyStore) RecordEscape(ctx context.Context, featureID, repo, lens, text string) error {
	s, err := l.open(ctx)
	if err != nil {
		return err
	}
	return s.RecordEscape(ctx, featureID, repo, lens, text)
}

func (l *lazyStore) ReviewStats(ctx context.Context, repo string, since int64) ([]domain.LensStat, error) {
	s, err := l.open(ctx)
	if err != nil {
		return nil, err
	}
	return s.ReviewStats(ctx, repo, since)
}

func (l *lazyStore) Notes(ctx context.Context, folder string, foldCase bool, now time.Time) ([]memory.Note, error) {
	s, err := l.open(ctx)
	if err != nil {
		return nil, err
	}
	return s.Notes(ctx, folder, foldCase, now)
}

func (l *lazyStore) Note(ctx context.Context, ref string) (memory.Note, error) {
	s, err := l.open(ctx)
	if err != nil {
		return memory.Note{}, err
	}
	return s.Note(ctx, ref)
}

func (l *lazyStore) SaveNote(ctx context.Context, n memory.Note) (memory.Note, error) {
	s, err := l.open(ctx)
	if err != nil {
		return memory.Note{}, err
	}
	return s.SaveNote(ctx, n)
}

func (l *lazyStore) SweepNotes(ctx context.Context, now time.Time) (int, error) {
	s, err := l.open(ctx)
	if err != nil {
		return 0, err
	}
	return s.SweepNotes(ctx, now)
}

func (l *lazyStore) DeleteNotesOfFeature(ctx context.Context, featureID string) (int, error) {
	s, err := l.open(ctx)
	if err != nil {
		return 0, err
	}
	return s.DeleteNotesOfFeature(ctx, featureID)
}

func (l *lazyStore) DeleteNotesUnder(ctx context.Context, folder string, foldCase bool) (int, error) {
	s, err := l.open(ctx)
	if err != nil {
		return 0, err
	}
	return s.DeleteNotesUnder(ctx, folder, foldCase)
}

var _ app.Sessions = (*lazyStore)(nil)

func toState(x sqlite.Session) app.SessionState { return app.SessionState(x) }

func (l *lazyStore) StartSession(ctx context.Context, x app.SessionState) error {
	s, err := l.open(ctx)
	if err != nil {
		return err
	}
	return s.StartSession(ctx, sqlite.Session(x))
}

func (l *lazyStore) Session(ctx context.Context, id string) (app.SessionState, bool, error) {
	s, err := l.open(ctx)
	if err != nil {
		return app.SessionState{}, false, err
	}
	x, ok, err := s.Session(ctx, id)
	return toState(x), ok, err
}

func (l *lazyStore) ConsumeNotesPending(ctx context.Context, id string) (bool, error) {
	s, err := l.open(ctx)
	if err != nil {
		return false, err
	}
	return s.ConsumeNotesPending(ctx, id)
}

func (l *lazyStore) ChainNote(ctx context.Context, id, noteID string) error {
	s, err := l.open(ctx)
	if err != nil {
		return err
	}
	return s.ChainNote(ctx, id, noteID)
}

func (l *lazyStore) MarkEdit(ctx context.Context, id, folder string, at int64) error {
	s, err := l.open(ctx)
	if err != nil {
		return err
	}
	return s.MarkEdit(ctx, id, folder, at)
}

func (l *lazyStore) MarkStopAsked(ctx context.Context, id string) error {
	s, err := l.open(ctx)
	if err != nil {
		return err
	}
	return s.MarkStopAsked(ctx, id)
}

func (l *lazyStore) MarkReadHint(ctx context.Context, id string, at int64) (bool, error) {
	s, err := l.open(ctx)
	if err != nil {
		return false, err
	}
	return s.MarkReadHint(ctx, id, at)
}

func (l *lazyStore) EndSession(ctx context.Context, id, reason string, at int64) error {
	s, err := l.open(ctx)
	if err != nil {
		return err
	}
	return s.EndSession(ctx, id, reason, at)
}

func (l *lazyStore) RecentEnd(ctx context.Context, folder, reason string, since int64, foldCase bool) (app.SessionState, bool, error) {
	s, err := l.open(ctx)
	if err != nil {
		return app.SessionState{}, false, err
	}
	x, ok, err := s.RecentEnd(ctx, folder, reason, since, foldCase)
	return toState(x), ok, err
}

func (l *lazyStore) PruneSessions(ctx context.Context, before time.Time) (int, error) {
	s, err := l.open(ctx)
	if err != nil {
		return 0, err
	}
	return s.PruneSessions(ctx, before)
}

var _ app.Bootstrap = (*lazyStore)(nil)

func (l *lazyStore) Bootstrap(ctx context.Context, repo string) (app.BootstrapState, bool, error) {
	s, err := l.open(ctx)
	if err != nil {
		return app.BootstrapState{}, false, err
	}
	b, ok, err := s.Bootstrap(ctx, repo)
	return app.BootstrapState(b), ok, err
}

func (l *lazyStore) SaveBootstrap(ctx context.Context, b app.BootstrapState) error {
	s, err := l.open(ctx)
	if err != nil {
		return err
	}
	return s.SaveBootstrap(ctx, sqlite.BootstrapState(b))
}

func (l *lazyStore) MarkOffered(ctx context.Context, repo string) (bool, error) {
	s, err := l.open(ctx)
	if err != nil {
		return false, err
	}
	return s.MarkOffered(ctx, repo)
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
		out = strings.TrimRight(out, "\r\n") + fmt.Sprintf("\nnxy: command timed out after %s and was killed", acceptTimeout)
		return app.ShellResult{Exit: -1, Output: out, TimedOut: true}, nil
	}
	return app.ShellResult{Exit: exit, Output: out}, err
}

// gitChanges answers from git without a shell.
type gitChanges struct{}

func (gitChanges) Head(dir string) string { return platform.GitHead(dir) }
func (gitChanges) Changed(ctx context.Context, dir, base string) ([]string, error) {
	return platform.GitChanged(ctx, dir, base)
}
func (gitChanges) Files(ctx context.Context, dir string) ([]string, error) {
	return platform.GitFiles(ctx, dir)
}

// codegraphGraph drives the codegraph CLI; it is found once per process.
type codegraphGraph struct {
	once sync.Once
	g    *tools.Graph
}

func (c *codegraphGraph) Affected(ctx context.Context, dir string, changed []string) app.GraphResult {
	c.once.Do(func() { c.g = tools.NewGraph(os.Getenv) })
	r := c.g.Affected(ctx, dir, changed)
	st := app.GraphMissing
	switch r.Status {
	case tools.StatusOK:
		st = app.GraphOK
	case tools.StatusInitialized:
		st = app.GraphInitialized
	case tools.StatusInitFailed:
		st = app.GraphInitFailed
	case tools.StatusInitTimeout:
		st = app.GraphInitTimeout
	case tools.StatusLocked:
		st = app.GraphLocked
	case tools.StatusSyncFailed:
		st = app.GraphSyncFailed
	case tools.StatusQueryFailed:
		st = app.GraphQueryFailed
	}
	return app.GraphResult{Tests: r.Tests, Status: st, Took: r.Took}
}

// runnerRows is the embedded table plus <project>/.nxy/runners.json.
type runnerRows struct{}

func (runnerRows) Rows(dir string) ([]affected.Runner, string) {
	builtin := affected.Builtin()
	data, err := os.ReadFile(filepath.Join(dir, ".nxy", "runners.json"))
	if err != nil {
		return builtin, ""
	}
	repo, err := affected.Parse(data)
	if err != nil {
		return builtin, ".nxy/runners.json inválido, se ignora: " + err.Error()
	}
	return affected.Merge(repo, builtin), ""
}

type diskTree struct{ root string }

func (t diskTree) Read(p string) ([]byte, bool) {
	b, err := os.ReadFile(filepath.Join(t.root, filepath.FromSlash(p)))
	return b, err == nil
}

type fileTrees struct{}

func (fileTrees) Tree(dir string) affected.Tree { return diskTree{platform.NativePath(dir)} }

type fileProbe struct{}

func (fileProbe) NativePath(p string) string { return platform.NativePath(p) }
func (fileProbe) IsDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func (fileProbe) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// docsFinder looks for the docs a feature changes with rg, or by walking the repo's .md files.
type docsFinder struct{}

func (docsFinder) Find(ctx context.Context, root string, changed []string) domain.Docs {
	return tools.FindDocs(ctx, platform.NativePath(root), changed)
}

// lensDirs reads the lens files of a folder; an invalid one is skipped and named in the warning.
type lensDirs struct{}

func readLensDir(dir, label string) ([]review.Lens, string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, ""
	}
	var out []review.Lens
	var bad []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			bad = append(bad, e.Name())
			continue
		}
		l, ok := review.ParseLens(string(raw), strings.TrimSuffix(e.Name(), ".md"))
		if !ok {
			bad = append(bad, e.Name())
			continue
		}
		out = append(out, l)
	}
	if len(bad) == 0 {
		return out, ""
	}
	return out, fmt.Sprintf("lente inválida en %s, se ignora: %s", label, strings.Join(bad, ", "))
}

func (lensDirs) User() ([]review.Lens, string) {
	u, err := platform.UserDir()
	if err != nil {
		return nil, ""
	}
	return readLensDir(filepath.Join(u, "lenses"), "~/.nxy/lenses")
}

func (lensDirs) Repo(dir string) ([]review.Lens, string) {
	return readLensDir(filepath.Join(platform.NativePath(dir), ".nxy", "lenses"), ".nxy/lenses")
}

// gitDiffs gives the diff since the approved base.
// Each hook is its own process, so the result is cached per process by dir+base:
// N reviewers in one hook run git once.
type gitDiffs struct{}

var diffCache = struct {
	sync.Mutex
	m map[string][]review.FileDiff
}{m: map[string][]review.FileDiff{}}

func (gitDiffs) Diff(ctx context.Context, dir, base string) ([]review.FileDiff, error) {
	key := dir + "\x00" + base
	diffCache.Lock()
	defer diffCache.Unlock()
	if out, ok := diffCache.m[key]; ok {
		return out, nil
	}
	ds, err := platform.GitDiff(ctx, dir, base)
	if err != nil {
		return nil, err
	}
	out := make([]review.FileDiff, len(ds))
	for i, d := range ds {
		out[i] = review.FileDiff{Path: d.Path, Status: d.Status, Text: d.Text}
	}
	diffCache.m[key] = out
	return out, nil
}

// reviewDocs keeps the full review at <UserDir>/reviews/<id>.md while the feature is open.
type reviewDocs struct{}

func reviewPath(id string) (string, error) {
	u, err := platform.UserDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(u, "reviews", id+".md"), nil
}

func (reviewDocs) Write(id, markdown string) (string, error) {
	p, err := reviewPath(id)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, []byte(markdown), 0o644); err != nil {
		return "", err
	}
	return p, nil
}

func (reviewDocs) Discard(id string) error {
	p, err := reviewPath(id)
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
		Config: filterConfig{}, Engine: filterEngine{}, Permissions: permissionRules{}, Roles: roleConfig{},
		Store: st, Repos: repoKeys{}, Legacy: legacyPlugin{}, Viewer: planViewer{}, Shell: shellRunner{}, Files: fileProbe{}, Exe: exePath(),
		Changes: gitChanges{}, Graph: &codegraphGraph{}, Runners: runnerRows{}, Trees: fileTrees{},
		Lenses: lensDirs{}, Diffs: gitDiffs{}, ReviewLog: st, ReviewFiles: reviewDocs{}, Docs: docsFinder{},
		Memory: st, Sessions: st, Bootstrap: st,
		Gate: gateFiles{getenv: os.Getenv}, Context: claudeContext{},
	}, st
}
