package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/mananos/nxy.dev/v2/internal/format"
)

// The cell model of the mockup: a line is a list of segments, each with its own colors. A
// segment without Bg takes the Bg of the block it ends up in (bgize); when painted every
// segment carries both colors, so a text color never cuts the background of a block.

// durText is a duration in ms for the screens; a negative one (views.UnknownMs: a step with no
// start) prints nothing instead of the "?" of FmtDuration.
func durText(ms int64) string {
	if ms < 0 {
		return ""
	}
	return format.FmtDuration(float64(ms))
}

// chipW is the width of the state chips.
const chipW = 13

type seg struct {
	Text  string
	Fg    tok
	Bg    tok
	Bold  bool
	Mark  bool // in plain the first cell is drawn as › (the selected row)
	Under bool // underlined
	Head  bool // a section title: block draws ▌ in the cell before it
}

type line []seg

// T is a segment of text in a color.
func T(text string, fg tok) seg { return seg{Text: text, Fg: fg} }

// B is bold text.
func B(text string, fg tok) seg { return seg{Text: text, Fg: fg, Bold: true} }

// S is text in a named style (ch-need, tn-fail, ch-mute, ch-off, tabon).
func S(text, style string) seg {
	fg, bg, bold := styleOf(style)
	return seg{Text: text, Fg: fg, Bg: bg, Bold: bold}
}

// L joins strings, segments and lines into one line.
func L(parts ...any) line {
	var out line
	for _, p := range parts {
		switch x := p.(type) {
		case nil:
		case string:
			out = append(out, seg{Text: x})
		case seg:
			out = append(out, x)
		case line:
			out = append(out, x...)
		}
	}
	return out
}

func spaces(n int, bg tok) seg { return seg{Text: strings.Repeat(" ", max(n, 0)), Bg: bg} }

// lw is the width of a line in cells.
func lw(l line) int {
	n := 0
	for _, s := range l {
		n += ansi.StringWidth(s.Text)
	}
	return n
}

// fit cuts or pads a line to exactly w cells; a cut ends with …
func fit(l line, w int) line {
	if w <= 0 {
		return nil
	}
	if lw(l) <= w {
		return append(append(line{}, l...), spaces(w-lw(l), ""))
	}
	var out line
	room := w - 1
	var last seg
	for _, s := range l {
		sw := ansi.StringWidth(s.Text)
		if sw <= room {
			out = append(out, s)
			room -= sw
			last = s
			continue
		}
		if room > 0 {
			cut := s
			cut.Text = ansi.Truncate(s.Text, room, "")
			room -= ansi.StringWidth(cut.Text)
			out = append(out, cut)
		}
		last = s
		break
	}
	out = append(out, spaces(room, last.Bg))
	ell := last
	ell.Text, ell.Mark = "…", false
	out = append(out, ell)
	return out
}

// lr puts left and right at the two ends of w cells. The result is exactly w wide; when it does
// not fit the left side is cut and the right side stays.
func lr(left, right line, w int) line {
	if w <= 0 {
		return nil
	}
	rw := lw(right)
	if rw > w {
		right, rw = fit(right, w), w
	}
	minGap := 0
	if rw > 0 {
		minGap = 1
	}
	if lw(left)+rw+minGap > w {
		left = fit(left, w-rw-minGap)
	}
	return L(left, spaces(w-lw(left)-rw, ""), right)
}

// bgize gives a background to the segments that do not have one.
func bgize(l line, bg tok) line {
	out := make(line, len(l))
	for i, s := range l {
		if s.Bg == "" {
			s.Bg = bg
		}
		out[i] = s
	}
	return out
}

// indent puts n cells on the left of every line.
func indent(lines []line, n int) []line {
	out := make([]line, len(lines))
	for i, l := range lines {
		out[i] = L(spaces(n, ""), l)
	}
	return out
}

// col is a line cut or padded to w.
func col(l any, w int) line { return fit(L(l), w) }

// rcol is a line aligned to the right in w cells.
func rcol(l any, w int) line {
	x := L(l)
	if lw(x) > w {
		return fit(x, w)
	}
	return L(spaces(w-lw(x), ""), x)
}

