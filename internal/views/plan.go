package views

// Plan is the plan of a feature with its impact map.
type Plan struct {
	Feature    FeatureHeader  `json:"feature"`
	State      string         `json:"state"`       // approval|running|done
	ApprovedAt string         `json:"approved_at"` // RFC3339; empty while it waits for approval
	Goal       string         `json:"goal"`
	Repos      []RepoRef      `json:"repos"`
	Criteria   []Criterion    `json:"criteria"`
	Decisions  []PlanDecision `json:"decisions"`
	Batches    []PlanBatch    `json:"batches"`
	Risks      []string       `json:"risks"`
	Impact     Impact         `json:"impact"`
	Diagram    Diagram        `json:"diagram"` // d2 of the change; empty Source = no diagram
	Actions    []Action       `json:"actions"`
}

// Plan states.
const (
	PlanApproval = "approval"
	PlanRunning  = "running"
	PlanDone     = "done"
)

type Criterion struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Test string `json:"test"`
}

type PlanDecision struct {
	Scope      string `json:"scope"` // repo|task
	Repo       string `json:"repo"`
	Text       string `json:"text"`
	Question   string `json:"question"`
	FromDesign bool   `json:"from_design"` // it comes from the design (Ingeniero), not from the plan
}

type PlanBatch struct {
	N        int        `json:"n"`
	Title    string     `json:"title"`
	Repo     string     `json:"repo"`
	Status   StepStatus `json:"status"`
	Depends  []int      `json:"depends"`
	Risk     string     `json:"risk"`
	RiskWhy  string     `json:"risk_why"`
	Attempt  int        `json:"attempt"`  // 0 until it runs
	Attempts int        `json:"attempts"` // how many it gets
	Accept   string     `json:"accept"`
	Changes  []Change   `json:"changes"`
}

type Change struct {
	At   string `json:"at"`
	What string `json:"what"`
}

// Impact maps files -> importers -> tests, per batch.
type Impact struct {
	Source string       `json:"source"` // codegraph|name|none
	Files  []ImpactFile `json:"files"`
}

// ImpactFile is a file a batch touches. Path is relative to its repo.
type ImpactFile struct {
	Path       string       `json:"path"`
	Repo       string       `json:"repo"`
	Batch      int          `json:"batch"`
	Kind       string       `json:"kind"` // edit|new
	Lines      []FileChange `json:"lines"`
	ImportedBy []string     `json:"imported_by"`
	Tests      []TestRef    `json:"tests"`
	Contract   *ContractRef `json:"contract"`
}

// FileChange is what the plan changes at a line (":39 nota pasa a NotaParcial").
type FileChange struct {
	Line int    `json:"line"`
	What string `json:"what"`
}

// ContractRef is the contract of another repo that a file takes (↔ NotaCreditoDto).
type ContractRef struct {
	Name   string     `json:"name"`
	Repo   string     `json:"repo"`
	Batch  int        `json:"batch"`
	Status StepStatus `json:"status"`
}
