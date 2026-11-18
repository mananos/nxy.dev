package views

import (
	"github.com/mananos/nxy.dev/v2/internal/pricing"
	"github.com/mananos/nxy.dev/v2/internal/report"
)

// Stats is the stats screen of one session. Usage and Savings are reused from
// pricing and report (flat structs, camelCase tags inherited from the 1.x);
// everything else is snake_case.
type Stats struct {
	SessionID    string          `json:"session_id"`
	Project      string          `json:"project"`
	Model        string          `json:"model"`
	Turns        int             `json:"turns"`
	Calls        int             `json:"calls"`
	MainCalls    int             `json:"main_calls"`
	Usage        pricing.Usage   `json:"usage"`
	USD          float64         `json:"usd"`
	USDEstimated bool            `json:"usd_estimated"`
	CacheHitPct  float64         `json:"cache_hit_pct"`
	Shape        Shape           `json:"shape"`
	ByModel      []ModelRow      `json:"by_model"`
	Filter       FilterRow       `json:"filter"`
	Broad        BroadRow        `json:"broad"`
	Rtk          *report.Savings `json:"rtk"`
	CacheTTL     string          `json:"cache_ttl"`
	Sessions     []SessionRef    `json:"sessions"` // the other sessions of the repo, for `s`
}

// SessionRef is a session the user can switch to.
type SessionRef struct {
	ID        string  `json:"id"`
	Label     string  `json:"label"`
	StartedAt string  `json:"started_at"`
	USD       float64 `json:"usd"`
}

// Shape says how the session spends its calls.
type Shape struct {
	CallsPerTurn     float64 `json:"calls_per_turn"`
	MainCallsPerTurn float64 `json:"main_calls_per_turn"`
	MainCtxAvg       float64 `json:"main_ctx_avg"`
	SubagentSharePct float64 `json:"subagent_share_pct"`
}

// ModelRow is one model's share of calls and cost.
type ModelRow struct {
	Model       string        `json:"model"`
	Calls       int           `json:"calls"`
	Usage       pricing.Usage `json:"usage"`
	USD         float64       `json:"usd"`
	CacheHitPct float64       `json:"cache_hit_pct"`
	Estimated   bool          `json:"estimated"`
}

// FilterRow summarizes the output filter.
type FilterRow struct {
	Rows      int     `json:"rows"`
	ByEngine  []Count `json:"by_engine"`
	EstTokens float64 `json:"est_tokens"`
}

// Count is a named counter.
type Count struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// BroadRow summarizes broad searches caught by the read gate.
type BroadRow struct {
	Count         int `json:"count"`
	ContextTokens int `json:"context_tokens"`
	Threshold     int `json:"threshold"`
}
