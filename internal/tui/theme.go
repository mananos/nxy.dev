package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/mananos/nxy.dev/internal/views"
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

	tDoneBg tok = "donebg" // tint of an added line
	tFailBg tok = "failbg" // tint of the red block of a failed batch and of a deleted line
)

var darkPalette = map[tok]string{
	"bg": "#101318", "fg": "#d6dae1", "mu": "#8b94a3", "sb": "#4b5463", "ac": "#5fd4c4",
	"bg1": "#171b22", "bg2": "#1f242d", "sel": "#222a37",
	"work": "#6ea8fe", "ver": "#b69cff", "need": "#f2b65a", "done": "#7fd19a", "fail": "#f07c7c", "d2": "#aab2bf",
	"needbg": "#2b2416", "failbg": "#2a1719", "workbg": "#16202f", "verbg": "#211b30", "donebg": "#15241b", "acbg": "#12262a",
}

var lightPalette = map[tok]string{
	"bg": "#fbfbf9", "fg": "#1d2430", "mu": "#5f6878", "sb": "#aab1bd", "ac": "#0c7d70",
	"bg1": "#f0f1ed", "bg2": "#e6e8e2", "sel": "#e2e8f1",
	"work": "#2f6bd8", "ver": "#7a52d6", "need": "#a86500", "done": "#2b8a4b", "fail": "#c93c3c", "d2": "#434b58",
	"needbg": "#fbf0dc", "failbg": "#fbe7e6", "workbg": "#e5edfb", "verbg": "#eee8fb", "donebg": "#e3f2e7", "acbg": "#dcf0ec",
}

type styleKey struct {
	fg, bg tok
	bold   bool
}

// theme paints segments. With plain=true nothing is painted, so the output has no ESC.
type theme struct {
	plain bool
	pal   map[tok]string
	cache map[styleKey]lipgloss.Style
}

func newTheme(dark, plain bool) *theme {
	t := &theme{plain: plain, pal: lightPalette, cache: map[styleKey]lipgloss.Style{}}
	if dark {
		t.pal = darkPalette
	}
	return t
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
func (t *theme) style(fg, bg tok, bold bool) lipgloss.Style {
	if fg == "" {
		fg = tFg
	}
	if bg == "" {
		bg = tBg
	}
	k := styleKey{fg, bg, bold}
	if s, ok := t.cache[k]; ok {
		return s
	}
	s := lipgloss.NewStyle().Foreground(t.color(fg)).Background(t.color(bg)).Bold(bold)
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
