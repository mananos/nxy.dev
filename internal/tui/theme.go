package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/mananos/nxy.dev/v2/internal/views"
)

// tok is the name of a color of the theme (the tokens of the .term of the mockup).
type tok string

// Tokens of the palette. An empty Fg is "fg" and an empty Bg is "bg".
const (
	tBg   tok = "bg"
	tFg   tok = "fg"
	tMu   tok = "mu"
	tSb   tok = "sb"
	tAc   tok = "ac"
	tBg1  tok = "bg1"
	tBg2  tok = "bg2"
	tSel  tok = "sel"
	tWork tok = "work"
	tVer  tok = "ver"
	tNeed tok = "need"
	tDone tok = "done"
	tFail tok = "fail"
	tD2   tok = "d2"

	tCap    tok = "cap"    // keycap of the footer
	tBandMu tok = "bandmu" // muted text on the yellow band
	tTape   tok = "tape"   // the ╱ fill of the band

	tDoneBg tok = "donebg" // tint of an added line
	tFailBg tok = "failbg" // tint of the red block of a failed batch and of a deleted line
)

var darkPalette = map[tok]string{
	"bg": "#070707", "fg": "#DEDBD2", "mu": "#8F8B80", "sb": "#5B5850", "ac": "#FDBF01",
	"bg1": "#0E0E0D", "bg2": "#191916", "sel": "#FDBF01",
	"work": "#6ea8fe", "ver": "#b69cff", "need": "#FDBF01", "done": "#7fd19a", "fail": "#FF8A80", "d2": "#B8B4A8",
	"needbg": "#2A2200", "failbg": "#3A1414", "workbg": "#13203A", "verbg": "#231A3A", "donebg": "#14301C", "acbg": "#2A2200",
	"cap": "#2A2822", "bandmu": "#5C4800", "tape": "#C79600",
}

// selInk is the dark-on-yellow tone of each token when it is drawn on the chosen row (tSel).
var selInk = map[tok]string{
	"fg": "#070707", "ac": "#070707", "need": "#070707", "d2": "#070707",
	"mu": "#4A3B00", "sb": "#6B5600", "fail": "#8A1C12", "done": "#0F5A2A", "work": "#0B3D91", "ver": "#4B2A9A",
}

type styleKey struct {
	fg, bg tok
	bold   bool
	under  bool
}

// theme paints segments. With plain=true nothing is painted, so the output has no ESC.
type theme struct {
	plain bool
	pal   map[tok]string
	cache map[styleKey]lipgloss.Style
}

func newTheme(plain bool) *theme {
	return &theme{plain: plain, pal: darkPalette, cache: map[styleKey]lipgloss.Style{}}
}

func (t *theme) color(k tok) color.Color {
	hex, ok := t.pal[k]
	if !ok {
		hex = t.pal["fg"]
	}
	return lipgloss.Color(hex)
}

// bgColor is the color of the terminal background (nil in plain).
func (t *theme) bgColor() color.Color {
	if t.plain {
		return nil
	}
	return t.color(tBg)
}

// style is the lipgloss style of a segment: both colors are always explicit, so a text color
// never lets the terminal background show through a block.
func (t *theme) style(fg, bg tok, bold, under bool) lipgloss.Style {
	if fg == "" {
		fg = tFg
	}
	if bg == "" {
		bg = tBg
	}
	k := styleKey{fg, bg, bold, under}
	if s, ok := t.cache[k]; ok {
		return s
	}
	fgc := t.color(fg)
	if bg == tSel {
		ink, ok := selInk[fg]
		if !ok {
			ink = "#070707"
		}
		fgc = lipgloss.Color(ink)
	}
	s := lipgloss.NewStyle().Foreground(fgc).Background(t.color(bg)).Bold(bold).Underline(under)
	t.cache[k] = s
	return s
}

// styleOf resolves a named style: ch-<state> (solid chip), tn-<state> (tonal), ch-mute, ch-off
// and tabon.
func styleOf(name string) (fg, bg tok, bold bool) {
	switch {
	case name == "tabon":
		return "", tBg, true
	case name == "ch-mute":
		return tMu, tBg2, false
	case name == "ch-off":
		return tSb, tBg1, false
	case strings.HasPrefix(name, "ch-"):
		return tBg, tok(name[3:]), true
	case strings.HasPrefix(name, "tn-"):
		k := tok(name[3:])
		return k, k + "bg", true
	}
	return "", "", false
}

// statusLabel is the Spanish label of a state key (§ 14.16, mockup).
func statusLabel(s views.Status) string {
	switch s {
	case views.StatusWorking:
		return "Trabajando"
	case views.StatusVerifying:
		return "Verificando"
	case views.StatusNeedsYou:
		return "Te necesita"
	case views.StatusDone:
		return "Listo"
	}
	return string(s)
}

// statusTok is the color token of a state.
func statusTok(s views.Status) tok {
	switch s {
	case views.StatusWorking:
		return tWork
	case views.StatusVerifying:
		return tVer
	case views.StatusNeedsYou:
		return tNeed
	case views.StatusDone:
		return tDone
	}
	return tSb
}
