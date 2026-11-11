package domain

import "encoding/json"

// ForkStatus is where a fork of the design stands.
type ForkStatus string

const (
	ForkOpen      ForkStatus = "open"
	ForkDecided   ForkStatus = "decided"
	ForkDelegated ForkStatus = "delegated"
)

// ForkSource says who raised a fork.
type ForkSource string

const (
	SourceAgenda  ForkSource = "agenda"
	SourcePlanner ForkSource = "planner"
	SourceOffer   ForkSource = "offer"
)

// ForkOption is one way out of a fork: what it is, its trade-offs and what it touches.
type ForkOption struct {
	Key     string   `json:"key,omitempty"`
	Label   string   `json:"label"`
	Summary string   `json:"summary,omitempty"`
	Pros    []string `json:"pros,omitempty"`
	Cons    []string `json:"cons,omitempty"`
	Touches string   `json:"touches,omitempty"`
	D2      string   `json:"d2,omitempty"`
}

// UnmarshalJSON also reads a bare JSON string as the label: forks saved when Options was []string.
func (o *ForkOption) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*o = ForkOption{Label: s}
		return nil
	}
	type plain ForkOption
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*o = ForkOption(p)
	return nil
}

// Fork is one open question of the design ("B1").
type Fork struct {
	ID       string       `json:"id"`
	Title    string       `json:"title"`
	Status   ForkStatus   `json:"status"`
	Source   ForkSource   `json:"source,omitempty"`
	Question string       `json:"question,omitempty"`
	Options  []ForkOption `json:"options,omitempty"`
	Opinion  string       `json:"opinion,omitempty"`
}

// DesignClose is what the main thread proposes when closing the design.
type DesignClose struct {
	Criteria []Criterion `json:"criteria"`
	D2       string      `json:"d2,omitempty"`
	Delegate bool        `json:"delegate,omitempty"`
}

// Design is the debate of an engineer feature, kept in the db as it happens.
type Design struct {
	Agenda    []Fork       `json:"agenda,omitempty"`
	Decisions []Decision   `json:"decisions,omitempty"`
	Pending   *Decision    `json:"pending,omitempty"`
	Close     *DesignClose `json:"close,omitempty"`
	Criteria  []Criterion  `json:"criteria,omitempty"`
	D2        string       `json:"d2,omitempty"`
	Delegate  bool         `json:"delegate,omitempty"`
	Refs      []string     `json:"refs,omitempty"`
	Closed    bool         `json:"closed,omitempty"`
	Current   string       `json:"current,omitempty"` // fork being debated
}
