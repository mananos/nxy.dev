// Package legacy turns what the 1.x knows about a repo (rows of its memory.db and
// review.jsonl) into values of the v2 memory. Pure: reading the 1.x files and
// writing the v2 db belong to the adapters.
package legacy

import "github.com/mananos/nxy.dev/internal/memory"

// Memory is a row of the 1.x memories table (core/memory/store.mjs). Times are
// Unix milliseconds.
type Memory struct {
	ID      string
	Scope   string // global | project | area
	Project string
	Area    string
	Type    string // convention | decision | bug | preference | handoff | ...
	Title   string
	Body    string
	Private bool // never leaves the machine on export in the 1.x
	Created int64
	Updated int64
}

// Edge is a row of the 1.x edges table. File edges point at "file:<path>".
type Edge struct {
	Src, Dst, Kind string
}

// Source kinds: which slice of Import a row went to.
const (
	SourceLearning = "learning"
	SourceFeature  = "feature"
	SourceNote     = "note"
)

// Source ties an imported value to the id it had in the 1.x. The Meta.ID of the
// values stays empty: the store derives a deterministic ULID from LegacyID.
type Source struct {
	Kind     string
	Index    int // position in the slice of its kind
	LegacyID string
}

// Import is what a repo's 1.x memory becomes.
type Import struct {
	Learnings []memory.Learning
	Features  []memory.ClosedFeature
	Notes     []memory.Note
	Sources   []Source
	Counts    Counts
}

// Pick is one finding a 1.x review offered, and whether the user chose it.
type Pick struct {
	FeatureID string
	Finding   string
	Repo      string
	Lens      string
	Severity  string
	Chosen    bool
	CreatedAt int64
}