// block paints lines of exactly w cells over a background with padX cells on each side. The
// first line of a selected block (bg sel) carries the plain marker.
func block(lines []line, w int, bg tok, padX int) []line {
	out := make([]line, len(lines))
	for i, l := range lines {
		pad := L(spaces(padX, bg))
		if len(l) > 0 && l[0].Head && padX > 0 {
			pad = L(spaces(padX-1, bg), headBar())
		}
		if bg == tSel && i == 0 && padX > 0 {
			pad[0].Mark = true
		}
		body := L(pad, fit(l, w-2*padX), spaces(padX, bg))
		if bg == tSel {
			out[i] = selLine(body)
		} else {
			out[i] = bgize(body, bg)
		}
	}
	return out
}

// errBlock is the dim red block of a view that failed to load: the first line of the error.
func errBlock(msg string, w int) []line {
	first := strings.TrimSpace(msg)
	if i := strings.IndexByte(first, '\n'); i >= 0 {
		first = strings.TrimSpace(first[:i])
	}
	return block([]line{L(T("No se pudo cargar", tFail)), L(T(first, tMu))}, w, tFailBg, 2)
}

func loadingLine() line { return L(T("cargando…", tMu)) }

// missing is what a screen shows while its view is not there: the error, the loading line or the
// plain statement that there is nothing (empty).
func (st *state) missing(key, empty string, w int) []line {
	if err := st.data.Errs[key]; err != nil {
		return indent(append([]line{nil}, errBlock(err.Error(), w-4)...), 2)
	}
	// the cheap views load together: one flag
	cheap := key == "stats" || key == "trend" || key == "config" || strings.HasPrefix(key, "memory/")
	if st.data.Loading[key] || (cheap && st.data.Loading["cockpit"]) {
		return indent([]line{nil, loadingLine()}, 2)
	}
	return indent([]line{nil, L(T(empty, tMu))}, 2)
}

// hcat puts columns side by side. A column shorter than the others is filled with the
// background of its last line.
func hcat(cols [][]line, ws []int, gap int) []line {
	h := 0
	for _, c := range cols {
		h = max(h, len(c))
	}
	out := make([]line, h)
	for i := 0; i < h; i++ {
		var row line
		for c, lines := range cols {
			if c > 0 {
				row = append(row, spaces(gap, ""))
			}
			if i < len(lines) {
				row = append(row, fit(lines[i], ws[c])...)
				continue
			}
			var bg tok
			if n := len(lines); n > 0 && len(lines[n-1]) > 0 {
				bg = lines[n-1][0].Bg
			}
			row = append(row, spaces(ws[c], bg))
		}
		out[i] = row
	}
	return out
}

// tonal is a label in a tonal state block of fixed width w: " borrar    ".
func tonal(label string, state tok, w int) seg {
	pad := w - 1 - len([]rune(label))
	return S(" "+label+strings.Repeat(" ", max(pad, 0)), "tn-"+string(state))
}

// chip is a label in a state chip of fixed width: " Te necesita ".
func chip(label string, state tok, w int) line {
	text := " " + strings.ToUpper(label)
	if n := ansi.StringWidth(text); n > w-1 {
		text = ansi.Truncate(text, w-1, "")
	}
	pad := w - ansi.StringWidth(text)
	text += strings.Repeat(" ", max(pad, 0))
	if state == tNeed {
		return L(S(text, "ch-need"))
	}
	return L(B(text, state))
}

// keycap is a key drawn as a small button.
func keycap(k string) seg { return S(" "+k+" ", "ch-mute") }

// footCap is a key of the footer: yellow letter on a gray cap.
func footCap(k string) seg { return seg{Text: " " + k + " ", Fg: tAc, Bg: tCap, Bold: true} }

// heading is a section title: uppercase yellow bold; block draws the ▌ before it.
func heading(text string) seg {
	return seg{Text: strings.ToUpper(text), Fg: tAc, Bold: true, Head: true}
}

// headBar is the ▌ that opens a section title.
func headBar() seg { return T("▌", tAc) }

// accent is a label in yellow bold underlined, with a space on each side (same width as the old
// ch-ac chip).
func accent(text string) line {
	return L(" ", seg{Text: text, Fg: tAc, Bold: true, Under: true}, " ")
}

// selLine paints a line on the chosen row: a chip drawn on need/ac is inverted (yellow on dark).
func selLine(l line) line {
	out := make(line, len(l))
	for i, s := range l {
		if s.Bg == tNeed || s.Bg == tAc {
			s.Fg, s.Bg = tAc, tBg
		}
		out[i] = s
	}
	return bgize(out, tSel)
}

