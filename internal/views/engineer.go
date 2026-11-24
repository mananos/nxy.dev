package views

// Fork statuses of the agenda of the engineer mode.
const (
	ForkDecided = "decided"
	ForkCurrent = "current"
	ForkPending = "pending"
)

// Diagram is a D2 diagram already drawn as text. Where it is rendered for real (F5.2): the view
// builder calls internal/tools with `d2 --stdout-format txt` and caches the lines by source hash
// plus d2 version, so a 500 ms refresh does not redraw. Without d2 installed Lines is empty and
// the TUI shows Source with a hint to install it from Config. In the demo the lines are written
// by hand and Renderer is "demo".
type Diagram struct {
	Source   string   `json:"source"`   // d2 code
	Lines    []string `json:"lines"`    // text already drawn
	Renderer string   `json:"renderer"` // "d2 0.7.1 txt" or "demo"
	Problem  string   `json:"problem"`  // why there are no lines: missing|error|slow; empty with a drawing
	Error    string   `json:"error"`    // the message of d2 when Problem is error
}

// EngFork is one fork of the design agenda.
type EngFork struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"` // decided|current|pending
	Choice string `json:"choice"` // what was decided, when decided
}

// EngOption is one way to resolve the current fork.
type EngOption struct {
	Key     string   `json:"key"` // "A"
	Label   string   `json:"label"`
	Summary string   `json:"summary"`
	Pros    []string `json:"pros"`
	Cons    []string `json:"cons"`
	Touches string   `json:"touches"`
	Diagram Diagram  `json:"diagram"`
	Action  Action   `json:"action"`
}

// Engineer is the engineer mode: the design forks of a feature and the options of the current
// one. No option comes chosen and Opinion stays hidden until the user asks for it.
type Engineer struct {
	Feature  FeatureHeader `json:"feature"`
	Agenda   []EngFork     `json:"agenda"`
	Current  string        `json:"current"` // ID of the current fork
	AskID    string        `json:"ask_id"`  // the pending ask of the fork question; empty when none waits
	Question string        `json:"question"`
	Options  []EngOption   `json:"options"`
	Opinion  string        `json:"opinion"`
	Picked   string        `json:"picked"` // label of the option the user saved with enter; empty when none
	Today    Diagram       `json:"today"`  // how the code is today
	Other    string        `json:"other"`
}
