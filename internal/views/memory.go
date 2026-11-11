package views

// Memory scopes.
const (
	ScopeGlobal = "global"
	ScopeRepo   = "repo"
)

// Memory is the memory screen of one scope: a repo or the user's global rules.
type Memory struct {
	Scope     string          `json:"scope"` // global|repo
	Repo      RepoRef         `json:"repo"`  // global: only the name, "global"
	Rules     []Rule          `json:"rules"`
	Learnings []Learning      `json:"learnings"`
	Closed    []ClosedFeature `json:"closed"`
	Proposals []Proposal      `json:"proposals"`
	// Create makes a new rule in this scope. In a repo it is the nxy mem rule that exists;
	// global rules are a plan of their own.
	Create Action `json:"create"`
}

// Rule flags: the view says what is odd about a rule, the TUI has no thresholds.
const (
	FlagUncited = "uncited" // delivered many times and never cited
	FlagLegacy  = "legacy"  // brought from the 1.x
)

// Rule is a rule (K<n>) of a repo or a global one.
type Rule struct {
	ID        string   `json:"id"`
	Text      string   `json:"text"`
	Question  string   `json:"question"`
	Paths     []string `json:"paths"`
	Origin    string   `json:"origin"`     // closing|chat|bootstrap|legacy
	OriginRef string   `json:"origin_ref"` // the feature of a closing
	CreatedAt string   `json:"created_at"` // RFC3339
	Flag      string   `json:"flag"`       // uncited|legacy or empty
	Cited     int      `json:"cited"`
	LastCited string   `json:"last_cited"` // "F7K2 lote 2"
	Delivered int      `json:"delivered"`
	UpdatedAt string   `json:"updated_at"`
	// Roles a global rule reaches; empty is every role.
	Roles []string `json:"roles"`
	// Actions are edit and delete (ids "edit", "delete"); both Planned.
	Actions []Action `json:"actions"`
}

// Learning flags.
const (
	FlagStale = "stale" // its files are gone
	FlagSeen  = "seen"  // seen in several features
)

// Learning is a learning (A<n>) not yet promoted to a rule.
type Learning struct {
	ID        string   `json:"id"`
	Text      string   `json:"text"`
	Files     []string `json:"files"`
	Seen      int      `json:"seen"`
	Stale     bool     `json:"stale"`
	Flag      string   `json:"flag"`     // stale|seen or empty
	Features  []string `json:"features"` // where it was seen
	Delivered int      `json:"delivered"`
	Actions   []Action `json:"actions"`
}

// ClosedFeature is a delivered feature and the files it touched.
type ClosedFeature struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	ClosedAt string   `json:"closed_at"`
	Outcome  string   `json:"outcome"`
	Files    []string `json:"files"`
}

// Proposal is a memory cleanup the user can accept or reject.
type Proposal struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"` // delete|promote|merge
	Target string `json:"target"`
	Text   string `json:"text"`
	Why    string `json:"why"`
	Accept Action `json:"accept"`
	Reject Action `json:"reject"`
}
