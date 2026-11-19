package tui

import (
	"fmt"
	"path"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mananos/nxy.dev/v2/internal/views"
)

func init() { register(configScreen{}) }

type configScreen struct{}

func (configScreen) id() string                 { return "config" }
func (configScreen) section() string            { return "config" }
func (configScreen) crumb(*state, route) string { return "Config" }

func (configScreen) keys(st *state, _ route) []hint {
	h := []hint{{"↑↓", "fila"}, {"e", "modelo"}, {"f", "effort"}}
	if c := st.data.Config; c != nil && hasRepoActions(c) {
		h = append(h, hint{"r", "guardar en el repo"})
	}
	return append(h, hint{"i", "instalar"}, hint{"c", "copiar"})
}

// hasRepoActions tells whether the view brings the actions that save in the repo (it does not
// outside a repo).
func hasRepoActions(c *views.Config) bool {
	for _, ro := range c.Roles {
		if _, ok := roleAction(ro, "model_repo"); ok {
			return true
		}
	}
	return false
}

// cfgRow is one row the cursor can stand on.
type cfgRow struct {
	kind string // role|setting|tool|statusline|binary
	idx  int
}

func cfgRows(c *views.Config) []cfgRow {
	var out []cfgRow
	for i := range c.Roles {
		out = append(out, cfgRow{"role", i})
	}
	for i := range c.Settings {
		out = append(out, cfgRow{"setting", i})
	}
	for i := range c.Tools {
		out = append(out, cfgRow{"tool", i})
	}
	return append(out, cfgRow{"statusline", 0}, cfgRow{"binary", 0})
}

func (st *state) cfgCursor(r route, n int) int {
	return min(max(st.cur[r.ckey()], 0), max(n-1, 0))
}

// cfgValue is what a role has now, from the view. kind is model or effort. The second value is
// where it comes from.
func (st *state) cfgValue(ro views.RoleRow, kind string) (string, string) {
	if kind == "model" {
		return ro.Model, ro.Source
	}
	return ro.Effort, ro.Source
}

func sourceText(s string) string {
	if s == "user" {
		return "usuario"
	}
	return s
}

func roleAction(ro views.RoleRow, id string) (views.Action, bool) {
	for _, a := range ro.Actions {
		if a.ID == id {
			return a, true
		}
	}
	return views.Action{}, false
}

// withValue is the action with {value} replaced in its command.
func withValue(a views.Action, value string) views.Action {
	out := make([]string, len(a.Argv))
	for i, x := range a.Argv {
		out[i] = strings.ReplaceAll(x, "{value}", value)
	}
	a.Argv = out
	return a
}

func cycle(options []string, cur string) string {
	if len(options) == 0 {
		return cur
	}
	for i, o := range options {
		if o == cur {
			return options[(i+1)%len(options)]
		}
	}
	return options[0]
}

func valueText(v any) string {
	switch x := v.(type) {
	case bool:
		if x {
			return "sí"
		}
		return "no"
	case string:
		switch x {
		case "on":
			return "sí"
		case "off":
			return "no"
		}
		return x
	}
	return ""
}

func (configScreen) key(st *state, r route, k string) intent {
	c := st.data.Config
	if c == nil {
		return intent{}
	}
	rows := cfgRows(c)
	sel := st.cfgCursor(r, len(rows))
	cur := rows[sel]
	switch k {
	case "up", "k":
		st.setCursor(r, max(sel-1, 0))
	case "down", "j":
		st.setCursor(r, min(sel+1, len(rows)-1))
	case "e", "f", "r":
		switch cur.kind {
		case "role":
			return st.cfgRole(c.Roles[cur.idx], k)
		case "setting":
			if k == "f" {
				return intent{toast: "El effort es de los roles; un módulo se cambia con e."}
			}
			return st.cfgSetting(c.Settings[cur.idx], k == "r")
		}
		return intent{toast: "Elegí un rol o un módulo para cambiarlo."}
	case "i":
		switch cur.kind {
		case "tool":
			t := c.Tools[cur.idx]
			if t.Found || t.Install == nil {
				return intent{toast: t.Name + " ya está instalada."}
			}
			return intent{toast: st.runAction(actionReq{key: "tool/" + t.Name, label: "instalar " + t.Name, busy: "instalando " + t.Name + "…", a: *t.Install})}
		case "statusline":
			if c.Statusline.Installed {
				return intent{toast: st.runAction(actionReq{key: "statusline", label: "quitar la statusline", busy: "quitando…", a: c.Statusline.Remove})}
			}
			return intent{toast: st.runAction(actionReq{key: "statusline", label: "instalar la statusline", busy: "instalando…", a: c.Statusline.Install})}
		}
		return intent{toast: "Elegí una herramienta o la statusline para instalar."}
	case "c":
		if cur.kind != "binary" {
			return intent{toast: "Elegí el binario para copiar su comando."}
		}
		if c.Binary.ManualCommand == "" {
			return intent{toast: "No hace falta descargar nada."}
		}
		cp, toast := st.copyText(c.Binary.ManualCommand)
		st.cmd = tea.Batch(st.cmd, cp)
		return intent{toast: toast}
	}
	return intent{}
}

