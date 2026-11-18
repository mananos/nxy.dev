package tui

import (
	"regexp"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/mananos/nxy.dev/v2/internal/views"
)

// memUndoWindow is how long a delete can be undone before its command would run: a constant
// of nxy, not a setting.
const memUndoWindow = 5 * time.Second

// memExpireMsg says the undo window of a delete is over.
type memExpireMsg struct{ seq int }

// takeCmd hands the program the command a key left behind (the undo timer).
func (st *state) takeCmd() tea.Cmd {
	c := st.cmd
	st.cmd = nil
	return c
}

func (st *state) memFindItem(key string) *memItem {
	for _, sc := range st.memScopes() {
		mem := st.data.Memory[sc]
		for _, sec := range memSecs {
			for _, it := range st.memItems(mem, sc, sec.id) {
				if it.key() == key {
					return it
				}
			}
		}
	}
	return nil
}

func itemAction(it *memItem, id string) (views.Action, bool) {
	var list []views.Action
	switch it.kind {
	case "rule":
		list = it.rule.Actions
	case "learn":
		list = it.learn.Actions
	}
	for _, a := range list {
		if a.ID == id {
			return a, true
		}
	}
	return views.Action{}, false
}

// memOpenEdit opens the fields of a rule (text, question, applies to) or a learning (text, files).
func (st *state) memOpenEdit(it *memItem) {
	m := st.memUI()
	m.edit, m.eid, m.efield = true, it.key(), 0
	st.editing = true
	switch it.kind {
	case "rule":
		m.ekeys = []string{"text", "question", "paths"}
		m.efields = []textinput.Model{newInput(it.rule.Text), newInput(it.rule.Question), newInput(strings.Join(it.rule.Paths, ", "))}
	default:
		m.ekeys = []string{"text", "paths"}
		m.efields = []textinput.Model{newInput(it.learn.Text), newInput(strings.Join(it.learn.Files, ", "))}
	}
	m.focusField(0)
}

var placeholderRe = regexp.MustCompile(`\{([a-z]+)\}`)

// memOpenForm opens one single-line field per placeholder of the action, in order of
// appearance. Enter fills them in and runs the action; esc runs nothing.
func (st *state) memOpenForm(label string, a views.Action, after func()) {
	m := st.memUI()
	var keys []string
	seen := map[string]bool{}
	for _, x := range a.Argv {
		for _, g := range placeholderRe.FindAllStringSubmatch(x, -1) {
			if !seen[g[1]] {
				seen[g[1]] = true
				keys = append(keys, g[1])
			}
		}
	}
	if len(keys) == 0 {
		return
	}
	a.Confirm = "" // the form is the question
	m.edit, m.eform, m.elabel, m.eact, m.eafter, m.eid = true, true, label, a, after, ""
	m.ekeys = keys
	m.efields = make([]textinput.Model, len(keys))
	for i := range keys {
		m.efields[i] = newInput("")
	}
	st.editing = true
	m.focusField(0)
}

// substitute replaces every `{name}` of argv with its value in one pass, so a value that
// itself carries `{algo}` stays literal. It reports whether a placeholder had no value.
func substitute(argv []string, vals map[string]string, trim bool) ([]string, bool) {
	out := make([]string, len(argv))
	unfilled := false
	for i, x := range argv {
		out[i] = placeholderRe.ReplaceAllStringFunc(x, func(m string) string {
			v, ok := vals[m[1:len(m)-1]]
			if !ok {
				unfilled = true
				return m
			}
			if trim {
				v = strings.TrimSpace(v)
			}
			return v
		})
	}
	return out, unfilled
}

// fillForm substitutes the values into argv. An optional flag (--paths, --roles) whose value
// is left empty is dropped together with its flag. It reports whether a placeholder was left
// without a value.
func fillForm(argv []string, vals map[string]string) ([]string, bool) {
	kept := make([]string, 0, len(argv))
	for i := 0; i < len(argv); i++ {
		x := argv[i]
		if strings.HasPrefix(x, "--") && i+1 < len(argv) {
			if g := placeholderRe.FindStringSubmatch(argv[i+1]); g != nil && g[0] == argv[i+1] &&
				(g[1] == "paths" || g[1] == "roles") && strings.TrimSpace(vals[g[1]]) == "" {
				i++
				continue
			}
		}
		kept = append(kept, x)
	}
	return substitute(kept, vals, true)
}

// memFormEnter runs the form; a required field left empty keeps it open.
func (st *state) memFormEnter() intent {
	m := st.memUI()
	vals := map[string]string{}
	for i, f := range m.efields {
		k := m.ekeys[i]
		if strings.TrimSpace(f.Value()) == "" && k != "paths" && k != "roles" {
			return intent{toast: "Falta " + fieldLabels[k] + "."}
		}
		vals[k] = f.Value()
	}
	a, label, after := m.eact, m.elabel, m.eafter
	st.memCloseEdit()
	var unfilled bool
	a.Argv, unfilled = fillForm(a.Argv, vals)
	return intent{toast: st.runAction(actionReq{label: label, a: a, after: after, filled: !unfilled})}
}

