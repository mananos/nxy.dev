package memory

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// BundleVersion is the export format this nxy reads and writes.
const BundleVersion = 1

// Bundle is the versioned JSON export of the memory.
type Bundle struct {
	Version    int             `json:"version"`
	Machine    string          `json:"machine,omitempty"`
	ExportedAt int64           `json:"exported_at"`
	Rules      []Rule          `json:"rules"`
	Features   []ClosedFeature `json:"features"`
	Learnings  []Learning      `json:"learnings"`
	Notes      []Note          `json:"notes,omitempty"`
}

// EncodeBundle writes the bundle as indented JSON, rows ordered by ID. The
// input is not modified.
func EncodeBundle(b Bundle) ([]byte, error) {
	b.Version = BundleVersion
	b.Rules = sortedByID(b.Rules, func(r Rule) string { return r.ID })
	b.Features = sortedByID(b.Features, func(f ClosedFeature) string { return f.ID })
	b.Learnings = sortedByID(b.Learnings, func(l Learning) string { return l.ID })
	b.Notes = sortedByID(b.Notes, func(n Note) string { return n.ID })
	if b.Rules == nil {
		b.Rules = []Rule{}
	}
	if b.Features == nil {
		b.Features = []ClosedFeature{}
	}
	if b.Learnings == nil {
		b.Learnings = []Learning{}
	}
	out, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func sortedByID[T any](in []T, id func(T) string) []T {
	if in == nil {
		return nil
	}
	out := append([]T(nil), in...)
	sort.SliceStable(out, func(i, j int) bool { return id(out[i]) < id(out[j]) })
	return out
}

// DecodeBundle parses and validates an export.
func DecodeBundle(data []byte) (Bundle, error) {
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return Bundle{}, fmt.Errorf("not a nxy memory export: %v", err)
	}
	if b.Version != BundleVersion {
		return Bundle{}, fmt.Errorf("unsupported export version %d (this nxy reads %d)", b.Version, BundleVersion)
	}
	for i, r := range b.Rules {
		if r.ID == "" {
			return Bundle{}, fmt.Errorf("rule without id")
		}
		b.Rules[i].Short = cleanShort(r.Short, 'K')
		if r.Deleted() {
			continue
		}
		if strings.TrimSpace(r.Repo) == "" {
			return Bundle{}, fmt.Errorf("rule %s: a rule needs a repo", r.ID)
		}
		if err := ValidateRule(r); err != nil {
			return Bundle{}, fmt.Errorf("rule %s: %v", r.ID, err)
		}
	}
	for _, f := range b.Features {
		if f.ID == "" {
			return Bundle{}, fmt.Errorf("feature without id")
		}
		if !f.Deleted() && strings.TrimSpace(f.Title) == "" {
			return Bundle{}, fmt.Errorf("feature %s: a feature needs a title", f.ID)
		}
	}
	for i, l := range b.Learnings {
		if l.ID == "" {
			return Bundle{}, fmt.Errorf("learning without id")
		}
		b.Learnings[i].Short = cleanShort(l.Short, 'A')
		if l.Deleted() {
			continue
		}
		if strings.TrimSpace(l.Repo) == "" {
			return Bundle{}, fmt.Errorf("learning %s: a learning needs a repo", l.ID)
		}
		if strings.TrimSpace(l.Text) == "" {
			return Bundle{}, fmt.Errorf("learning %s: a learning needs a text", l.ID)
		}
		b.Learnings[i].Hash = LearningHash(l.Text, l.Files)
	}
	for i, n := range b.Notes {
		if n.ID == "" {
			return Bundle{}, fmt.Errorf("note without id")
		}
		b.Notes[i].Short = cleanShort(n.Short, 'N')
		if n.Deleted() {
			continue
		}
		if err := ValidateNote(n); err != nil {
			return Bundle{}, fmt.Errorf("note %s: %v", n.ID, err)
		}
	}
	return b, nil
}

// cleanShort keeps an alias of the form <letter><digits> and blanks anything
// else, so the import assigns a fresh one.
func cleanShort(s string, letter byte) string {
	if len(s) < 2 || s[0] != letter {
		return ""
	}
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return ""
		}
	}
	return s
}

// Snapshot is the local state Merge compares against, tombstones included.
type Snapshot struct {
	Rules     []Rule
	Features  []ClosedFeature
	Learnings []Learning
	Notes     []Note
	Aliases   []Alias
}

// Alias records that a foreign learning (by ULID) was folded into a local one.
// It is local state: never exported. UpdatedAt is the incoming row's, so the
// same bundle imported again is Skipped.
type Alias struct {
	ID        string // ULID of the foreign learning
	Into      string // ULID of the local learning that absorbed it
	UpdatedAt int64
	Seen      int // Seen of the incoming row when last folded: counting does not move UpdatedAt, so growth is read here
}

// ChangeKind is what importing a row does.
type ChangeKind string

const (
	New     ChangeKind = "new"     // unknown ULID (tombstones too, so a deletion does not revive)
	Changed ChangeKind = "changed" // known, incoming updated_at is newer
	Skipped ChangeKind = "skipped" // known, incoming is equal or older
	Folded  ChangeKind = "folded"  // learning duplicate of a live local one
)

