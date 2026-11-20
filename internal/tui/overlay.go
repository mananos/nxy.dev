package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// overlay draws the lines of top over base, with the corner of top at column x and row y.
// Both are already painted (ANSI strings); the cells of base around top keep their colors.
func overlay(base, top []string, x, y int) []string {
	out := append([]string(nil), base...)
	for i, t := range top {
		r := y + i
		if r < 0 || r >= len(out) {
			continue
		}
		bw := ansi.StringWidth(out[r])
		tw := ansi.StringWidth(t)
		left := ansi.Truncate(out[r], x, "")
		if lwd := ansi.StringWidth(left); lwd < x {
			left += strings.Repeat(" ", x-lwd)
		}
		right := ""
		if bw > x+tw {
			right = ansi.Cut(out[r], x+tw, bw)
		}
		reset := ""
		if strings.Contains(out[r], "\x1b") || strings.Contains(t, "\x1b") {
			reset = "\x1b[0m"
		}
		out[r] = left + reset + t + reset + right
	}
	return out
}

// dim is the base of a window: the same text without colors, in the faint color.
func dim(base []string, th *theme) []string {
	out := make([]string, len(base))
	for i, l := range base {
		plain := ansi.Strip(l)
		if th.plain || plain == "" {
			out[i] = plain
			continue
		}
		out[i] = th.style(tSb, tBg, false, false).Render(plain)
	}
	return out
}

// centerX is the column where a window of width tw starts to be centered in w.
func centerX(w, tw int) int { return max((w-tw)/2, 0) }
