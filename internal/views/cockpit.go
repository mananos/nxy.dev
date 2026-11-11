package views

// Cockpit is the first screen: every active feature and the worktrees.
type Cockpit struct {
	GeneratedAt string           `json:"generated_at"`
	Totals      CockpitTotals    `json:"totals"`
	Features    []CockpitFeature `json:"features"`
	Worktrees   []Worktree       `json:"worktrees"`
}

type CockpitTotals struct {
	Active       int     `json:"active"`
	NeedsYou     int     `json:"needs_you"`
	CostTodayUSD float64 `json:"cost_today_usd"`
}

type CockpitFeature struct {
	FeatureHeader
	Running   []int        `json:"running"`
	Failed    []int        `json:"failed"`
	LastEvent string       `json:"last_event"`
	TokensIn  int64        `json:"tokens_in"`
	TokensOut int64        `json:"tokens_out"`
	Decision  *DecisionRef `json:"decision"`
	Actions   []Action     `json:"actions"`
}

type DecisionRef struct {
	FeatureID string `json:"feature_id"`
	Header    string `json:"header"`
	Question  string `json:"question"`
}

type Worktree struct {
	Repo      string `json:"repo"`
	Path      string `json:"path"`
	Branch    string `json:"branch"`
	FeatureID string `json:"feature_id"`
	State     string `json:"state"` // active|paused|orphan
	Dirty     bool   `json:"dirty"`
	Ahead     int    `json:"ahead"`
	Behind    int    `json:"behind"`
}
