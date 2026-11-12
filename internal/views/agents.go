package views

// Agents is the agent tree of a feature; the main thread is the root.
type Agents struct {
	Feature FeatureHeader `json:"feature"`
	Root    AgentNode     `json:"root"`
}

type AgentNode struct {
	ID            string      `json:"id"`
	Role          string      `json:"role"`
	Label         string      `json:"label"`
	Title         string      `json:"title"` // what the batch does ("Componente de nota parcial")
	Batch         int         `json:"batch"`
	Repo          string      `json:"repo"`
	Status        StepStatus  `json:"status"`
	Model         string      `json:"model"`
	Effort        string      `json:"effort"`
	After         []int       `json:"after"`
	WaitingOn     []int       `json:"waiting_on"` // already resolved
	Attempt       int         `json:"attempt"`
	Attempts      int         `json:"attempts"` // the most attempts the batch gets
	Failure       *Failure    `json:"failure"`
	CurrentTool   *ToolUse    `json:"current_tool"`
	Tools         []ToolCount `json:"tools"`
	TokensIn      int64       `json:"tokens_in"`
	TokensOut     int64       `json:"tokens_out"`
	CacheRead     int64       `json:"cache_read"`
	CacheWrite    int64       `json:"cache_write"`
	CacheHitPct   *float64    `json:"cache_hit_pct"`
	ElapsedMs     int64       `json:"elapsed_ms"` // -1 (UnknownMs) = no duration
	CostUSD       float64     `json:"cost_usd"`
	CostEstimated bool        `json:"cost_estimated"`
	Received      []MemRef    `json:"received"`
	Learned       []string    `json:"learned"` // plain text, without the "learned:" prefix
	Children      []AgentNode `json:"children"`
}

type ToolUse struct {
	Name string `json:"name"`
	Arg  string `json:"arg"`
}

type ToolCount struct {
	Name   string `json:"name"`
	Count  int    `json:"count"`
	Detail string `json:"detail"` // "npm test 3 veces"
}

// Failure is why a batch is red: the test and what it said.
type Failure struct {
	Test  string   `json:"test"`
	Lines []string `json:"lines"`
}