// cfgRole handles e, f and r on a role. e and f move to the next value and ask y/n with it; n
// leaves it pending for the next press; r saves in the repo the pending value (or the current).
func (st *state) cfgRole(ro views.RoleRow, k string) intent {
	kind, repo := "model", false
	switch k {
	case "f":
		kind = "effort"
	case "r":
		kind, repo = st.cfgKind, true
		if kind == "" {
			kind = "model"
		}
	}
	pk := ro.Role + "/" + kind
	now, _ := st.cfgValue(ro, kind)
	if v, ok := st.cfgPend[pk]; ok {
		now = v
	}
	opts := ro.Models
	if kind == "effort" {
		opts = ro.Efforts
	}
	next, id := now, kind
	if repo {
		id = kind + "_repo"
	} else {
		next = cycle(opts, now)
		st.cfgKind = kind
		st.cfgPend[pk] = next
	}
	a, ok := roleAction(ro, id)
	if !ok {
		return intent{toast: "Esta carpeta no es un repo: no hay dónde guardarlo."}
	}
	return intent{toast: st.runAction(actionReq{label: next + " para " + ro.Role, a: withValue(a, next),
		after: func() { delete(st.cfgPend, pk) }})}
}

// cfgSetting is the same for a module: e cycles its options, r saves the value in the repo.
func (st *state) cfgSetting(s views.Setting, repo bool) intent {
	pk := "setting/" + s.ID
	now := fmt.Sprint(s.Value)
	if v, ok := st.cfgPend[pk]; ok {
		now = v
	}
	var opts []string
	for _, o := range s.Options {
		opts = append(opts, fmt.Sprint(o))
	}
	next, id := now, "set"
	if repo {
		id = "set_repo"
	} else {
		next = cycle(opts, now)
		st.cfgPend[pk] = next
	}
	var a views.Action
	found := false
	for _, x := range s.Actions {
		if x.ID == id {
			a, found = x, true
		}
	}
	if !found {
		return intent{toast: "Esta carpeta no es un repo: no hay dónde guardarlo."}
	}
	return intent{toast: st.runAction(actionReq{label: valueText(next) + " para " + s.Label, a: withValue(a, next),
		after: func() { delete(st.cfgPend, pk) }})}
}

// lead is the two cells before a selectable row; in plain a selected one shows ›.
func lead(selected bool) seg {
	if selected {
		return seg{Text: "  ", Bg: tSel, Mark: true}
	}
	return seg{Text: "  "}
}

