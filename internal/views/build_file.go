package views

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/memory"
	"github.com/mananos/nxy.dev/v2/internal/repomap"
	"github.com/mananos/nxy.dev/v2/internal/textdiff"
)

// FileIn is what the screen of one file needs. Before and After are nil when the file is absent at
// the base or in the tree; After is already capped by the caller. Rules, Learnings and Closed are
// the memory anchored to the file; Active are the other unfinished features of the same repo.
type FileIn struct {
	FeatureIn
	Repo, Path string
	Before     *string
	After      *string
	Binary     bool
	TooBig     bool // a side passed the cap: its diff would be false, so none is made
	ImportedBy []FileRef
	Tests      []TestRef
	Source     string
	Rules      []memory.Rule
	Learnings  []memory.Learning
	Closed     []memory.ClosedFeature
	Active     []domain.Feature
}

var languages = map[string]string{
	".java": "java", ".go": "go", ".ts": "typescript", ".tsx": "typescript",
	".js": "javascript", ".jsx": "javascript", ".mjs": "javascript", ".cjs": "javascript",
	".py": "python", ".scala": "scala", ".cs": "csharp",
}

// failNoteRe finds "path:line" in the output of a failing test.
var failNoteRe = regexp.MustCompile(`((?:[A-Za-z]:)?[^\s:()"'\[\]]+):(\d+)`)

const maxNoteLen = 200

// BuildFile builds the screen of one file of a feature.
func BuildFile(in FileIn) File {
	f := in.Feature
	src := in.Source
	if src == "" {
		src = "none"
	}
	out := File{
		Path: in.Path, Repo: in.Repo, Source: src, Language: languages[strings.ToLower(repomap.Ext(in.Path))],
		DiffLabel: "diff de la feature", Hunks: []Hunk{}, Content: []string{}, Notes: []FileNote{},
		ImportedBy: append([]FileRef{}, in.ImportedBy...), Tests: append([]TestRef{}, in.Tests...),
		Rules: []MemRef{}, Learnings: []MemRef{}, Features: []FileFeature{},
	}
	if in.Binary {
		in.Before, in.After = nil, nil
	}

	names := RepoNames(f)
	touched := batchesTouching(f, names, in.Repo, in.Path)
	switch len(touched) {
	case 0:
	case 1:
		out.EditedBy = "lo edita el lote " + strconv.Itoa(touched[0])
	default:
		out.EditedBy = "lo editan los lotes " + domain.JoinInts(touched, "y")
	}

	if in.TooBig {
		out.DiffLabel = "archivo demasiado grande para mostrar el diff"
	}
	if !in.Binary {
		if !in.TooBig {
			out.Hunks = diffHunks(in.Before, in.After)
		}
		if in.After != nil {
			out.Content = splitText(*in.After)
		}
	}
	out.Notes = failNotes(f, in.Path)

	for _, r := range memory.RulesFor(in.Rules, []string{in.Path}) {
		if len(r.Paths) == 0 {
			continue
		}
		id := r.Short
		if id == "" {
			id = r.ID
		}
		out.Rules = append(out.Rules, MemRef{ID: id, Kind: "rule", Text: r.Text})
	}
	for _, l := range in.Learnings {
		id := l.Short
		if id == "" {
			id = l.ID
		}
		out.Learnings = append(out.Learnings, MemRef{ID: id, Kind: "learning", Text: l.Text})
	}

	for _, a := range append([]domain.Feature{f}, in.Active...) {
		if a.ID == f.ID && a.ID != "" && len(out.Features) > 0 && out.Features[0].ID == f.ID {
			continue // the viewed feature came first; do not repeat it
		}
		ns := batchesTouching(a, RepoNames(a), in.Repo, in.Path)
		if len(ns) == 0 {
			continue
		}
		state := touchState(a, ns)
		out.Features = append(out.Features, FileFeature{ID: a.ID, Title: titleOf(a), Batch: ns[0], State: state})
	}
	for _, c := range in.Closed {
		out.Features = append(out.Features, FileFeature{ID: c.ID, Title: c.Title, State: "closed", ClosedAt: rfc(c.CreatedAt)})
	}
	return out
}

// batchesTouching lists the batches of f whose changes hit repo/path, in plan order.
func batchesTouching(f domain.Feature, names []string, repo, path string) []int {
	var out []int
	if f.Plan == nil {
		return out
	}
	for _, pb := range f.Plan.Batches {
		for _, c := range pb.Change {
			r, p, _, _ := ParseAt(c.At, names)
			r = RepoOfChange(f, pb, r)
			if p == path && (r == "" || repo == "" || r == repo) {
				out = append(out, pb.N)
				break
			}
		}
	}
	return out
}

// touchState is what a feature is doing to a file, by the steps of the batches that touch it: failed
// if any failed, running if any started, pending while none has (still in design, plan or approval).
func touchState(f domain.Feature, batches []int) string {
	state := "pending"
	for _, n := range batches {
		for _, st := range f.State.Steps {
			if st.N != n {
				continue
			}
			if st.Status == domain.StepFailed {
				return "failed"
			}
			if st.Launched || st.Status != domain.StepPending {
				state = "running"
			}
		}
	}
	return state
}

func lfOf(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// splitText is the lines of s without the empty one a final newline leaves.
func splitText(s string) []string {
	s = lfOf(s)
	if s == "" {
		return []string{}
	}
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func diffHunks(before, after *string) []Hunk {
	b, a := "", ""
	if before != nil {
		b = lfOf(*before)
	}
	if after != nil {
		a = lfOf(*after)
	}
	out := []Hunk{}
	for _, h := range textdiff.Hunks(textdiff.DiffLines(b, a), 3) {
		vh := Hunk{OldStart: h.OldStart, NewStart: h.NewStart, Lines: []DiffLine{}}
		if before == nil {
			vh.OldStart = 0
		}
		if after == nil {
			vh.NewStart = 0
		}
		o, n := h.OldStart, h.NewStart
		for _, op := range h.Ops {
			switch op.Op {
			case '+':
				vh.Lines = append(vh.Lines, DiffLine{Kind: DiffAdd, New: n, Text: op.Line})
				n++
			case '-':
				vh.Lines = append(vh.Lines, DiffLine{Kind: DiffDel, Old: o, Text: op.Line})
				o++
			default:
				vh.Lines = append(vh.Lines, DiffLine{Kind: DiffContext, Old: o, New: n, Text: op.Line})
				o++
				n++
			}
		}
		out = append(out, vh)
	}
	return out
}

// failNotes pins the lines of failing output that name the file ("path:line") on it.
func failNotes(f domain.Feature, path string) []FileNote {
	out := []FileNote{}
	seen := map[string]bool{}
	scan := func(output string) {
		for _, raw := range strings.Split(lfOf(output), "\n") {
			for _, m := range failNoteRe.FindAllStringSubmatch(raw, -1) {
				p := strings.TrimPrefix(strings.ReplaceAll(m[1], "\\", "/"), "./")
				if p != path && !strings.HasSuffix(p, "/"+path) {
					continue
				}
				line, _ := strconv.Atoi(m[2])
				text := clip(strings.TrimSpace(raw), maxNoteLen)
				if k := m[2] + "\x00" + text; !seen[k] {
					seen[k] = true
					out = append(out, FileNote{Line: line, Kind: "fail", Text: text})
				}
				break
			}
		}
	}
	for _, st := range f.State.Steps {
		if st.Status == domain.StepFailed {
			scan(f.ProofOf(st.N).Failure)
		}
	}
	if f.Affected != nil {
		scan(f.Affected.Failure)
	}
	return out
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
