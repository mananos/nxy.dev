package legacy

// Cursor is how far a 1.x jsonl file was read: the byte offset after the last
// complete line, the file size seen then, and a hash of the first bytes of the
// file up to that offset, at most 4 KB (a change there means the file was
// rotated or rewritten).
type Cursor struct {
	Offset int64
	Size   int64
	Head   string
}

// FilterResult is what importing a slice of the 1.x filter.jsonl did.
type FilterResult struct {
	Applied  bool // false: the stored cursor was not "from", so another process went first
	Sessions int  // sessions that got groups
	V2       int  // sessions skipped because the v2 wrote them
	Rows     int  // filter rows that went in
}

// Mark is the record of an import done once (scope "memory" keyed by repo, "review" by checkout).
type Mark struct {
	Scope      string
	Key        string
	ImportedAt int64
	Counts     Counts
	Error      string // scope "memory-error": the last failure of the memory import (ImportedAt = when)
	Answer     string // scope "plan-1x" (ScopeHalfPlan): replan | ignore | closed, kept as plain text in the counts column
}

// ScopeMemoryError is the scope of the mark that keeps the last memory import failure per repo.
const ScopeMemoryError = "memory-error"

// ScopeMemoryGlobal is the scope of the mark (empty key) of the global memories, imported once per machine.
const ScopeMemoryGlobal = "memory-global"

// StoredCursor is a stored read position of a checkout's filter.jsonl.
type StoredCursor struct {
	Checkout  string
	Cursor    Cursor
	UpdatedAt int64
}
