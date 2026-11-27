package domain

// TestKind says how a rule is proved.
type TestKind string

const (
	TestTDD      TestKind = "tdd"
	TestExisting TestKind = "existing"
	TestBuild    TestKind = "build"
	TestManual   TestKind = "manual"
)

// BatchTestKind says whether a batch adds a test or reuses one.
type BatchTestKind string

const (
	BatchTestNew      BatchTestKind = "new"
	BatchTestExisting BatchTestKind = "existing"
)

// Risk is how dangerous a batch is.
type Risk string

const (
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)

// Scope says where a decision applies.
type Scope string

const (
	ScopeRepo Scope = "repo"
	ScopeTask Scope = "task"
)

// Plan is what the planner saves.
type Plan struct {
	Title string `json:"title"`
	// Repos are the git repos the plan touches; empty means the one repo of the session.
	Repos     []PlanRepo  `json:"repos,omitempty"`
	Decisions []Decision  `json:"decisions,omitempty"`
	Criteria  []Criterion `json:"criteria"`
	Batches   []Batch     `json:"batches"`
	Risks     []string    `json:"risks,omitempty"`
	Questions []Question  `json:"questions,omitempty"`
	NoTests   bool        `json:"no_tests,omitempty"`
	// Suite is the full-suite command, used only when the affected tests cannot be worked out.
	Suite string `json:"suite,omitempty"`
	// D2 is the d2 source of the diagram of the change (optional).
	D2 string `json:"d2,omitempty"`
	// RepoRunners are the rows of the repo's .nxy/runners.json, set by nxy (not the planner) when
	// the plan is saved so the user sees what will run; RepoRunnersWarn is set when the file is invalid.
	RepoRunners     []RepoRunner `json:"repo_runners,omitempty"`
	RepoRunnersWarn string       `json:"repo_runners_warn,omitempty"`
}

// PlanRepo is one repo of a multi-repo plan. Path is relative to the session folder ("." is itself).
type PlanRepo struct {
	Path  string `json:"path"`
	Role  string `json:"role,omitempty"`
	Suite string `json:"suite,omitempty"`
	// Name is how the repo is written in paths ("api/src/X.java"); nxy sets it, not the planner.
	Name string `json:"name,omitempty"`
}

// RepoRunner is one row of the repo's .nxy/runners.json, as shown at the checkpoint.
type RepoRunner struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// Criterion is one behavior the feature must have.
type Criterion struct {
	ID    string   `json:"id"` // "R1"
	Text  string   `json:"text"`
	Test  TestKind `json:"test"`
	Check string   `json:"check,omitempty"` // what to look at when Test is manual
}

// Batch is one unit of behavior to implement and verify.
type Batch struct {
	N      int       `json:"n"`
	Title  string    `json:"title"`
	Covers []string  `json:"covers"`
	Read   []string  `json:"read,omitempty"`
	Change []Change  `json:"change"`
	Test   BatchTest `json:"test"`
	Risk   Risk      `json:"risk"`
	// RiskWhy is why nxy raised Risk; nxy sets it, not the planner.
	RiskWhy string `json:"risk_why,omitempty"`
	Depends []int  `json:"depends,omitempty"`
	// Repo is the repo name the batch edits in a multi-repo plan; nxy deduces it from change.
	Repo string `json:"repo,omitempty"`
}

// Change is one edit, located as path:line.
type Change struct {
	At   string `json:"at"`
	What string `json:"what"`
}

// BatchTest is the proof of a batch.
type BatchTest struct {
	Kind   BatchTestKind `json:"kind"`
	Name   string        `json:"name,omitempty"`
	Accept string        `json:"accept,omitempty"`
}

// Problem is one thing wrong with an input (a plan, a design), located by Where.
type Problem struct {
	Where string
	Msg   string
}

func (p Problem) String() string { return p.Where + ": " + p.Msg }

// DecisionBy says who chose a decision when it was not the user.
type DecisionBy string

// ByPlanner marks a decision the planner took on a fork the user delegated.
const ByPlanner DecisionBy = "planner"

// Decision is something already settled.
type Decision struct {
	ID       string     `json:"id,omitempty"`
	Scope    Scope      `json:"scope"`
	Text     string     `json:"text"`
	Question string     `json:"question,omitempty"`
	Why      string     `json:"why,omitempty"`
	Dissent  string     `json:"dissent,omitempty"`
	Fork     string     `json:"fork,omitempty"`
	By       DecisionBy `json:"by,omitempty"`
	// Repo is the name of the repo a repo-scope decision belongs to (multi-repo plans).
	Repo string `json:"repo,omitempty"`
}

// Question is asked to the user before the plan is final.
type Question struct {
	Question string   `json:"question"`
	Header   string   `json:"header"`
	Options  []Option `json:"options"`
}

// Option is one answer to a Question.
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description"`
	Recommended bool   `json:"recommended,omitempty"`
}

// Answer is the user's reply to a Question.
type Answer struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}
