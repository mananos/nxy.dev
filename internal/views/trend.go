package views

// Trend is the trend screen: usage over time, by model and by agent role.
type Trend struct {
	Scope  string      `json:"scope"`
	Range  TrendRange  `json:"range"`
	Totals TrendTotals `json:"totals"`
	Days   []PeriodRow `json:"days"`  // the last 7 days
	Weeks  []PeriodRow `json:"weeks"` // the last 8 weeks
	Models []ModelRow  `json:"models"`
	Agents []AgentRow  `json:"agents"`
}

// TrendRange is the span the days cover (dates, YYYY-MM-DD).
type TrendRange struct {
	Since string `json:"since"`
	Until string `json:"until"`
}

// TrendTotals is the sum over the whole range.
type TrendTotals struct {
	Sessions int     `json:"sessions"`
	Turns    int     `json:"turns"`
	Tokens   int64   `json:"tokens"`
	USD      float64 `json:"usd"`
}

// PeriodRow is one day or week.
type PeriodRow struct {
	Label       string  `json:"label"`
	Sessions    int     `json:"sessions"`
	Turns       int     `json:"turns"`
	Tokens      int64   `json:"tokens"`
	USD         float64 `json:"usd"`
	CacheHitPct float64 `json:"cache_hit_pct"`
}

// AgentRow is the cost of one subagent role.
type AgentRow struct {
	Role  string  `json:"role"`
	Runs  int     `json:"runs"`
	AvgMs int64   `json:"avg_ms"`
	USD   float64 `json:"usd"`
}