// memFormLines is the right side while a form is open.
func (st *state) memFormLines(ri int) []line {
	m := st.memUI()
	out := []line{lr(L(B(m.elabel, tAc)), nil, ri), nil}
	for i, f := range m.efields {
		out = append(out, L(T(fieldLabels[m.ekeys[i]], tSb)))
		out = append(out, fieldLines(f, i == m.efield, ri)...)
		if i < len(m.efields)-1 {
			out = append(out, nil)
		}
	}
	return append(out, nil, L(T("enter la crea · esc cancela · aplica a y roles pueden quedar vacíos", tSb)))
}

func (m *memUI) focusField(i int) {
	m.efield = i
	for j := range m.efields {
		if j == i {
			m.efields[j].Focus()
		} else {
			m.efields[j].Blur()
		}
	}
}

func (st *state) memCloseEdit() {
	m := st.memUI()
	m.edit, m.eform, m.eafter = false, false, nil
	m.efields, m.ekeys = nil, nil
	st.editing = m.search
}

var fieldLabels = map[string]string{"text": "texto", "question": "pregunta", "paths": "aplica a", "roles": "roles"}

// fieldLines draws a field: the value wrapped, with the cursor in the focused one.
func fieldLines(ti textinput.Model, focus bool, w int) []line {
	var s string
	if focus {
		rs := []rune(ti.Value())
		pos := min(max(ti.Position(), 0), len(rs))
		s = string(rs[:pos]) + "▏" + string(rs[pos:])
	} else {
		s = ti.Value()
	}
	var out []line
	for _, x := range wrap(s, w-2) {
		var l line
		if before, after, ok := strings.Cut(x, "▏"); ok {
			l = L(" ", T(before, ""), T("▏", tAc), T(after, ""))
		} else {
			l = L(" ", T(x, ""))
		}
		out = append(out, bgize(fit(l, w), tBg2))
	}
	return out
}

// memEditLines is the right side while an item is edited in place.
func (st *state) memEditLines(it *memItem, ri int) []line {
	m := st.memUI()
	out := []line{lr(L(B(it.id, tAc), "   ", T("editando", tNeed)), nil, ri), nil}
	for i, f := range m.efields {
		out = append(out, L(T(fieldLabels[m.ekeys[i]], tSb)))
		out = append(out, fieldLines(f, i == m.efield, ri)...)
		if i < len(m.efields)-1 {
			out = append(out, nil)
		}
	}
	return append(out, nil, L(T("sigue siendo "+it.id+", con sus contadores", tSb)))
}

func (st *state) memEditKey(k string) intent {
	m := st.memUI()
	n := len(m.efields)
	switch k {
	case "esc":
		st.memCloseEdit()
		return intent{toast: "Sin cambios."}
	case "tab":
		m.focusField((m.efield + 1) % n)
	case "shift+tab":
		m.focusField((m.efield + n - 1) % n)
	case "enter":
		if m.eform {
			return st.memFormEnter()
		}
		it := st.memFindItem(m.eid)
		vals := map[string]string{}
		for i, f := range m.efields {
			vals[m.ekeys[i]] = f.Value()
		}
		st.memCloseEdit()
		if it == nil {
			return intent{}
		}
		a, ok := itemAction(it, "edit")
		if !ok {
			return intent{toast: "Guardada " + it.id + " · el comando todavía no existe"}
		}
		argv, unfilled := substitute(a.Argv, vals, false)
		a.Argv = argv
		return intent{toast: st.runAction(actionReq{label: "Guardar " + it.id, a: a, filled: !unfilled})}
	default:
		if msg := keyMsg(k); msg != nil {
			m.efields[m.efield], _ = m.efields[m.efield].Update(msg)
		}
	}
	return intent{}
}

// memConfirmKey answers the "¿Borrar?" of the detail. The item hides at once; the command
// would only run when the undo window is over.
func (st *state) memConfirmKey(scope, k string) intent {
	m := st.memUI()
	switch k {
	case "y":
		key := m.confirm
		m.confirm = ""
		it := st.memFindItem(key)
		if it == nil {
			return intent{}
		}
		a, _ := itemAction(it, "delete")
		a.Confirm = "" // the ¿Borrar? of the detail was the question
		restore, _ := itemAction(it, "restore")
		id := it.id
		t := st.runAction(actionReq{label: "borrar " + id, a: a, after: func() {
			m.deleted[key] = true
			if len(restore.Argv) == 0 {
				return
			}
			m.seq++
			seq := m.seq
			m.undo = &memUndo{key: key, id: id, seq: seq, act: restore}
			st.cmd = tea.Batch(st.cmd, tea.Tick(memUndoWindow, func(time.Time) tea.Msg { return memExpireMsg{seq} }))
		}})
		return intent{toast: t}
	case "n", "esc":
		m.confirm = ""
	}
	return intent{}
}

// memUndo brings back the last deleted item.
func (st *state) memUndo() intent {
	m := st.memUI()
	if m.undo == nil {
		return intent{}
	}
	u := *m.undo
	m.undo = nil
	return intent{toast: st.runAction(actionReq{label: "restaurar " + u.id, a: u.act, after: func() { delete(m.deleted, u.key) }})}
}

// onMemExpire is the end of the undo window: the delete can no longer be undone.
func (st *state) onMemExpire(seq int) {
	m := st.memUI()
	if m.undo == nil || m.undo.seq != seq {
		return
	}
	m.undo = nil
}
