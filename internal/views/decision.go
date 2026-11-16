package views

// Decision is a pending question for the user. There is one per feature. Header, Question,
// MultiSelect and Options are the first question of the ask; Rest holds the others.
type Decision struct {
	ID          string   `json:"id"`
	FeatureID   string   `json:"feature_id"`
	Repo        string   `json:"repo"`
	Kind        string   `json:"kind"` // an ask.Kind (approval, questions, batch_failed, repo_clash, branch, findings, docs, rules...) or fork
	Title       string   `json:"title"`
	Body        []string `json:"body"` // the block shown before the question, one line per entry
	Header      string   `json:"header"`
	Question    string   `json:"question"`
	Context     []string `json:"context"`
	MultiSelect bool     `json:"multi_select"`
	AskedAt     string   `json:"asked_at"`
	Fork        *ForkRef `json:"fork"` // only with Kind "fork": it is read here and answered in Ingeniero
	// AskID is the pending ask the hook holds for this question; empty when none is waiting, and then
	// the options have no argv and the question is answered in the Claude Code terminal.
	AskID string `json:"ask_id"`
	// Release hands the question back to the Claude Code dialog (decide --release); empty argv without AskID.
	Release Action   `json:"release"`
	Options []Option `json:"options"`
	// Rest are questions 2..n of a multi-question ask.
	Rest []DecisionQuestion `json:"rest"`
}

// DecisionQuestion is one more question of a multi-question Decision.
type DecisionQuestion struct {
	Header      string   `json:"header"`
	Question    string   `json:"question"`
	MultiSelect bool     `json:"multi_select"`
	Options     []Option `json:"options"`
}

// ForkRef says which design fork a question is: "2 of 4".
type ForkRef struct {
	N     int    `json:"n"`
	Of    int    `json:"of"`
	Title string `json:"title"`
}

type Option struct {
	Label       string `json:"label"`
	Description string `json:"description"`
	Recommended bool   `json:"recommended"`
	Action      Action `json:"action"`
}
