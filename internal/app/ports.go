package app

import (
	"context"
	"errors"
	"time"

	"github.com/mananos/nxy.dev/internal/affected"
	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/filter"
	"github.com/mananos/nxy.dev/internal/gate"
	"github.com/mananos/nxy.dev/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/internal/legacy"
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

// FeatureFilter narrows Store.Features: Home is a folder key (empty = any); with neither Active nor
// Paused every feature matches.
type FeatureFilter struct {
	Home   string
	Active bool
	Paused bool
}

// Store persists features. There is no Load/Save: changes go through Transition.
type Store interface {
	CreateFeature(ctx context.Context, f domain.Feature) (string, error)
	// Feature reads one feature; it fails when the id is unknown.
	Feature(ctx context.Context, id string) (domain.Feature, error)
	// ActiveFeature is the feature that holds the checkout (one active feature per checkout).
	ActiveFeature(ctx context.Context, checkout string) (domain.Feature, bool, error)
	// LatestFeature is the most recent feature started from the folder key, done or not.
	LatestFeature(ctx context.Context, key string) (domain.Feature, bool, error)
	// Features lists features, newest first; Active excludes paused and done ones, Paused keeps only paused.
	Features(ctx context.Context, f FeatureFilter) ([]domain.Feature, error)
	// Transition applies fn to the stored feature. fn must be pure: it can run more than once.
	Transition(ctx context.Context, id string, fn func(domain.Feature) (domain.Feature, error)) error
}

// MemoryStore persists the memory (rules, closed features, learnings, notes). The
// Resolve-style references (K3, A7, N2, ULIDs) are resolved by the store.
type MemoryStore interface {
	AddRule(ctx context.Context, r memory.Rule) (memory.Rule, error)
	Rules(ctx context.Context, repo string) ([]memory.Rule, error)
	AddGlobalRule(ctx context.Context, r memory.Rule) (memory.Rule, error)
	GlobalRules(ctx context.Context) ([]memory.Rule, error)
	EditRule(ctx context.Context, ref string, e memory.RuleEdit) (memory.Rule, error)
	EditLearning(ctx context.Context, ref string, e memory.LearningEdit) (memory.Learning, error)
	MemoryKind(ctx context.Context, ref string) (string, error)
	DeleteMemory(ctx context.Context, ref string) (string, error)
	KeepMemory(ctx context.Context, ref string) (string, error)
	RestoreMemory(ctx context.Context, ref string) (string, error)
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
	// GlobalRules are the user's global rules (G<n>).
	GlobalRules(ctx context.Context) ([]memory.Rule, error)
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
	// Temporary notes the flow reads and cleans up.
	Notes(ctx context.Context, folder string, foldCase bool, now time.Time) ([]memory.Note, error)
	Note(ctx context.Context, ref string) (memory.Note, error)
	SaveNote(ctx context.Context, n memory.Note) (memory.Note, error)
	SweepNotes(ctx context.Context, now time.Time) (int, error)
	DeleteNotesOfFeature(ctx context.Context, featureID string) (int, error)
	DeleteNotesUnder(ctx context.Context, folder string, foldCase bool) (int, error)
}

// SessionState is the local per-session state of the hooks; times are in ms (zero = never).
type SessionState struct {
	ID              string
	Folder          string
	Source          string
	StartedAt       int64
	NotesPending    bool
	RecommendedNote string
	NoteID          string
	StopAsked       bool
	LastEditAt      int64
	ReadHintAt      int64
	EndedAt         int64
	EndReason       string
	FeatureID       string
}

// Sessions keeps the per-session state of the hooks (local, never exported).
type Sessions interface {
	StartSession(ctx context.Context, s SessionState) error
	Session(ctx context.Context, id string) (SessionState, bool, error)
	// ConsumeNotesPending is true only the one time notes_pending was set.
	ConsumeNotesPending(ctx context.Context, id string) (bool, error)
	ChainNote(ctx context.Context, id, noteID string) error
	// LinkFeature ties the session to the feature it works on.
	LinkFeature(ctx context.Context, sessionID, featureID string) error
	// OtherLive is true when a session other than exceptID, not ended, is linked to the feature.
	OtherLive(ctx context.Context, featureID, exceptID string) (bool, error)
	MarkEdit(ctx context.Context, id, folder string, at int64) error
	MarkStopAsked(ctx context.Context, id string) error
	// MarkReadHint is true only the first time for the session.
	MarkReadHint(ctx context.Context, id string, at int64) (bool, error)
	EndSession(ctx context.Context, id, reason string, at int64) error
	// RecentEnd finds the newest session of the folder that ended with reason at or after since (ms).
	RecentEnd(ctx context.Context, folder, reason string, since int64, foldCase bool) (SessionState, bool, error)
	PruneSessions(ctx context.Context, before time.Time) (int, error)
}

// BootstrapState is the bootstrap of the first rules of a repo: status is "", "scouting",
// "confirming" or "done"; the pending request waits for it to finish.
type BootstrapState struct {
	Repo            string
	OfferedAt       int64
	Status          string
	Candidates      []memory.BootstrapCandidate
	PendingRequest  string
	PendingEngineer bool
}

// Bootstrap keeps the bootstrap state per repo key (local, never exported).
type Bootstrap interface {
	Bootstrap(ctx context.Context, repo string) (BootstrapState, bool, error)
	SaveBootstrap(ctx context.Context, b BootstrapState) error
	// MarkOffered is true only the first time for the repo.
	MarkOffered(ctx context.Context, repo string) (bool, error)
}

// GateEscape names a one-shot escape of the gate: edit or search.
type GateEscape string

const (
	GateEscapeEdit   GateEscape = "edit"
	GateEscapeSearch GateEscape = "search"
)

// GateRecord is one ledger row; Kind is "read" for a search and "" for an edit. Ts is epoch ms.
type GateRecord struct {
	Ts            int64
	Tool          string
	Allow         bool
	Reason        string
	ContextTokens *float64
	Threshold     float64
	Kind          string
}

// Gate serves the gate: its config, one-shot escapes and the ledger of decisions (per checkout).
type Gate interface {
	// Settings is the gate section of the merged config for dir.
	Settings(dir string) gate.Config
	// Arm arms an escape for the config window and returns when it expires (epoch ms).
	Arm(dir string, kind GateEscape) (int64, error)
	// Until is when the armed escape expires (epoch ms), 0 when there is none.
	Until(dir string, kind GateEscape) int64
	// Consume uses up the escape; true means let this one through.
	Consume(dir string, kind GateEscape) bool
	Record(dir string, r GateRecord)
	Rows(dir string) []gate.Row
}

// ContextMeter measures the main thread's context from its transcript; ok is false when unknown.
type ContextMeter interface {
	MainContextTokens(transcriptPath string) (tokens float64, ok bool)
}

// Repos gives the stable key of the repository a directory belongs to.
type Repos interface {
	Key(dir string) string
	// Checkout is the key of the checkout (worktree root, or the folder when it is no repo) of dir.
	Checkout(dir string) string
	// Root is the git root of dir; ok is false when dir is not in a git repo.
	Root(dir string) (string, bool)
}

// BranchInfo is what a repo offers for the branch question: its local branches (newest first), the
// default one ("" when unknown) and the one checked out in dir ("" when detached).
type BranchInfo struct {
	Branches []string
	Default  string
	Current  string
}

// BranchChange is what Switch or AddWorktree did: the git command, the branch and, for a worktree,
// its path. Created is true when the branch did not exist.
type BranchChange struct {
	Command string
	Path    string
	Branch  string
	Created bool
}

// Branches runs the git of the branch step: the branch in the session's folder or a worktree per repo.
// It never stashes and never forces; the errors are git's words in Spanish.
type Branches interface {
	List(ctx context.Context, dir string) BranchInfo
	// Switch moves dir to branch (a free existing one) or creates it from the local base.
	Switch(ctx context.Context, dir, branch, base string) (BranchChange, error)
	// AddWorktree opens <repo>.worktrees/<slug> on the branch, creating it from base when it is new.
	AddWorktree(ctx context.Context, repoDir, branch, base string) (BranchChange, error)
	// RemoveWorktree removes a worktree opened by AddWorktree (it has no changes yet).
	RemoveWorktree(ctx context.Context, cwd, path string) error
	// OpenCommand is the command line that opens path in the user's editor.
	OpenCommand(path string) string
	// MissingDeps is true when path has a package.json without node_modules.
	MissingDeps(path string) bool
}

// Legacy tells whether the 1.x plugin is still enabled for a project.
type Legacy interface {
	Active(projectDir string) bool
}

// LegacyData reads what the 1.x left on disk. Every read is on a copy or read-only: the 1.x files
// are never written.
type LegacyData interface {
	// ProjectKey is the key the 1.x memory.db files the repo of dir under (platform.LegacyProjectKey).
	ProjectKey(dir string) string
	// RepoKey is the v2 key of the repo of dir.
	RepoKey(dir string) string
	// Checkout names the checkout of dir in the filter tables.
	Checkout(dir string) string
	// Folder is the folder the imported notes belong to: the repo root, or dir itself.
	Folder(dir string) string
	// HasMemory tells whether the 1.x memory.db exists.
	HasMemory() bool
	// Memory reads the memories of the project and their edges.
	Memory(ctx context.Context, project string) ([]legacy.Memory, []legacy.Edge, error)
	// HasReview tells whether review.jsonl of dir's checkout exists.
	HasReview(dir string) bool
	// Review reads the rows of review.jsonl of dir's checkout.
	Review(dir string) []any
	// FilterSize is the size of filter.jsonl of dir's checkout (false without the file).
	FilterSize(dir string) (int64, bool)
	// FilterHead hashes the start of filter.jsonl up to upTo bytes (cursor head).
	FilterHead(dir string, upTo int64) string
	// FilterFrom reads the complete lines of filter.jsonl from offset.
	FilterFrom(dir string, offset int64) ([]any, legacy.Cursor, error)
}

// LegacyStore keeps what LegacyData read; each import is atomic and marks itself, so a repeat writes nothing.
type LegacyStore interface {
	// ImportLegacyMemory returns the counts of what was really stored (real A<n> range, repeated left out).
	ImportLegacyMemory(ctx context.Context, repo string, imp legacy.Import) (legacy.Counts, bool, error)
	ImportLegacyReviews(ctx context.Context, checkout string, picks []legacy.Pick, counts legacy.Counts) (bool, error)
	ImportLegacyFilter(ctx context.Context, checkout string, from, to legacy.Cursor, groups map[string][]report.FilterGroup, reset bool) (legacy.FilterResult, error)
	LegacyImports(ctx context.Context) ([]legacy.Mark, []legacy.StoredCursor, error)
	// SetLegacyError keeps the last memory import failure of repo (empty msg clears it).
	SetLegacyError(ctx context.Context, repo string, at int64, msg string) error
	// AddFilterGroup adds what the v2's PostToolUse hook saw to the session's summary (source "v2").
	AddFilterGroup(ctx context.Context, checkout, session string, g report.FilterGroup) error
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

// AgentFiles reads a subagent's transcript; ok is false when it could not be read.
type AgentFiles interface {
	Read(path, agentID string) (transcript.FileResult, bool)
}

// AgentRuns keeps one row per subagent run.
type AgentRuns interface {
	RecordAgentRun(ctx context.Context, r domain.AgentRun) error
}

// PendingAsks keeps the questions of nxy held by the hook until the TUI answers them.
type PendingAsks interface {
	CreatePendingAsk(ctx context.Context, a domain.PendingAsk) (domain.PendingAsk, error)
	// PendingAsk reads one row; it fails when it is gone.
	PendingAsk(ctx context.Context, id string) (domain.PendingAsk, error)
	// TouchPendingAsk renews an unanswered row; rows nobody renews stop being listed, read or answerable.
	TouchPendingAsk(ctx context.Context, id string) error
	OpenPendingAsks(ctx context.Context) ([]domain.PendingAsk, error)
	// AnswerPendingAsk is false when the row was already answered or is gone.
	AnswerPendingAsk(ctx context.Context, id string, answers []string, by string) (bool, error)
	ReleaseFeatureAsks(ctx context.Context, featureID, by string) error
	DeletePendingAsk(ctx context.Context, id string) error
}

// TUILiveness tells whether a TUI is open and can answer questions.
type TUILiveness interface {
	Alive() bool
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
