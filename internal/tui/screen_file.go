package tui

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/mananos/nxy.dev/internal/views"
)

func init() { register(fileScreen{}) }

type fileScreen struct{}

func (fileScreen) id() string                     { return "archivo" }
func (fileScreen) section() string                { return "inicio" }
func (fileScreen) crumb(_ *state, r route) string { return path.Base(r.key) }

func (fileScreen) keys(st *state, r route) []hint {
	v := hint{"v", "archivo entero"}
	if st.fileFull(r) {
		v = hint{"v", "diff"}
	}
	return []hint{{"n N", "otro cambio"}, v, {"o", "editor"}, {"enter", "importador"}, {"↑↓", "importador"}}
}

func (st *state) fileFull(r route) bool { return st.cur[r.ckey()+"#v"] == 1 }

// featureOfFile is the feature whose plan touches a file ("repo/path"), or "".
func (st *state) featureOfFile(key string) string {
	var ids []string
	for id := range st.data.Plans {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for _, f := range st.data.Plans[id].Impact.Files {
			if f.Repo+"/"+f.Path == key {
				return id
			}
		}
	}
	return ""
}

// hunkEnd is the last line of the new side of a hunk.
func hunkEnd(h views.Hunk) int {
	end := h.NewStart
	for _, d := range h.Lines {
		end = max(end, d.New)
	}
	return end
}

// fileCode draws the code of the left column: the changed blocks with the lines that did not
// change between them, or the whole file. hunkRow is the row where each block starts.
func (st *state) fileCode(f *views.File, cur int, full bool, iw int) (lines []line, hunkRow []int) {
	maxNo := len(f.Content)
	for _, h := range f.Hunks {
		maxNo = max(maxNo, hunkEnd(h), h.OldStart+len(h.Lines))
	}
	numW := len(fmt.Sprint(maxNo))
	// colors: the file tokenised once and each side of every hunk tokenised once, so multi-line
	// comments and strings keep their color; a line with no entry falls back to its own text.
	var content [][]seg
	if len(f.Content) > 0 {
		content = highlightLines(f.Language, f.Path, append([]string(nil), f.Content...))
	}
	hunkSegs := make([][]([]seg), len(f.Hunks))
	for hi, h := range f.Hunks {
		var newT, oldT []string
		var newI, oldI []int
		for li, d := range h.Lines {
			if d.Kind != views.DiffDel {
				newT, newI = append(newT, d.Text), append(newI, li)
			}
			if d.Kind != views.DiffAdd {
				oldT, oldI = append(oldT, d.Text), append(oldI, li)
			}
		}
		hunkSegs[hi] = make([][]seg, len(h.Lines))
		for k, s := range highlightLines(f.Language, f.Path, newT) {
			hunkSegs[hi][newI[k]] = s
		}
		for k, s := range highlightLines(f.Language, f.Path, oldT) {
			if d := h.Lines[oldI[k]]; d.Kind == views.DiffDel {
				hunkSegs[hi][oldI[k]] = s
			}
		}
	}
	row := func(mark string, no int, sign string, text string, bg tok, segs []seg) line {
		if segs == nil {
			segs = highlight(f.Language, f.Path, text)
		}
		n := ""
		if no > 0 {
			n = fmt.Sprint(no)
		}
		sc := tSb
		switch sign {
		case "+":
			sc = tDone
		case "-":
			sc = tFail
		}
		l := L(T(mark, tAc), " ", rcol(T(n, tSb), numW), " ", T(sign, sc), " ", line(segs))
		if bg != "" {
			l = bgize(fit(l, iw), bg)
		}
		return l
	}
	rowBg := func(kind string) (string, tok) {
		switch kind {
		case views.DiffAdd:
			return "+", tDoneBg
		case views.DiffDel:
			return "-", tFailBg
		}
		return " ", ""
	}

	if !full || len(f.Content) == 0 {
		for hi, h := range f.Hunks {
			if hi > 0 {
				if gap := h.NewStart - hunkEnd(f.Hunks[hi-1]) - 1; gap > 0 {
					lines = append(lines, L("    ", T(fmt.Sprintf("··· %s sin cambios ···", plural(gap, "línea", "líneas")), tSb)))
				}
			}
			hunkRow = append(hunkRow, len(lines))
			for li, d := range h.Lines {
				mark := " "
				if hi == cur && li == 0 {
					mark = "▸"
				}
				sign, bg := rowBg(d.Kind)
				no := d.New
				if d.Kind == views.DiffDel {
					no = d.Old
				}
				lines = append(lines, row(mark, no, sign, d.Text, bg, hunkSegs[hi][li]))
			}
		}
		return lines, hunkRow
	}

	// the whole file: the deleted lines go in front of the line that follows them
	dels := map[int][]views.DiffLine{}
	added := map[int]bool{}
	for _, h := range f.Hunks {
		var pending []views.DiffLine
		for _, d := range h.Lines {
			if d.Kind == views.DiffDel {
				pending = append(pending, d)
				continue
			}
			if len(pending) > 0 {
				dels[d.New] = append(dels[d.New], pending...)
				pending = nil
			}
			if d.Kind == views.DiffAdd {
				added[d.New] = true
			}
		}
	}
	starts := map[int]int{}
	for hi, h := range f.Hunks {
		starts[h.NewStart] = hi
	}
	hunkRow = make([]int, len(f.Hunks))
	for i, text := range f.Content {
		no := i + 1
		if hi, ok := starts[no]; ok {
			hunkRow[hi] = len(lines)
		}
		for _, d := range dels[no] {
			lines = append(lines, row(" ", d.Old, "-", d.Text, tFailBg, nil))
		}
		mark := " "
		if hi, ok := starts[no]; ok && hi == cur {
			mark = "▸"
		}
		if added[no] {
			lines = append(lines, row(mark, no, "+", text, tDoneBg, content[i]))
		} else {
			lines = append(lines, row(mark, no, " ", text, "", content[i]))
		}
	}
	return lines, hunkRow
}

