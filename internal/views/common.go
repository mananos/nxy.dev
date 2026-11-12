package views

// Status is the stable key of a feature state; the TUI picks the Spanish label.
type Status string

const (
	StatusWorking   Status = "working"
	StatusVerifying Status = "verifying"
	StatusNeedsYou  Status = "needs_you"
	StatusDone      Status = "done"
)

// StepStatus mirrors domain.StepStatus.
type StepStatus string

const (
	StepPending   StepStatus = "pending"
	StepRunning   StepStatus = "running"
	StepVerifying StepStatus = "verifying"
	StepGreen     StepStatus = "green"
	StepFailed    StepStatus = "failed"
	StepAccepted  StepStatus = "accepted"
)

// Stage keys of a feature, in flow order (the TUI picks the Spanish label).
const (
	StageDesign  = "design"
	StagePlan    = "plan"
	StageBatches = "batches"
	StageReview  = "review"
	StageClose   = "close"
)

// UnknownMs is a duration with no start (a step saved before the start was stamped): the TUI prints nothing.
const UnknownMs int64 = -1

// Action is a button: a command of the binary. Confirm says what it changes;
// Planned means the command does not exist yet. An Argv may carry "{value}": the
// TUI replaces it with the option the user picked (Config uses it for model and effort).
type Action struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Argv    []string `json:"argv"`
	Confirm string   `json:"confirm"`
	Planned bool     `json:"planned"`
}

// FeatureHeader is the summary line shared by every screen of a feature.
type FeatureHeader struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Status        Status    `json:"status"`
	Phase         string    `json:"phase"`
	Stage         string    `json:"stage"`        // design|plan|batches|review|close
	StageDetail   string    `json:"stage_detail"` // "3/4"
	ClosedAt      string    `json:"closed_at"`    // RFC3339; empty while open
	Repos         []RepoRef `json:"repos"`
	Batch         int       `json:"batch"`
	Batches       int       `json:"batches"`
	ElapsedMs     int64     `json:"elapsed_ms"` // -1 (UnknownMs) = no duration
	CostUSD       float64   `json:"cost_usd"`
	CostEstimated bool      `json:"cost_estimated"`
}

// RepoRef names a repo of a feature.
type RepoRef struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Role     string `json:"role"`
	Branch   string `json:"branch"`
	Worktree bool   `json:"worktree"`
}

// TestRef is a test file and what its last run said.
type TestRef struct {
	Path   string `json:"path"`
	Status string `json:"status"` // pass|fail|none (✓ ✗ ○)
}

// Test statuses.
const (
	TestPass = "pass"
	TestFail = "fail"
	TestNone = "none"
)

// MemRef is a rule (K3) or learning (A2) delivered to an agent or anchored to a file.
type MemRef struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"` // rule|learning
	Text  string `json:"text"`
	Cited bool   `json:"cited"`
}