// pick draws a card: when selected only its first line is on the chosen row, the rest keep the
// normal background.
func pick(lines []line, w int, normal tok, selected bool, padX int) []line {
	if !selected || len(lines) == 0 {
		return block(lines, w, normal, padX)
	}
	out := block(lines[:1], w, tSel, padX)
	return append(out, block(lines[1:], w, normal, padX)...)
}

// fillRun replaces the interior of the longest run of plain spaces (at least minRun) with ch in
// fg, leaving keep spaces on each side. The width does not change.
func fillRun(l line, ch string, fg tok, minRun, keep int) line {
	var cells line
	for _, s := range l {
		if strings.Trim(s.Text, " ") != "" {
			cells = append(cells, s)
			continue
		}
		for range s.Text {
			c := s
			c.Text = " "
			cells = append(cells, c)
		}
	}
	bestStart, bestLen := 0, 0
	for i := 0; i < len(cells); {
		if cells[i].Text != " " {
			i++
			continue
		}
		j := i
		for j < len(cells) && cells[j].Text == " " {
			j++
		}
		if j-i > bestLen {
			bestStart, bestLen = i, j-i
		}
		i = j
	}
	if bestLen < minRun || bestLen-2*keep <= 0 {
		return l
	}
	for i := bestStart + keep; i < bestStart+bestLen-keep; i++ {
		cells[i].Text, cells[i].Fg = ch, fg
	}
	var out line
	for _, c := range cells {
		if n := len(out); n > 0 {
			p := &out[n-1]
			if p.Fg == c.Fg && p.Bg == c.Bg && p.Bold == c.Bold && p.Under == c.Under && !p.Mark && !c.Mark && !p.Head && !c.Head {
				p.Text += c.Text
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

var stageNames = []struct{ key, name string }{
	{"design", "Diseño"}, {"plan", "Plan"}, {"batches", "Lotes"}, {"review", "Review"}, {"close", "Cierre"},
}

// stepper draws the stages of a feature: the passed ones with ✓, the current one filled (with
// its detail, "2/4") and the rest dimmed.
func stepper(stage, detail string) line {
	cur := -1
	for i, s := range stageNames {
		if s.key == stage {
			cur = i
		}
	}
	var p line
	for i, s := range stageNames {
		if i > 0 {
			if i <= cur {
				p = append(p, T(" ━━ ", tAc))
			} else {
				p = append(p, T(" ── ", tSb))
			}
		}
		switch {
		case i < cur:
			p = append(p, S(" ✓ "+s.name+" ", "ch-mute"))
		case i == cur:
			name := s.name
			if detail != "" {
				name += " " + detail
			}
			p = append(p, accent(name)...)
		default:
			p = append(p, S(" "+s.name+" ", "ch-off"))
		}
	}
	return p
}

// progress is a bar of w cells: n of m painted.
func progress(n, m, w int, fg tok) line {
	if m <= 0 {
		n, m = 0, 1
	}
	n = min(max(n, 0), m)
	k := (2*w*n + m) / (2 * m) // round(w*n/m)
	return L(T(strings.Repeat("━", k), fg), T(strings.Repeat("─", w-k), tSb))
}

// crumb is the path of the screen: Inicio › F7K2 › Plan.
func crumb(parts []string) line {
	var out line
	for i, p := range parts {
		if i > 0 {
			out = append(out, T(" › ", tSb))
		}
		if i == len(parts)-1 {
			out = append(out, B(p, ""))
		} else {
			out = append(out, T(p, tMu))
		}
	}
	return out
}

// render paints a line. In plain only the text comes out (same width) and the marked cell is ›.
func render(l line, th *theme) string {
	var b strings.Builder
	if th.plain {
		for _, s := range l {
			text := s.Text
			if s.Mark && text != "" {
				_, n := utf8.DecodeRuneInString(text)
				text = "›" + text[n:]
			}
			b.WriteString(text)
		}
		return b.String()
	}
	for i := 0; i < len(l); {
		s := l[i]
		text := s.Text
		j := i + 1
		for ; j < len(l) && l[j].Fg == s.Fg && l[j].Bg == s.Bg && l[j].Bold == s.Bold && l[j].Under == s.Under; j++ {
			text += l[j].Text
		}
		i = j
		if text != "" {
			b.WriteString(th.style(s.Fg, s.Bg, s.Bold, s.Under).Render(text))
		}
	}
	return b.String()
}
