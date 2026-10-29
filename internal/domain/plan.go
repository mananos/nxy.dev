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
	Title     string     `json:"title"`
	Decisions []Decision `json:"decisions,omitempty"`
	Rules     []Rule     `json:"rules"`
	Batches   []Batch    `json:"batches"`
	Risks     []string   `json:"risks,omitempty"`
	Questions []Question `json:"questions,omitempty"`
	NoTests   bool       `json:"no_tests,omitempty"`
}

// Rule is one behavior the feature must have.
type Rule struct {
	ID    string   `json:"id"` // "R1"
	Text  string   `json:"text"`
	Test  TestKind `json:"test"`
	Check string   `json:"check,omitempty"` // what to look at when Test is manual
}

// Batch is one unit of behavior to implement and verify.
type Batch struct {
	N       int       `json:"n"`
	Title   string    `json:"title"`
	Covers  []string  `json:"covers"`
	Read    []string  `json:"read,omitempty"`
	Change  []Change  `json:"change"`
	Test    BatchTest `json:"test"`
	Risk    Risk      `json:"risk"`
	Depends []int     `json:"depends,omitempty"`
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

// Decision is something already settled.
type Decision struct {
	Scope    Scope  `json:"scope"`
	Text     string `json:"text"`
	Question string `json:"question,omitempty"`
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
