package app

import (
	"context"
	"errors"
	"time"

	"github.com/mananos/nxy.dev/internal/affected"
	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/filter"
	"github.com/mananos/nxy.dev/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/internal/memory"
	"github.com/mananos/nxy.dev/internal/repomap"
	"github.com/mananos/nxy.dev/internal/report"
	"github.com/mananos/nxy.dev/internal/review"
	"github.com/mananos/nxy.dev/internal/scout"
)

// RepoIndex is the repo map of a project: structure, built by a script and queried by a script.
type RepoIndex interface {
	// Info is the existing index, ok false when there is none.
	Info(dir string) (scout.IndexInfo, bool)
	// Build builds the index (force: from scratch, otherwise only what changed).
	Build(dir string, force bool) (scout.IndexInfo, error)
	Query(dir, term string, limit int) []repomap.Row
}

// TextSearch is the exact-text search (rg): zero tokens, finds what the index misses.
type TextSearch interface {
	Available() bool
	Search(ctx context.Context, dir, pattern string, o scout.RgOpts) scout.RgResult
}

// CodeExplorer is codegraph: relationships between symbols. Optional, capped, declares its fallback.
type CodeExplorer interface {
	Explore(ctx context.Context, dir, question string) scout.CodeResult
	Relations(ctx context.Context, dir, kind, symbol string) scout.CodeResult
	// Describe is the one-line status of codegraph in dir.
	Describe(dir string) string
}

// SessionLookup selects the transcripts a report reads.
type SessionLookup struct {
	// Cwd limits the lookup to the project of that directory unless All is set.
	Cwd string
	All bool
	// ProjectsDir overrides the host's transcript root ("" = its default).
	ProjectsDir string
	// Since drops sessions last modified before this epoch ms; 0 keeps everything.
	Since int64
}

// Transcripts reads the host's session transcripts.
type Transcripts interface {
	// ResolveSession finds "last", an id or a unique id prefix; nil when there is none.
	ResolveSession(selector string, opts SessionLookup) *transcript.SessionRef
	ListSessions(opts SessionLookup) []transcript.SessionRef
	ParseSession(ref transcript.SessionRef, opts transcript.SessionOpts) transcript.Session
	// LastCacheTTL is the TTL the main thread last wrote its cache with ("5m" or "1h").
	LastCacheTTL(path string) (string, bool)
}

// Ledger is rtk's savings ledger, read only.
type Ledger interface {
	// Rows are the commands in [since, until] (epoch ms, nil is open); false when it cannot be read.
	Rows(since, until *int64) ([]report.LedgerRow, bool)
	// Unavailable says why Rows would fail: "no-db" or "".
	Unavailable() string
	// DBPath is where the ledger is looked for.
	DBPath() string
}

// ErrActiveFeature is what CreateFeature returns when the repo already has a feature that is not done.
var ErrActiveFeature = errors.New("repo already has an active feature")

// Store persists features. There is no Load/Save: changes go through Transition.
type Store interface {
	CreateFeature(ctx context.Context, f domain.Feature) (string, error)
	// Feature reads one feature; it fails when the id is unknown.
	Feature(ctx context.Context, id string) (domain.Feature, error)
	// ActiveFeature is the most recent feature of the repo that is not done.
	ActiveFeature(ctx context.Context, repo string) (domain.Feature, bool, error)
	// LatestFeature is the most recent feature of the repo, done or not.
	LatestFeature(ctx context.Context, repo string) (domain.Feature, bool, error)
	// Transition applies fn to the stored feature. fn must be pure: it can run more than once.
	Transition(ctx context.Context, id string, fn func(domain.Feature) (domain.Feature, error)) error
}

// MemoryStore persists the memory (rules, closed features, learnings, notes). The
// Resolve-style references (K3, A7, N2, ULIDs) are resolved by the store.
type MemoryStore interface {
	AddRule(ctx context.Context, r memory.Rule) (memory.Rule, error)
	Rules(ctx context.Context, repo string) ([]memory.Rule, error)
	AddLearning(ctx context.Context, l memory.Learning) (memory.Learning, error)
	Learnings(ctx context.Context, repo string) ([]memory.Learning, error)
	Promote(ctx context.Context, ref, question string, paths []string) (memory.Rule, error)
	Related(ctx context.Context, repo string, paths []string) ([]memory.ClosedFeature, []memory.Learning, error)
	Search(ctx context.Context, repo string, all bool, q string, limit int) ([]memory.Hit, error)
	SaveNote(ctx context.Context, n memory.Note) (memory.Note, error)
	Notes(ctx context.Context, folder string, foldCase bool, now time.Time) ([]memory.Note, error)
	DeleteNote(ctx context.Context, ref string) error
	ExportBundle(ctx context.Context, includeNotes bool) (memory.Bundle, error)
	MemorySnapshot(ctx context.Context) (memory.Snapshot, error)
	ApplyImport(ctx context.Context, d memory.Diff) error
}

