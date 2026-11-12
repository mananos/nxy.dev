package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/mananos/nxy.dev/internal/format"
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
	Text string
	Fg   tok
	Bg   tok
	Bold bool
	Mark bool // in plain the first cell is drawn as › (the selected row)
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
		pad := spaces(padX, bg)
		pad.Mark = bg == tSel && i == 0 && padX > 0
		out[i] = bgize(L(pad, fit(l, w-2*padX), spaces(padX, bg)), bg)
	}
	return out
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

// chip is a label in a state chip of fixed width: " Te necesita ".
func chip(label string, state tok, w int) line {
	text := " " + label
	if n := ansi.StringWidth(text); n > w-1 {
		text = ansi.Truncate(text, w-1, "")
	}
	pad := w - ansi.StringWidth(text)
	return L(S(text+strings.Repeat(" ", max(pad, 0)), "ch-"+string(state)))
}

// keycap is a key drawn as a small button.
func keycap(k string) seg { return S(" "+k+" ", "ch-mute") }

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
			p = append(p, S(" "+name+" ", "ch-ac"))
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
		for ; j < len(l) && l[j].Fg == s.Fg && l[j].Bg == s.Bg && l[j].Bold == s.Bold; j++ {
			text += l[j].Text
		}
		i = j
		if text != "" {
			b.WriteString(th.style(s.Fg, s.Bg, s.Bold).Render(text))
		}
	}
	return b.String()
}
