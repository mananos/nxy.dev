package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/mananos/nxy.dev/internal/views"
)

// memUI is what the memory screen remembers: the scope, the search, the edit in place and the
// deletes that can still be undone.
type memUI struct {
	scope string // chosen with [ ]; empty is the first repo

	search bool
	all    bool // the search looks in every repo, not only this one
	q      textinput.Model

	edit    bool
	eid     string // scope/id being edited
	efields []textinput.Model
	ekeys   []string // text|question|paths of each field
	efield  int

	// a form opened from an action with placeholders (+, Hacerla regla): enter fills the argv
	eform  bool
	elabel string
	eact   views.Action
	eafter func()

	confirm string          // scope/id waiting for y/n
	deleted map[string]bool // scope/id hidden by a delete
	undo    *memUndo        // the last delete, until its command would run
	seq     int

	fold     map[string]bool // scope/section: folded or not (default: only the closed features)
	open     map[string]bool // scope/section: shows more than memLimit
	answered map[string]bool // scope/proposal answered
}

type memUndo struct {
	key string
	id  string
	seq int
	act views.Action
}

func (st *state) memUI() *memUI {
	if st.mem == nil {
		st.mem = &memUI{deleted: map[string]bool{}, fold: map[string]bool{}, open: map[string]bool{}, answered: map[string]bool{}}
	}
	return st.mem
}

// newInput is a text box that draws nothing itself: the screen paints its value and cursor.
func newInput(value string) textinput.Model {
	ti := textinput.New()
	ti.SetVirtualCursor(false)
	ti.SetValue(value)
	ti.CursorEnd()
	ti.Focus()
	return ti
}

// keyMsg turns a key name into what a text box expects; keys it does not know are dropped.
func keyMsg(k string) tea.Msg {
	switch k {
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "delete":
		return tea.KeyPressMsg{Code: tea.KeyDelete}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	if r, n := utf8.DecodeRuneInString(k); n == len(k) && r != utf8.RuneError {
		return tea.KeyPressMsg{Code: r, Text: k}
	}
	return nil
}

func (st *state) memOpenSearch() {
	m := st.memUI()
	m.search, m.all = true, false
	m.q = newInput("")
	st.editing = true
	delete(st.cur, route{screen: "memoria", key: "#q"}.ckey())
}

func (st *state) memCloseSearch() {
	st.memUI().search = false
	st.editing = st.memUI().edit
}

// memSearchRows are the results: the sections that have some, grouped like the list. They
// look in this repo, or in every one after tab.
func (st *state) memSearchRows(scope string) []memRow {
	m := st.memUI()
	q := strings.ToLower(strings.TrimSpace(m.q.Value()))
	scopes := []string{scope}
	if m.all {
		scopes = st.memScopes()
	}
	var rows []memRow
	for _, sec := range memSecs {
		var found []*memItem
		for _, sc := range scopes {
			mem := st.data.Memory[sc]
			if mem == nil {
				continue
			}
			for _, it := range st.memItems(mem, sc, sec.id) {
				if q == "" || strings.Contains(strings.ToLower(it.hay()), q) {
					found = append(found, it)
				}
			}
		}
		if len(found) == 0 {
			continue
		}
		if len(rows) > 0 {
			rows = append(rows, memRow{kind: "gap"})
		}
		rows = append(rows, memRow{kind: "title", sec: sec.id, title: sec.title, n: len(found)})
		for _, it := range found {
			rows = append(rows, memRow{kind: "item", sec: sec.id, it: it})
		}
	}
	return rows
}

// cursorLine is a value with the cursor drawn at its position.
func cursorLine(ti textinput.Model, bold bool) line {
	rs := []rune(ti.Value())
	pos := min(max(ti.Position(), 0), len(rs))
	mk := func(s string) seg {
		if bold {
			return B(s, "")
		}
		return T(s, "")
	}
	return L(mk(string(rs[:pos])), T("▏", tAc), mk(string(rs[pos:])))
}

func (st *state) memSearchBar(scope string, aw int) []line {
	m := st.memUI()
	rows := st.memSearchRows(scope)
	n := 0
	for _, r := range rows {
		if r.kind == "item" {
			n++
		}
	}
	this, all := T("este repo", tAc), T("todos", tSb)
	this.Bold = !m.all
	all.Bold = m.all
	if m.all {
		this.Fg, all.Fg = tSb, tAc
	}
	count := fmt.Sprintf("%d resultados", n)
	if n == 1 {
		count = "1 resultado"
	}
	right := L(T(count+"     ", tSb), this, T("  ·  ", tSb), all, "   ", keycap("tab"), T(" cambia", tMu))
	return block([]line{lr(L(B("/ ", tAc), cursorLine(m.q, true)), right, aw-6)}, aw, tBg1, 3)
}

func (st *state) memSearchKey(scope, k string) intent {
	m := st.memUI()
	ck := route{screen: "memoria", key: "#q"}
	rows := st.memSearchRows(scope)
	sel := memSel(rows)
	pos := min(st.cursor(ck), max(len(sel)-1, 0))
	switch k {
	case "esc":
		st.memCloseSearch()
	case "tab":
		m.all = !m.all
		st.setCursor(ck, 0)
	case "up":
		st.setCursor(ck, max(pos-1, 0))
	case "down":
		st.setCursor(ck, min(pos+1, max(len(sel)-1, 0)))
	case "enter":
		if len(sel) == 0 {
			return intent{}
		}
		it := rows[sel[pos]].it
		st.memCloseSearch()
		st.memSelect(it.scope, it.id)
	default:
		msg := keyMsg(k)
		if msg == nil {
			break
		}
		before := m.q.Value()
		m.q, _ = m.q.Update(msg)
		if m.q.Value() != before {
			st.setCursor(ck, 0)
		}
	}
	return intent{}
}