// featureState says what a feature did with a file.
func featureState(now string, ft views.FileFeature) line {
	var l line
	if ft.Batch > 0 {
		l = L(T(fmt.Sprintf("  lote %d · ", ft.Batch), tSb))
	} else {
		l = L("  ")
	}
	switch ft.State {
	case "failed":
		return append(l, T("en rojo", tFail))
	case "running":
		return append(l, T("en curso", tWork))
	case "pending":
		return append(l, T("por empezar", tMu))
	}
	if a := ago(now, ft.ClosedAt); a != "" {
		return append(l, T("cerrada "+a, tMu))
	}
	return append(l, T("cerrada", tMu))
}

// fileSide is the right column: who imports the file, its tests, the memory anchored to it and
// the features that touched it.
func (st *state) fileSide(f *views.File, imp, w int) []line {
	iw := w - 6
	var g []line
	section := func(title string) {
		if len(g) > 0 {
			g = append(g, nil)
		}
		g = append(g, L(B(title, "")))
	}
	if len(f.ImportedBy) > 0 {
		section("Importado por")
		for i, x := range f.ImportedBy {
			name := T(path.Base(x.Path), "")
			mark := T(" ", "")
			if i == imp {
				name, mark.Mark = B(path.Base(x.Path), tAc), true
			}
			g = append(g, lr(L(mark, name), L(T(fmt.Sprintf(":%d", x.Line), tSb)), iw))
		}
	}
	if len(f.Tests) > 0 {
		section("Tests")
		for _, t := range f.Tests {
			g = append(g, L(st.testGlyph(t.Status), " ", path.Base(t.Path)))
		}
	}
	if len(f.Rules)+len(f.Learnings) > 0 {
		section("Memoria anclada")
		for _, m := range append(append([]views.MemRef(nil), f.Rules...), f.Learnings...) {
			g = append(g, L(B(m.ID, ""), T("  "+m.Text, tMu)))
		}
	}
	if len(f.Features) > 0 {
		section("Features")
		for _, ft := range f.Features {
			g = append(g, L(T(ft.ID, tMu), featureState(st.data.Cockpit.GeneratedAt, ft)))
		}
	}
	if len(g) == 0 {
		g = append(g, L(T("nada más apunta a este archivo", tMu)))
	}
	return block(g, w, tBg1, 3)
}

