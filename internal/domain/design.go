package domain

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

// Fork is one open question of the design ("B1").
type Fork struct {
	ID      string     `json:"id"`
	Title   string     `json:"title"`
	Status  ForkStatus `json:"status"`
	Source  ForkSource `json:"source,omitempty"`
	Options []string   `json:"options,omitempty"`
}

// DesignClose is what the main thread proposes when closing the design.
type DesignClose struct {
	Rules    []Rule `json:"rules"`
	D2       string `json:"d2,omitempty"`
	Delegate bool   `json:"delegate,omitempty"`
}

// Design is the debate of an engineer feature, kept in the db as it happens.
type Design struct {
	Agenda    []Fork       `json:"agenda,omitempty"`
	Decisions []Decision   `json:"decisions,omitempty"`
	Pending   *Decision    `json:"pending,omitempty"`
	Close     *DesignClose `json:"close,omitempty"`
	Rules     []Rule       `json:"rules,omitempty"`
	D2        string       `json:"d2,omitempty"`
	Delegate  bool         `json:"delegate,omitempty"`
	Refs      []string     `json:"refs,omitempty"`
	Closed    bool         `json:"closed,omitempty"`
}
