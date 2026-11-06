package domain

// Severity is how bad a finding is.
type Severity string

const (
	SeverityHigh   Severity = "high"
	SeverityMedium Severity = "medium"
	SeverityLow    Severity = "low"
)

// Cause says whether the change introduced the finding or it was already there.
type Cause string

const (
	CauseChange      Cause = "change"
	CausePreexisting Cause = "preexisting"
)

// Verdict is the refuter's conclusion about a finding.
type Verdict string

const (
	VerdictConfirmed    Verdict = "confirmed"
	VerdictRefuted      Verdict = "refuted"
	VerdictInconclusive Verdict = "inconclusive"
)

// Finding is one problem a reviewer found.
type Finding struct {
	ID         string   `json:"id"` // "H1"
	Lens       string   `json:"lens"`
	Severity   Severity `json:"severity"`
	File       string   `json:"file,omitempty"`
	Line       int      `json:"line,omitempty"`
	Title      string   `json:"title"`
	Scenario   string   `json:"scenario,omitempty"`
	Evidence   string   `json:"evidence,omitempty"`
	Fix        string   `json:"fix,omitempty"`
	Cause      Cause    `json:"cause,omitempty"`
	Criterion  string   `json:"criterion,omitempty"` // set when it comes from a failed criterion check
	Rule       string   `json:"rule,omitempty"`      // K<n> of the repo rule the change breaks
	Dropped    string   `json:"dropped,omitempty"`   // why the precision filter dropped it
	Verdict    Verdict  `json:"verdict,omitempty"`
	VerdictWhy string   `json:"verdict_why,omitempty"`
	Why        string   `json:"why,omitempty"`    // engineer: why it matters
	Lesson     string   `json:"lesson,omitempty"` // engineer: the general rule behind it
}

// DecisionCheck is the base pass's answer for one decision of the design: where
// the diff carries it, or how (without a file) it is not in the diff.
type DecisionCheck struct {
	ID   string `json:"id"` // "D1"
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	How  string `json:"how,omitempty"`
}

// CriterionCheck is the reviewer's answer for one criterion of the plan.
type CriterionCheck struct {
	ID   string `json:"id"` // "R1"
	OK   bool   `json:"ok"`
	Why  string `json:"why,omitempty"`
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
}

// LensRun is one lens selected for the review and where it stands.
type LensRun struct {
	Name       string   `json:"name"`
	Title      string   `json:"title,omitempty"`
	Files      []string `json:"files,omitempty"`
	Dispatched bool     `json:"dispatched,omitempty"`
	Saved      bool     `json:"saved,omitempty"`
	Delivered  []string `json:"delivered,omitempty"` // K<n> of the repo rules its reviewer received
	// Origins says from which repos (by name) each delivered id came.
	Origins map[string][]string `json:"origins,omitempty"`
}

// Review is the review of a finished feature.
type Review struct {
	Risk      Risk             `json:"risk"`
	RiskWhy   string           `json:"risk_why,omitempty"`
	Quick     bool             `json:"quick,omitempty"`
	Lenses    []LensRun        `json:"lenses,omitempty"`
	Returned  []string         `json:"returned,omitempty"` // agent ids that already stopped
	Criteria  []CriterionCheck `json:"criteria,omitempty"`
	Decisions []DecisionCheck  `json:"decisions,omitempty"`
	Findings  []Finding        `json:"findings,omitempty"`
	Refuted   bool             `json:"refuted,omitempty"`
	Chosen    []string         `json:"chosen,omitempty"`
	FullPath  string           `json:"full_path,omitempty"`
	Warning   string           `json:"warning,omitempty"` // invalid user/repo lens files, ignored
}

// Pick records whether the user chose to fix one offered finding.
type Pick struct {
	Finding  string   `json:"finding"`
	Lens     string   `json:"lens"`
	Severity Severity `json:"severity"`
	Chosen   bool     `json:"chosen"`
}

// LensStat is what a lens did for a repo in a period: findings the user chose and ignored, and
// escapes (problems it missed).
type LensStat struct {
	Lens    string
	Chosen  int
	Ignored int
	Escapes int
}