func (fileScreen) view(st *state, r route, w int) ([]line, int) {
	f := st.data.Files[r.key]
	if f == nil {
		return indent([]line{nil, L(T("No hay datos de "+r.key+" en el demo.", tMu))}, 2), 0
	}
	iw := w - 4 - 6
	full := st.fileFull(r)
	cur := min(max(st.cur[r.ckey()+"#n"], 0), max(len(f.Hunks)-1, 0))
	imp := min(max(st.cur[r.ckey()+"#i"], 0), max(len(f.ImportedBy)-1, 0))

	title := []line{
		lr(L(T("←", tMu), "   ", B(path.Base(f.Path), ""), "   ", T(f.Repo, tSb)), L(S(" "+sourceLabel(f.Source)+" ", "ch-mute")), iw),
		L("    ", T(strings.Join(nonEmpty(f.Path, f.EditedBy), " · "), tMu)),
	}
	out := []line{nil}
	out = append(out, indent(block(title, w-4, tBg1, 3), 2)...)
	out = append(out, nil)

	side := w >= 100
	sideW, codeW := w-4, w-4
	if side {
		sideW = 38
		codeW = w - 4 - 2 - sideW
	}
	ciw := codeW - 6
	label := f.DiffLabel
	if label == "" {
		label = "diff"
	}
	diffChip, fullChip := S(" "+label+" ", "ch-ac"), S(" archivo ", "ch-off")
	if full {
		diffChip, fullChip = S(" "+label+" ", "ch-off"), S(" archivo ", "ch-ac")
	}
	body, rows := st.fileCode(f, cur, full, ciw)
	head := []line{lr(L(diffChip, " ", fullChip), L(keycap("v"), T(" alterna ", tMu), keycap("o"), T(" editor", tMu)), ciw), nil}
	code := append(head, body...)
	if len(f.Notes) > 0 {
		code = append(code, nil)
		for _, n := range f.Notes {
			mark := T("· ", tSb)
			if n.Kind == "fail" {
				mark = T(st.ic.Failed+" ", tFail)
			}
			code = append(code, L(mark, T(n.Text, tMu)))
		}
	}
	focus := len(out)
	if cur < len(rows) {
		focus += len(head) + rows[cur]
	}
	sideLines := st.fileSide(f, imp, sideW)
	if side {
		out = append(out, indent(hcat([][]line{block(code, codeW, tBg1, 3), sideLines}, []int{codeW, sideW}, 2), 2)...)
	} else {
		out = append(out, indent(block(code, codeW, tBg1, 3), 2)...)
		out = append(out, nil)
		out = append(out, indent(sideLines, 2)...)
	}
	return out, focus
}

func (fileScreen) key(st *state, r route, k string) intent {
	f := st.data.Files[r.key]
	if f == nil {
		return intent{}
	}
	cur := min(max(st.cur[r.ckey()+"#n"], 0), max(len(f.Hunks)-1, 0))
	imp := min(max(st.cur[r.ckey()+"#i"], 0), max(len(f.ImportedBy)-1, 0))
	switch k {
	case "n":
		st.cur[r.ckey()+"#n"] = min(cur+1, max(len(f.Hunks)-1, 0))
	case "N":
		st.cur[r.ckey()+"#n"] = max(cur-1, 0)
	case "v":
		st.cur[r.ckey()+"#v"] = 1 - st.cur[r.ckey()+"#v"]
	case "up", "k":
		st.cur[r.ckey()+"#i"] = max(imp-1, 0)
	case "down", "j":
		st.cur[r.ckey()+"#i"] = min(imp+1, max(len(f.ImportedBy)-1, 0))
	case "o":
		line := 1
		if cur < len(f.Hunks) {
			line = f.Hunks[cur].NewStart
		}
		return intent{toast: fmt.Sprintf("correría: $EDITOR +%d %s", line, f.Path)}
	case "enter":
		if len(f.ImportedBy) == 0 {
			return intent{}
		}
		x := f.ImportedBy[imp]
		key := f.Repo + "/" + x.Path
		if _, ok := st.data.Files[key]; !ok {
			return intent{toast: "El demo no trae el código de " + path.Base(x.Path) + "."}
		}
		return intent{push: &route{screen: "archivo", key: key}}
	}
	return intent{}
}
