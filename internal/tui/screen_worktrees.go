package tui

import (
	"fmt"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/views"
)

func init() { register(worktreesScreen{}) }

type worktreesScreen struct{}

func (worktreesScreen) id() string                 { return "worktrees" }
func (worktreesScreen) section() string            { return "inicio" }
func (worktreesScreen) crumb(*state, route) string { return "Worktrees" }
func (worktreesScreen) keys(*state, route) []hint {
	return []hint{{"esc", "volver"}, {"tab", "sección"}}
}
func (worktreesScreen) key(*state, route, string) intent {
	return intent{}
}

func worktreeState(s string) string {
	switch s {
	case "orphan":
		return "huérfano"
	case "paused":
		return "pausado"
	}
	return "activo"
}

// worktreeDelta is "↑3 · sin commitear": what the worktree has that the base does not, or the reverse.
func worktreeDelta(x views.Worktree, ic icons) string {
	var p []string
	if x.Ahead > 0 {
		p = append(p, fmt.Sprintf("%s%d", ic.Up, x.Ahead))
	}
	if x.Behind > 0 {
		p = append(p, fmt.Sprintf("%s%d", ic.Down, x.Behind))
	}
	if x.Dirty {
		p = append(p, "sin commitear")
	}
	return strings.Join(p, " · ")
}

func (worktreesScreen) view(st *state, _ route, w int) ([]line, int) {
	c := st.data.Cockpit
	iw := w - 4 - 6
	widths := [4]int{}
	rows := make([][5]string, len(c.Worktrees))
	for i, x := range c.Worktrees {
		fid := x.FeatureID
		if fid == "" {
			fid = "—"
		}
		rows[i] = [5]string{x.Repo, x.Branch, fid, worktreeState(x.State), worktreeDelta(x, st.ic)}
		for j := 0; j < 4; j++ {
			widths[j] = max(widths[j], len([]rune(rows[i][j])))
		}
	}
	gap := 2
	if widths[0]+widths[1]+widths[2]+widths[3]+4*gap+18 > iw {
		gap = 1
	}
	inner := []line{L(B("Worktrees", ""), T("  "+plural(len(rows), "carpeta", "carpetas"), tSb)), nil}
	for _, x := range rows {
		fg := tok("")
		if x[3] == "huérfano" {
			fg = tNeed
		}
		inner = append(inner, L(
			col(x[0], widths[0]+gap), col(T(x[1], tMu), widths[1]+gap), col(T(x[2], tMu), widths[2]+gap),
			col(T(x[3], fg), widths[3]+gap), T(x[4], tSb)))
	}
	if len(rows) == 0 {
		inner = append(inner, L(T("No hay worktrees.", tMu)))
	}
	inner = append([]line{nil}, inner...)
	inner = append(inner, nil)
	return append([]line{nil}, indent(block(inner, w-4, tBg1, 3), 2)...), 0
}
