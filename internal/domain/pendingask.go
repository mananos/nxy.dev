package domain

// AskedQuestion is one entry of the questions of an AskUserQuestion call (tool_input.questions).
type AskedQuestion struct {
	Question    string   `json:"question"`
	Header      string   `json:"header,omitempty"`
	MultiSelect bool     `json:"multiSelect,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// PendingAsk is a question of nxy held by the hook until the TUI (or the terminal) answers it.
// Answers is nil while nobody answered; AnsweredBy is "tui", "terminal" or "stopped".
type PendingAsk struct {
	ID         string          `json:"id"`
	SessionID  string          `json:"session_id"`
	FeatureID  string          `json:"feature_id"`
	ToolUseID  string          `json:"tool_use_id"`
	Questions  []AskedQuestion `json:"questions"`
	Answers    []string        `json:"answers,omitempty"`
	AnsweredBy string          `json:"answered_by,omitempty"`
	CreatedAt  int64           `json:"created_at"`
	AnsweredAt int64           `json:"answered_at,omitempty"`
}