// Change is one row of a Diff.
type Change[T any] struct {
	Kind          ChangeKind
	Row           T
	Deleted       bool   // the incoming row is a tombstone
	ReassignShort bool   // its Short collides with another local row: assign a new one
	FoldInto      string // Folded: ULID of the local learning that absorbs it
	Counters      bool   // Skipped: the incoming row has a larger counter than the local one (max is kept)
}

// Diff is the result of merging a bundle: what applying it would do.
type Diff struct {
	Rules     []Change[Rule]
	Features  []Change[ClosedFeature]
	Learnings []Change[Learning]
	Notes     []Change[Note]
}

// Count returns how many changes in the diff have the kind.
func (d Diff) Count(k ChangeKind) int {
	n := 0
	for _, c := range d.Rules {
		if c.Kind == k {
			n++
		}
	}
	for _, c := range d.Features {
		if c.Kind == k {
			n++
		}
	}
	for _, c := range d.Learnings {
		if c.Kind == k {
			n++
		}
	}
	for _, c := range d.Notes {
		if c.Kind == k {
			n++
		}
	}
	return n
}

// Writes counts the changes that touch the database.
func (d Diff) Writes() int {
	n := d.Count(New) + d.Count(Changed) + d.Count(Folded)
	for _, c := range d.Rules {
		if c.Counters {
			n++
		}
	}
	for _, c := range d.Learnings {
		if c.Counters {
			n++
		}
	}
	return n
}

type rowInfo[T any] struct {
	updated int64
	short   string
	row     T
}

func mergeRows[T any](local []T, in []T, id func(T) string, meta func(T) Meta, short func(T) string, counters func(local, in T) bool) []Change[T] {
	known := map[string]rowInfo[T]{}
	taken := map[string]string{} // short -> id
	for _, r := range local {
		known[id(r)] = rowInfo[T]{meta(r).UpdatedAt, short(r), r}
		if s := short(r); s != "" {
			taken[s] = id(r)
		}
	}
	var out []Change[T]
	for _, r := range sortedByID(in, id) {
		c := Change[T]{Row: r, Deleted: meta(r).Deleted()}
		if k, ok := known[id(r)]; ok {
			if meta(r).UpdatedAt > k.updated {
				c.Kind = Changed
			} else {
				c.Kind = Skipped
				c.Counters = counters != nil && !c.Deleted && counters(k.row, r)
			}
		} else {
			c.Kind = New
		}
		if c.Kind != Skipped {
			if s := short(r); s != "" {
				if owner, clash := taken[s]; clash && owner != id(r) {
					c.ReassignShort = true
				} else {
					taken[s] = id(r)
				}
			}
		}
		out = append(out, c)
	}
	return out
}

// Merge compares an incoming bundle with the local state, by ULID: unknown is
// New, known and newer is Changed, known and equal or older is Skipped. A
// learning with a new ULID but the same (Repo, Hash) as a live local one is
// Folded. Rows are listed by ID, so the result is deterministic.
func Merge(local Snapshot, in Bundle) Diff {
	var d Diff
	d.Rules = mergeRows(local.Rules, in.Rules,
		func(r Rule) string { return r.ID }, func(r Rule) Meta { return r.Meta }, func(r Rule) string { return r.Short },
		func(l, i Rule) bool {
			return i.Deliveries > l.Deliveries || i.Citations > l.Citations || i.LastCitedAt > l.LastCitedAt
		})
	d.Features = mergeRows(local.Features, in.Features,
		func(f ClosedFeature) string { return f.ID }, func(f ClosedFeature) Meta { return f.Meta }, func(ClosedFeature) string { return "" }, nil)
	d.Learnings = mergeRows(local.Learnings, in.Learnings,
		func(l Learning) string { return l.ID }, func(l Learning) Meta { return l.Meta }, func(l Learning) string { return l.Short },
		func(l, i Learning) bool { return i.Seen > l.Seen || i.LastSeenAt > l.LastSeenAt })
	d.Notes = mergeRows(local.Notes, in.Notes,
		func(n Note) string { return n.ID }, func(n Note) Meta { return n.Meta }, func(n Note) string { return n.Short }, nil)

	live := map[[2]string]string{} // (repo, hash) -> local id
	for _, l := range local.Learnings {
		if !l.Deleted() {
			live[[2]string{l.Repo, l.Hash}] = l.ID
		}
	}
	aliases := map[string]Alias{}
	for _, a := range local.Aliases {
		aliases[a.ID] = a
	}
	for i, c := range d.Learnings {
		if c.Kind != New {
			continue
		}
		if a, ok := aliases[c.Row.ID]; ok { // already folded once: only a newer row folds again
			d.Learnings[i].ReassignShort = false
			d.Learnings[i].FoldInto = a.Into
			if (c.Row.UpdatedAt > a.UpdatedAt || c.Row.Seen > a.Seen) && !c.Deleted {
				d.Learnings[i].Kind = Folded
			} else {
				d.Learnings[i].Kind = Skipped
			}
			continue
		}
		if c.Deleted {
			continue
		}
		if id, ok := live[[2]string{c.Row.Repo, c.Row.Hash}]; ok {
			d.Learnings[i].Kind = Folded
			d.Learnings[i].FoldInto = id
			d.Learnings[i].ReassignShort = false
		}
	}
	return d
}
