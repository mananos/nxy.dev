package domain

// AgentRun is what one subagent cost and how long it took, filled once at SubagentStop.
// Times are epoch milliseconds, like the rest of the db.
type AgentRun struct {
	FeatureID     string
	Batch         int // 0 when the agent did not work on a batch
	AgentID       string
	Role          string
	Model         string
	Effort        string
	Calls         int
	TokensIn      int64
	TokensOut     int64
	CacheRead     int64
	CacheWrite    int64
	CostUSD       float64
	CostEstimated bool
	StartedAt     int64
	EndedAt       int64
}