// FlowMemory is what the feature flow needs from the memory; a nil one in Hooks skips it all.
type FlowMemory interface {
	Rules(ctx context.Context, repo string) ([]memory.Rule, error)
	Related(ctx context.Context, repo string, paths []string) ([]memory.ClosedFeature, []memory.Learning, error)
	AddLearning(ctx context.Context, l memory.Learning) (memory.Learning, error)
	AddRule(ctx context.Context, r memory.Rule) (memory.Rule, error)
	SaveClosedFeature(ctx context.Context, f memory.ClosedFeature) (memory.ClosedFeature, error)
	// CountDeliveries adds one delivery to each rule (ULIDs).
	CountDeliveries(ctx context.Context, ids []string) error
	// Cite counts a citation on each existing rule (K3) and returns the ones that exist; at is in ms.
	Cite(ctx context.Context, repo string, shorts []string, at int64) ([]string, error)
	// MarkObsolete marks learnings (A7) as obsolete; at is in ms.
	MarkObsolete(ctx context.Context, repo string, refs []string, at int64) error
	SimilarRules(ctx context.Context, repo, text string, limit int) ([]memory.Rule, error)
	RecordEscape(ctx context.Context, featureID, repo, lens, text string) error
	ReviewStats(ctx context.Context, repo string, since int64) ([]domain.LensStat, error)
}

// Repos gives the stable key of the repository a directory belongs to.
type Repos interface {
	Key(dir string) string
}

// Legacy tells whether the 1.x plugin is still enabled for a project.
type Legacy interface {
	Active(projectDir string) bool
}

// Viewer shows the full plan to the user and cleans it up afterwards.
type Viewer interface {
	Show(ctx context.Context, f domain.Feature) (string, error)
	Discard(id string) error
}

// Clock gives the current time.
type Clock interface {
	Now() time.Time
}

// FilterConfig gives the filter settings for a project.
type FilterConfig interface {
	Filter(projectDir string) filter.Settings
}

// FilterEngine resolves which tool rewrites commands and asks it to.
type FilterEngine interface {
	Info(s filter.Settings, projectDir string) filter.EngineInfo
	Rewrite(info filter.EngineInfo, cmd string) (filter.RewriteResult, error)
}

// Roles gives the model and effort the config sets for a subagent role ("planner", "scout"...);
// an empty string means the config says nothing and the role keeps its frontmatter.
type Roles interface {
	Role(projectDir, role string) (model, effort string)
}

// ShellResult is the outcome of a command run by Shell: Output is the tail of stdout and stderr.
type ShellResult struct {
	Exit   int
	Output string
	// TimedOut: the command ran out of time and was killed (Exit is -1).
	TimedOut bool
}

// Shell runs a command line with the system shell. It fails only when the shell cannot
// start or ctx expires; a command that exits non-zero is a result, not an error.
type Shell interface {
	Run(ctx context.Context, dir, command string) (ShellResult, error)
}

// Files answers the two questions the Bash write parser asks of the file system.
type Files interface {
	// NativePath turns a Git Bash path (/c/x) into the native one.
	NativePath(p string) string
	IsDir(path string) bool
	// Exists reports whether the path exists (file or directory).
	Exists(path string) bool
}

// PermissionRules gives the host's allow/deny rules for a project.
type PermissionRules interface {
	Rules(projectDir string) filter.Rules
}

// Changes reads the repo's git state.
type Changes interface {
	// Head is the HEAD sha, or "" when there is no repo or no commit yet.
	Head(dir string) string
	// Changed lists the files changed since base (committed or not), repo-relative with slashes.
	Changed(ctx context.Context, dir, base string) ([]string, error)
	// Files lists the files of the repo (tracked and not ignored).
	Files(ctx context.Context, dir string) ([]string, error)
}

// GraphStatus says how the code graph answered; it mirrors the tools adapter.
type GraphStatus string

const (
	GraphMissing     GraphStatus = "missing"
	GraphInitialized GraphStatus = "initialized"
	GraphInitFailed  GraphStatus = "init_failed"
	GraphInitTimeout GraphStatus = "init_timeout"
	GraphLocked      GraphStatus = "locked"
	GraphSyncFailed  GraphStatus = "sync_failed"
	GraphQueryFailed GraphStatus = "query_failed"
	GraphOK          GraphStatus = "ok"
)

// GraphResult is the answer of the code graph: the tests the changed files affect.
type GraphResult struct {
	Tests  []string
	Status GraphStatus
	Took   time.Duration
}

// TestGraph finds the tests affected by a set of changed files.
type TestGraph interface {
	Affected(ctx context.Context, dir string, changed []string) GraphResult
}

// Runners gives the test-runner table of a project: the embedded rows plus the repo's own.
// The string is a Spanish warning when the repo's table is invalid ("" otherwise).
type Runners interface {
	Rows(dir string) ([]affected.Runner, string)
}

// Lenses gives the review lenses of the user and of the repo (the embedded ones are in the
// review package). The string is a Spanish warning when a lens file is invalid ("" otherwise).
type Lenses interface {
	User() ([]review.Lens, string)
	Repo(dir string) ([]review.Lens, string)
}

// Diffs gives the diff of a project since a base, one entry per changed file.
type Diffs interface {
	Diff(ctx context.Context, dir, base string) ([]review.FileDiff, error)
}

// Docs finds the docs of a project that name what the changed files touched.
type Docs interface {
	Find(ctx context.Context, root string, changed []string) domain.Docs
}

// ReviewLog records which offered findings the user chose and which they ignored.
type ReviewLog interface {
	RecordPicks(ctx context.Context, featureID, repo string, picks []domain.Pick) error
}

// ReviewFiles keeps the full review document of a feature while it is open.
type ReviewFiles interface {
	Write(id, markdown string) (string, error)
	Discard(id string) error
}

// FileTrees reads files of a project by repo-relative path.
type FileTrees interface {
	Tree(dir string) affected.Tree
}
