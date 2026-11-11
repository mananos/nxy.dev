package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/mananos/nxy.dev/internal/views"
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
	m.edit = false
	m.efields, m.ekeys = nil, nil
	st.editing = m.search
}

var fieldLabels = map[string]string{"text": "texto", "question": "pregunta", "paths": "aplica a"}

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
		argv := make([]string, len(a.Argv))
		for i, x := range a.Argv {
			for key, v := range vals {
				x = strings.ReplaceAll(x, "{"+key+"}", v)
			}
			argv[i] = x
		}
		a.Argv = argv
		return intent{toast: actionToast("Guardada "+it.id, a)}
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
		m.deleted[key] = true
		m.seq++
		m.undo = &memUndo{key: key, id: it.id, seq: m.seq, act: a}
		seq := m.seq
		st.cmd = tea.Tick(memUndoWindow, func(time.Time) tea.Msg { return memExpireMsg{seq} })
		return intent{toast: "borrada " + it.id + " · u deshace"}
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
	delete(m.deleted, m.undo.key)
	id := m.undo.id
	m.undo = nil
	return intent{toast: id + " vuelve"}
}

// onMemExpire is the end of the undo window: now the command would run.
func (st *state) onMemExpire(seq int) {
	m := st.memUI()
	if m.undo == nil || m.undo.seq != seq {
		return
	}
	st.toast = actionToast("Borrada "+m.undo.id, m.undo.act)
	m.undo = nil
}
