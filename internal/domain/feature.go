package domain

// Feature is one user request moving through the flow. It is stored as JSON so
// later phases add fields without a migration.
type Feature struct {
	ID         string   `json:"id"`
	Repo       string   `json:"repo"`
	ProjectDir string   `json:"project_dir"`
	Request    string   `json:"request"` // the user's words, verbatim
	State      State    `json:"state"`
	Next       Action   `json:"next"`
	Plan       *Plan    `json:"plan,omitempty"`
	PlanRev    int      `json:"plan_rev"`
	Answers    []Answer `json:"answers,omitempty"`
	// ChangeNotes are the user's literal notes asking to change the plan.
	ChangeNotes []string `json:"change_notes,omitempty"`
	// Proof is the evidence of the batch being implemented.
	Proof *Proof `json:"proof,omitempty"`
	// Base is the HEAD sha at approval; the changed files are counted from it.
	Base string `json:"base,omitempty"`
	// Affected is the last run of the affected tests.
	Affected *AffectedRun `json:"affected,omitempty"`
	// Review is the state of the review of the finished feature.
	Review *Review `json:"review,omitempty"`
	// Background is the role of the nxy agent launched in the background whose end has not been handled.
	Background string `json:"background,omitempty"`
	// Undelivered is true while Next still has to reach the main thread.
	Undelivered bool `json:"undelivered,omitempty"`
}
