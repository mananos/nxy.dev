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
	// Mode is who decides the design: the planner (delegate) or the user (engineer).
	Mode Mode `json:"mode,omitempty"`
	// Design is the design debate of an engineer feature.
	Design *Design `json:"design,omitempty"`
	// Docs are the docs of the repo that name what the feature changed, found when it closes.
	Docs *Docs `json:"docs,omitempty"`
	// OfferAnswered is true once the user answered the "Pasar a engineer" offer.
	OfferAnswered bool `json:"offer_answered,omitempty"`
	// Harvested are the ids of the implementer agents whose learnings were already saved.
	Harvested []string `json:"harvested,omitempty"`
	// Candidates are the rule candidates raised while the feature runs (review escapes, chat);
	// RuleOffer is what the closing question offered (S1..).
	Candidates []RuleCandidate `json:"candidates,omitempty"`
	RuleOffer  []RuleCandidate `json:"rule_offer,omitempty"`
}

// RuleCandidate is a convention that may become a rule of the repo when the feature closes.
type RuleCandidate struct {
	ID             string `json:"id,omitempty"` // "S1", set when it is offered
	Text           string `json:"text"`
	Question       string `json:"question"`
	Origin         string `json:"origin"`
	Lens           string `json:"lens,omitempty"`
	Supersedes     string `json:"supersedes,omitempty"`      // K<n> of the rule it would replace
	SupersedesText string `json:"supersedes_text,omitempty"` // that rule's text, for the question
}

// Mode says how a feature is run.
type Mode string

const (
	ModeDelegate Mode = ""
	ModeEngineer Mode = "engineer"
)