func (configScreen) view(st *state, r route, w int) ([]line, int) {
	c := st.data.Config
	if c == nil {
		return st.missing("config", "No hay config para mostrar.", w), 0
	}
	iw := w - 4 - 6
	rows := cfgRows(c)
	sel := st.cfgCursor(r, len(rows))
	focus := 0

	// layers
	var user, repo views.Layer
	for _, l := range c.Layers {
		if l.Name == "user" {
			user = l
		} else if l.Name == "repo" {
			repo = l
		}
	}
	repoName := path.Base(path.Dir(path.Dir(strings.ReplaceAll(repo.Path, "\\", "/"))))
	repoText := repo.Path
	if !repo.Exists {
		repoText = repoName + " · sin archivo"
	}
	layers := L("  ", accent("usuario"), "  ", T(user.Path, tMu), "      ", S(" repo ", "ch-off"), "  ", T(repoText, tSb))
	out := []line{nil, lr(layers, L(T("gana el repo", tSb), "  "), w), nil}

	// roles
	narrow := iw < 90
	doesW := iw - (13 + 16 + 8 + 8 + 8)
	if doesW < 20 {
		narrow = true
	}
	if narrow {
		doesW = iw - (16 + 8 + 8 + 8)
	}
	head := L()
	if !narrow {
		head = L(col(T("Etapa", tSb), 13))
	}
	head = L(head, col(T("Rol", tSb), 16), col(T("Qué hace", tSb), doesW-1), spaces(1, ""), col(T("Modelo", tSb), 8), col(T("Effort", tSb), 8), col(T("Origen", tSb), 8))
	title := lr(L(heading("Roles")), L(T("se aplica al próximo agente que se lance", tSb)), iw)
	out = append(out, indent(block([]line{title, head}, w-4, tBg1, 3), 2)...)
	prevStage := ""
	for i, ro := range c.Roles {
		isSel := sel == i
		model, _ := st.cfgValue(ro, "model")
		effort, _ := st.cfgValue(ro, "effort")
		src := ro.Source
		stage := ""
		if ro.Stage != prevStage {
			stage = ro.Stage
		}
		prevStage = ro.Stage
		hot := func(s string) seg {
			if isSel {
				return B(s, tAc)
			}
			return T(s, "")
		}
		name := T(ro.Role, "")
		if isSel {
			name.Bold = true
		}
		l := L()
		if !narrow {
			l = L(col(T(stage, tSb), 13))
		}
		srcTok := tMu
		if src == "default" {
			srcTok = tSb
		}
		l = L(l, col(name, 16), col(T(ro.Does, tMu), doesW-1), spaces(1, ""), col(hot(model), 8), col(hot(effort), 8), col(T(sourceText(src), srcTok), 8))
		if isSel {
			focus = len(out)
		}
		out = append(out, indent(pick([]line{l}, w-4, tBg1, isSel, 3), 2)...)
	}
	out = append(out, nil)

	// modules | tools
	var colW [2]int
	stacked := w < 100
	if stacked {
		colW = [2]int{w - 4, w - 4}
	} else {
		colW[0] = min((w-4-2)/2, 57) // modules keep their width at 120; tools take the rest
		colW[1] = w - 4 - 2 - colW[0]
	}
	mods := []line{L(heading("Módulos"))}
	for i, s := range c.Settings {
		isSel := sel == len(c.Roles)+i
		l := lr(L(lead(isSel), T(s.Label, "")), L(B(valueText(s.Value), "")), colW[0]-6)
		if isSel {
			l = selLine(l)
			focus = len(out) + len(mods)
		}
		mods = append(mods, l)
	}
	toolBase := len(c.Roles) + len(c.Settings)
	tools := []line{L(heading("Herramientas"))}
	for i, t := range c.Tools {
		isSel := sel == toolBase+i
		ld := lead(isSel)
		var l line
		tname := t.Name
		if tname == "nxy" {
			tname = "nxy en el PATH"
		}
		if run := st.busy["tool/"+t.Name]; run != "" {
			l = lr(L(ld, T(st.ic.Pending+" ", tSb), T(tname, "")), L(T(run, tWork)), colW[1]-6)
		} else if t.Found {
			l = lr(L(ld, T(st.ic.Done+" ", tDone), T(tname, "")), L(T(t.Version, tMu)), colW[1]-6)
		} else {
			l = lr(L(ld, T(st.ic.Pending+" ", tSb), T(tname, "")), L(T("no está   ", tSb), keycap("i"), T(" instalar", tMu)), colW[1]-6)
		}
		if isSel {
			l = selLine(l)
			focus = len(out) + len(tools)
		}
		tools = append(tools, l)
	}
	mb, tb := block(mods, colW[0], tBg1, 3), block(tools, colW[1], tBg1, 3)
	if stacked {
		out = append(out, indent(mb, 2)...)
		out = append(out, nil)
		out = append(out, indent(tb, 2)...)
		focus = focusFix(focus, sel, toolBase, len(c.Tools), len(out)-len(tb))
	} else {
		out = append(out, indent(hcat([][]line{mb, tb}, colW[:], 2), 2)...)
	}
	out = append(out, nil)

	// statusline and binary
	sl := c.Statusline
	slSel, binSel := sel == toolBase+len(c.Tools), sel == toolBase+len(c.Tools)+1
	var slText line
	var slHint line
	if run := st.busy["statusline"]; run != "" {
		slText = L(T(run, tWork))
	} else if sl.Installed {
		slText = L(T(st.ic.Done+" instalada", tDone))
		slHint = L(keycap("i"), T(" quitar", tMu))
	} else {
		slText = L(T(st.ic.Pending+" no instalada", tMu))
		slHint = L(keycap("i"), T(" instalar · cambia "+sl.SettingsPath, tMu))
	}
	b := c.Binary
	binText := L(T(b.Installed, ""))
	if b.Installed != b.Wanted {
		binText = append(binText, T(" · el plugin pide "+b.Wanted, tMu))
	}
	switch b.Download {
	case "failed":
		binText = append(binText, T("   ", ""), T(st.ic.Failed+" descarga fallida", tFail))
	case "running":
		binText = append(binText, T("   ", ""), T("descargando…", tWork))
	}
	var binHint line
	if b.ManualCommand != "" {
		binHint = L(keycap("c"), T(" copiar comando", tMu))
	}
	var bl []line
	add := func(selected bool, name string, text, hint line) {
		ld := lead(selected)
		row := func(l line) line {
			if selected {
				return selLine(l)
			}
			return l
		}
		if lw(hint) == 0 {
			bl = append(bl, row(L(ld, col(B(name, ""), 13), text)))
		} else if iw < 100 {
			bl = append(bl, row(L(ld, col(B(name, ""), 13), text)), row(L(ld, spaces(13, ""), hint)))
		} else {
			bl = append(bl, row(lr(L(ld, col(B(name, ""), 13), text), hint, iw)))
		}
	}
	add(slSel, "Statusline", slText, slHint)
	slEnd := len(bl)
	add(binSel, "Binario", binText, binHint)
	base := len(out)
	out = append(out, indent(block(bl, w-4, tBg1, 3), 2)...)
	switch {
	case slSel:
		focus = base
	case binSel:
		focus = base + slEnd
	}
	return out, focus
}

// focusFix places the focus of a tool row when the two blocks are stacked.
func focusFix(focus, sel, roles, tools, toolsStart int) int {
	if sel >= roles && sel < roles+tools {
		return toolsStart + 1 + sel - roles
	}
	return focus
}
