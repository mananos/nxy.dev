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
}
