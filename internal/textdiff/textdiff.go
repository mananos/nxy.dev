package textdiff

import (
	"strconv"
	"strings"
)

// MaxLCSCells: past this many cells the LCS table costs more than it is worth.
const MaxLCSCells = 4_000_000

// Op is one line of a diff: ' ' unchanged, '-' removed, '+' added.
type Op struct {
	Op   byte
	Line string
}

// Hunk is a group of ops with their line numbers (1-based).
type Hunk struct {
	OldStart int
	OldLines int
	NewStart int
	NewLines int
	Ops      []Op
}

// Result is the unified diff text of one file plus counts and the new-file range of each hunk.
type Result struct {
	Text    string
	Added   int
	Removed int
	Ranges  [][2]int
}

func splitLines(text string) []string {
	if text == "" {
		return []string{}
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func lcs(a, b []string) []Op {
	n, m := len(a), len(b)
	w := m + 1
	t := make([]uint32, (n+1)*w)
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				t[i*w+j] = t[(i+1)*w+j+1] + 1
			} else {
				x, y := t[(i+1)*w+j], t[i*w+j+1]
				if y > x {
					x = y
				}
				t[i*w+j] = x
			}
		}
	}
	out := []Op{}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, Op{' ', a[i]})
			i++
			j++
		case t[(i+1)*w+j] >= t[i*w+j+1]:
			out = append(out, Op{'-', a[i]})
			i++
		default:
			out = append(out, Op{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, Op{'-', a[i]})
	}
	for ; j < m; j++ {
		out = append(out, Op{'+', b[j]})
	}
	return out
}

// DiffLines is the line diff of before to after.
func DiffLines(before, after string) []Op {
	a := splitLines(before)
	b := splitLines(after)
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	A := a[p : len(a)-s]
	B := b[p : len(b)-s]
	var mid []Op
	if len(A)*len(B) > MaxLCSCells {
		mid = make([]Op, 0, len(A)+len(B))
		for _, l := range A {
			mid = append(mid, Op{'-', l})
		}
		for _, l := range B {
			mid = append(mid, Op{'+', l})
		}
	} else {
		mid = lcs(A, B)
	}
	out := make([]Op, 0, p+len(mid)+s)
	for _, l := range a[:p] {
		out = append(out, Op{' ', l})
	}
	out = append(out, mid...)
	for _, l := range a[len(a)-s:] {
		out = append(out, Op{' ', l})
	}
	return out
}

// Hunks groups a diff into hunks with `context` unchanged lines around each change.
func Hunks(ops []Op, context int) []Hunk {
	out := []Hunk{}
	var ranges [][2]int
	for i, o := range ops {
		if o.Op == ' ' {
			continue
		}
		from := max(0, i-context)
		to := min(len(ops)-1, i+context)
		if n := len(ranges); n > 0 && from <= ranges[n-1][1]+1 {
			ranges[n-1][1] = max(ranges[n-1][1], to)
		} else {
			ranges = append(ranges, [2]int{from, to})
		}
	}
	if len(ranges) == 0 {
		return out
	}
	oldAt := make([]int, len(ops))
	newAt := make([]int, len(ops))
	oldNo, newNo := 1, 1
	for i, o := range ops {
		oldAt[i], newAt[i] = oldNo, newNo
		if o.Op != '+' {
			oldNo++
		}
		if o.Op != '-' {
			newNo++
		}
	}
	for _, r := range ranges {
		slice := ops[r[0] : r[1]+1]
		h := Hunk{OldStart: oldAt[r[0]], NewStart: newAt[r[0]], Ops: slice}
		for _, o := range slice {
			if o.Op != '+' {
				h.OldLines++
			}
			if o.Op != '-' {
				h.NewLines++
			}
		}
		out = append(out, h)
	}
	return out
}

// FileDiff is the unified diff of one file; a nil before or after is a new or deleted file.
func FileDiff(label string, before, after *string) Result {
	b, a := "", ""
	if before != nil {
		b = *before
	}
	if after != nil {
		a = *after
	}
	ops := DiffLines(b, a)
	hs := Hunks(ops, 3)
	res := Result{Ranges: make([][2]int, 0, len(hs))}
	for _, o := range ops {
		switch o.Op {
		case '+':
			res.Added++
		case '-':
			res.Removed++
		}
	}
	for _, h := range hs {
		res.Ranges = append(res.Ranges, [2]int{h.NewStart, h.NewStart + max(h.NewLines, 1) - 1})
	}
	if len(hs) == 0 {
		return res
	}
	oldName, newName := "/dev/null", "/dev/null"
	if before != nil {
		oldName = "a/" + label
	}
	if after != nil {
		newName = "b/" + label
	}
	lines := []string{"--- " + oldName, "+++ " + newName}
	for _, h := range hs {
		lines = append(lines, "@@ -"+strconv.Itoa(h.OldStart)+","+strconv.Itoa(h.OldLines)+" +"+strconv.Itoa(h.NewStart)+","+strconv.Itoa(h.NewLines)+" @@")
		for _, o := range h.Ops {
			lines = append(lines, string(rune(o.Op))+o.Line)
		}
	}
	res.Text = strings.Join(lines, "\n")
	return res
}
