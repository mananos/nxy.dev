package views

// File is the screen of one file: what the feature changed in it, who uses it and the memory
// anchored to it. Path is relative to Repo.
type File struct {
	Path       string        `json:"path"`
	Repo       string        `json:"repo"`
	Source     string        `json:"source"` // codegraph|name|none
	Language   string        `json:"language"`
	EditedBy   string        `json:"edited_by"`  // "lo editó el lote 3"
	DiffLabel  string        `json:"diff_label"` // "diff del lote 3"
	Hunks      []Hunk        `json:"hunks"`
	Content    []string      `json:"content"` // the whole file, for the v key
	Notes      []FileNote    `json:"notes"`
	ImportedBy []FileRef     `json:"imported_by"`
	Tests      []TestRef     `json:"tests"`
	Rules      []MemRef      `json:"rules"`
	Learnings  []MemRef      `json:"learnings"`
	Features   []FileFeature `json:"features"`
}

// Hunk is a block of the diff; OldStart and NewStart are the first lines of each side.
type Hunk struct {
	OldStart int        `json:"old_start"`
	NewStart int        `json:"new_start"`
	Lines    []DiffLine `json:"lines"`
}

// DiffLine is one line of a hunk. Old is its number on the old side (0 for an added line) and
// New on the new side (0 for a deleted one).
type DiffLine struct {
	Kind string `json:"kind"` // context|add|del
	Old  int    `json:"old"`
	New  int    `json:"new"`
	Text string `json:"text"`
}

// Diff line kinds.
const (
	DiffContext = "context"
	DiffAdd     = "add"
	DiffDel     = "del"
)

// FileNote is a remark pinned to a line: a failing test or an observation.
type FileNote struct {
	Line int    `json:"line"`
	Kind string `json:"kind"` // fail|info
	Text string `json:"text"`
}

// FileRef points at a line of another file.
type FileRef struct {
	Path string `json:"path"`
	Line int    `json:"line"`
}

// FileFeature is a feature that touched the file.
type FileFeature struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Batch    int    `json:"batch"`
	State    string `json:"state"` // pending|running|failed|closed
	ClosedAt string `json:"closed_at"`
}
