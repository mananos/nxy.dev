package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/views"
)

func init() { register(memoryScreen{}) }

// memLimit is how many items a section shows before "↓ N más".
const memLimit = 5

type memoryScreen struct{}

func (memoryScreen) id() string                 { return "memoria" }
func (memoryScreen) section() string            { return "memoria" }
func (memoryScreen) crumb(*state, route) string { return "Memoria" }
func (memoryScreen) captures(st *state, _ route, _ string) bool {
	m := st.memUI()
	return m.search || m.edit || m.confirm != ""
}

func (memoryScreen) keys(st *state, r route) []hint {
	m := st.memUI()
	switch {
	case m.search:
		return []hint{{"abc", "filtra"}, {"↑↓", "mover"}, {"enter", "ir"}, {"tab", "este repo o todos"}, {"esc", "cerrar"}}
	case m.edit:
		return []hint{{"tab", "campo"}, {"enter", "guardar"}, {"esc", "cancelar"}}
	case m.confirm != "":
		return []hint{{"y", "borrar"}, {"n", "no"}}
	}
	var h []hint
	if m.undo != nil {
		h = append(h, hint{"u", "deshacer"})
	}
	h = append(h, hint{"↑↓", "mover"}, hint{"/", "buscar"}, hint{"e", "editar"}, hint{"x", "borrar"}, hint{"+", "nueva"}, hint{"[ ]", "repo"})
	if row, ok := st.memSelected(r); ok && row.kind == "item" {
		if row.it.kind != "rule" {
			h = append(h, hint{"enter", "abrir archivo"})
		}
		if st.memProposalFor(row.it) != nil {
			h = append(h, hint{"y n", "propuesta"})
		}
	}
	return append(h, hint{"esc", "volver"})
}

// memSecs are the sections of the list.
var memSecs = []struct{ id, title string }{{"rules", "Reglas"}, {"learn", "Aprendizajes"}, {"closed", "Features cerradas"}}

// memItem is a rule, a learning or a closed feature of a scope.
type memItem struct {
	scope, kind, id, text string
	rule                  *views.Rule
	learn                 *views.Learning
	closed                *views.ClosedFeature
}

func (it *memItem) key() string { return it.scope + "/" + it.id }

// hay is what the search looks into: text, question and where it applies.
func (it *memItem) hay() string {
	switch it.kind {
	case "rule":
		return it.rule.Text + "\n" + it.rule.Question + "\n" + strings.Join(it.rule.Paths, " ")
	case "learn":
		return it.learn.Text + "\n" + strings.Join(it.learn.Files, " ")
	}
	return it.closed.Title + "\n" + strings.Join(it.closed.Files, " ")
}

// memRow is a line of the list. Gaps, titles of a search and notes are not selectable.
type memRow struct {
	kind   string // head|title|item|more|note|gap
	sec    string
	title  string
	n      int
	folded bool
	text   string
	it     *memItem
}

func (r memRow) selectable() bool { return r.kind == "head" || r.kind == "item" || r.kind == "more" }

func memSel(rows []memRow) []int {
	var out []int
	for i, r := range rows {
		if r.selectable() {
			out = append(out, i)
		}
	}
	return out
}

// memScopes is Global first, then the repos by name.
func (st *state) memScopes() []string {
	var repos []string
	global := false
	for k, m := range st.data.Memory {
		if m.Scope == views.ScopeGlobal {
			global = true
			continue
		}
		repos = append(repos, k)
	}
	sort.Strings(repos)
	if global {
		return append([]string{views.ScopeGlobal}, repos...)
	}
	return repos
}

// memScope is the scope on screen: the one chosen with [ ], the one of the route or the first repo.
func (st *state) memScope(r route) string {
	m := st.memUI()
	if m.scope != "" && st.data.Memory[m.scope] != nil {
		return m.scope
	}
	if st.data.Memory[r.key] != nil {
		return r.key
	}
	scopes := st.memScopes()
	for _, s := range scopes {
		if st.data.Memory[s].Scope != views.ScopeGlobal {
			return s
		}
	}
	if len(scopes) > 0 {
		return scopes[0]
	}
	return ""
}

func (st *state) memItems(mem *views.Memory, scope, sec string) []*memItem {
	m := st.memUI()
	var flagged, rest []*memItem
	add := func(it *memItem, flag bool) {
		if m.deleted[it.key()] {
			return
		}
		if flag {
			flagged = append(flagged, it)
		} else {
			rest = append(rest, it)
		}
	}
	switch sec {
	case "rules":
		for i := range mem.Rules {
			x := &mem.Rules[i]
			add(&memItem{scope: scope, kind: "rule", id: x.ID, text: x.Text, rule: x}, x.Flag != "")
		}
	case "learn":
		for i := range mem.Learnings {
			x := &mem.Learnings[i]
			add(&memItem{scope: scope, kind: "learn", id: x.ID, text: x.Text, learn: x}, x.Flag != "")
		}
	case "closed":
		for i := range mem.Closed {
			x := &mem.Closed[i]
			add(&memItem{scope: scope, kind: "closed", id: x.ID, text: x.Title, closed: x}, false)
		}
	}
	return append(flagged, rest...)
}

func (m *memUI) folded(scope, sec string, mem *views.Memory) bool {
	if v, ok := m.fold[scope+"/"+sec]; ok {
		return v
	}
	return sec == "closed" && mem.Scope != views.ScopeGlobal
}

// memBrowseRows is the list of the scope: what is marked first, five per section, the closed
// features folded.
func (st *state) memBrowseRows(scope string) []memRow {
	mem := st.data.Memory[scope]
	if mem == nil {
		return nil
	}
	m := st.memUI()
	global := mem.Scope == views.ScopeGlobal
	var rows []memRow
	for _, sec := range memSecs {
		if sec.id == "closed" && global {
			continue
		}
		items := st.memItems(mem, scope, sec.id)
		if len(rows) > 0 {
			rows = append(rows, memRow{kind: "gap"})
		}
		title := sec.title
		if global && sec.id == "rules" {
			title = "Reglas tuyas"
		}
		fold := m.folded(scope, sec.id, mem)
		rows = append(rows, memRow{kind: "head", sec: sec.id, title: title, n: len(items), folded: fold})
		if fold {
			continue
		}
		show := items
		if !m.open[scope+"/"+sec.id] && len(items) > memLimit {
			show = items[:memLimit]
		}
		for _, it := range show {
			rows = append(rows, memRow{kind: "item", sec: sec.id, it: it})
		}
		if len(show) < len(items) {
			rows = append(rows, memRow{kind: "more", sec: sec.id, n: len(items) - len(show)})
		}
		if global && sec.id == "learn" && len(items) == 0 {
			rows = append(rows, memRow{kind: "note", text: "no hay globales: cada uno está"}, memRow{kind: "note", text: "atado a archivos de un repo"})
		}
	}
	return rows
}

// memRows is the list on screen: the search results while the search is open.
func (st *state) memRows(scope string) []memRow {
	if st.memUI().search {
		return st.memSearchRows(scope)
	}
	return st.memBrowseRows(scope)
}

// memCursorKey is the cursor of the list: one per scope, and one for the search.
func (st *state) memCursorKey(scope string) route {
	if st.memUI().search {
		return route{screen: "memoria", key: "#q"}
	}
	return route{screen: "memoria", key: scope}
}

// memPos is the cursor as a position among the selectable rows. Without one set yet it is the
// first item, not the title of the first section.
func (st *state) memPos(scope string, rows []memRow) int {
	sel := memSel(rows)
	if len(sel) == 0 {
		return 0
	}
	ck := st.memCursorKey(scope)
	if v, ok := st.cur[ck.ckey()]; ok {
		return min(max(v, 0), len(sel)-1)
	}
	for i, ri := range sel {
		if rows[ri].kind == "item" {
			return i
		}
	}
	return 0
}

// memSelected is the row under the cursor.
func (st *state) memSelected(r route) (memRow, bool) {
	scope := st.memScope(r)
	rows := st.memRows(scope)
	sel := memSel(rows)
	if len(sel) == 0 {
		return memRow{}, false
	}
	return rows[sel[st.memPos(scope, rows)]], true
}

// memSelect puts the cursor on the item id of the scope, opening what hides it.
func (st *state) memSelect(scope, id string) bool {
	m := st.memUI()
	m.scope = scope
	rows := st.memBrowseRows(scope)
	for pass := 0; pass < 2; pass++ {
		for pos, ri := range memSel(rows) {
			if it := rows[ri].it; rows[ri].kind == "item" && it.id == id {
				st.cur[route{screen: "memoria", key: scope}.ckey()] = pos
				return true
			}
		}
		if pass == 0 { // not in sight: unfold and expand every section, then look again
			for _, sec := range memSecs {
				m.fold[scope+"/"+sec.id] = false
				m.open[scope+"/"+sec.id] = true
			}
			rows = st.memBrowseRows(scope)
		}
	}
	return false
}

func memFlag(it *memItem) (string, tok) {
	switch {
	case it.rule != nil && it.rule.Flag == views.FlagUncited:
		return "sin citar", tNeed
	case it.rule != nil && it.rule.Flag == views.FlagLegacy:
		return "de la 1.x", tSb
	case it.learn != nil && it.learn.Flag == views.FlagSeen:
		return fmt.Sprintf("visto %d", it.learn.Seen), tMu
	case it.learn != nil && it.learn.Flag == views.FlagStale:
		return "obsoleto", tSb
	}
	return "", ""
}

// hlSegs is text with the query marked in ac.
func hlSegs(text, q string, base tok) line {
	if q == "" {
		return L(T(text, base))
	}
	lt, lq := strings.ToLower(text), strings.ToLower(q)
	if len(lt) != len(text) || len(lq) != len(q) {
		return L(T(text, base))
	}
	var out line
	for {
		i := strings.Index(lt, lq)
		if i < 0 {
			break
		}
		out = append(out, T(text[:i], base), B(text[i:i+len(lq)], tAc))
		text, lt = text[i+len(lq):], lt[i+len(lq):]
	}
	return append(out, T(text, base))
}

// memRowLine draws a row of the list in li cells.
func (st *state) memRowLine(row memRow, li int, search bool) line {
	m := st.memUI()
	switch row.kind {
	case "head":
		var lead line
		if row.folded {
			lead = L(T("▸ ", tSb))
		}
		return lr(L(lead, heading(row.title)), L(T(fmt.Sprint(row.n), tSb)), li)
	case "title":
		return lr(L(heading(row.title)), L(T(fmt.Sprint(row.n), tSb)), li)
	case "more":
		return L(spaces(5, ""), T(fmt.Sprintf("↓ %d más", row.n), tSb))
	case "note":
		return L(spaces(5, ""), T(row.text, tSb))
	case "item":
		it := row.it
		flag, ft := memFlag(it)
		dim := it.learn != nil && it.learn.Flag == views.FlagStale
		base, idTok := tok(""), tok("")
		if dim {
			base, idTok = tSb, tSb
		}
		id := B(it.id, idTok)
		if dim {
			id = T(it.id, tSb)
		}
		q := ""
		if search {
			q = m.q.Value()
		}
		left := L(col(id, 5), hlSegs(it.text, q, base))
		var right line
		switch {
		case search:
			right = L(T(it.scope, tSb))
		case flag != "":
			right = L(T(flag, ft))
		}
		return lr(left, right, li)
	}
	return nil
}

func fmtDM(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ""
	}
	return t.Format("02/01")
}

func originText(r *views.Rule) string {
	s := r.Origin
	switch r.Origin {
	case "closing":
		s = "cierre de " + r.OriginRef
	case "legacy":
		s = "de la 1.x"
	}
	if d := fmtDM(r.CreatedAt); d != "" {
		s += " · " + d
	}
	return s
}

func times(n int) string {
	switch n {
	case 0:
		return "nunca"
	case 1:
		return "1 vez"
	}
	return fmt.Sprintf("%d veces", n)
}

// kvl is a label and a value that wraps under itself.
func kvl(k, v string, c tok, ri int) []line {
	var out []line
	for i, s := range wrap(v, max(ri-11, 8)) {
		key := ""
		if i == 0 {
			key = k
		}
		out = append(out, L(col(T(key, tSb), 11), T(s, c)))
	}
	return out
}

func textLines(s string, ri int) []line {
	var out []line
	for _, x := range wrap(s, ri) {
		out = append(out, L(x))
	}
	return out
}

func yesNo(y, n string) line {
	return L(keycap("y"), T(" "+y+"   ", tMu), keycap("n"), T(" "+n, tMu))
}

func exLine() line {
	return L(keycap("e"), T(" editar   ", tMu), keycap("x"), T(" borrar", tMu))
}

func proposalKind(k string) (string, tok) {
	switch k {
	case "delete":
		return "borrar", tFail
	case "promote":
		return "promover", tDone
	}
	return "unir", tVer
}

// memProposalFor is the proposal about this item that is still open.
func (st *state) memProposalFor(it *memItem) *views.Proposal {
	mem := st.data.Memory[it.scope]
	if mem == nil {
		return nil
	}
	for i := range mem.Proposals {
		p := &mem.Proposals[i]
		if p.Target == it.id && !st.memUI().answered[it.scope+"/"+p.ID] {
			return p
		}
	}
	return nil
}

// memDetail is the right side: the item under the cursor, in full.
func (st *state) memDetail(row memRow, scope string, search bool, ri int) []line {
	m := st.memUI()
	switch row.kind {
	case "head":
		state := "enter la pliega"
		if row.folded {
			state = "enter la muestra"
		}
		return []line{L(B(row.title, "")), nil, L(T(fmt.Sprintf("%d en total", row.n), tMu)), L(T(state, tSb))}
	case "more":
		return []line{L(B(fmt.Sprintf("%d más", row.n), "")), nil, L(T("enter los muestra", tSb))}
	case "item":
	default:
		return nil
	}
	it := row.it
	if m.edit && m.eid == it.key() {
		return st.memEditLines(it, ri)
	}
	var out []line
	kind, right := "", ""
	switch it.kind {
	case "rule":
		kind, right = "regla", "del repo"
		if st.data.Memory[it.scope].Scope == views.ScopeGlobal {
			kind, right = "regla tuya", "global"
		}
	case "learn":
		kind = "aprendizaje"
		if f, _ := memFlag(it); f != "" {
			right = f
		}
	default:
		kind, right = "feature cerrada", fmtDM(it.closed.ClosedAt)
	}
	if search {
		right = it.scope
	}
	head := lr(L(B(it.id, tAc), "   ", T(kind, tMu)), L(T(right, tSb)), ri)
	out = append(out, head, nil)
	out = append(out, textLines(it.text, ri)...)
	out = append(out, nil)
	var tail []line
	switch it.kind {
	case "rule":
		r := it.rule
		global := st.data.Memory[it.scope].Scope == views.ScopeGlobal
		if global {
			roles := "todos los roles · e elige"
			if len(r.Roles) > 0 {
				roles = strings.Join(r.Roles, ", ")
			}
			out = append(out, kvl("llega a", roles, tMu, ri)...)
		} else {
			out = append(out, kvl("pregunta", r.Question, tMu, ri)...)
			out = append(out, kvl("aplica a", strings.Join(r.Paths, ", "), tMu, ri)...)
		}
		out = append(out, kvl("vino de", originText(r), tMu, ri)...)
		out = append(out, nil)
		out = append(out, kvl("entregada", times(r.Delivered), tMu, ri)...)
		cited := "nunca"
		if r.Cited > 0 {
			cited = fmt.Sprint(r.Cited)
			if d := fmtDM(r.LastCited); d != "" {
				cited += " · la última, " + d
			}
		}
		out = append(out, kvl("citada", cited, tMu, ri)...)
		out = append(out, nil)
		if global {
			tail = append(tail, L(T("si un repo dice lo contrario,", tSb)), L(T("gana el repo", tSb)), nil)
		}
	case "learn":
		l := it.learn
		out = append(out, L(T("archivos", tSb)))
		for _, f := range l.Files {
			out = append(out, L(T(f, tMu)))
		}
		out = append(out, nil)
		if l.Flag == views.FlagStale {
			out = append(out, L(T("sus archivos ya no existen en el repo", tNeed)), nil)
		}
		if len(l.Features) > 0 {
			out = append(out, kvl("visto en", strings.Join(l.Features, " · "), tMu, ri)...)
		}
		out = append(out, kvl("entregado", times(l.Delivered), tMu, ri)...)
		out = append(out, nil)
	case "closed":
		c := it.closed
		out = append(out, kvl("resultado", c.Outcome, tMu, ri)...)
		out = append(out, nil, L(T("archivos", tSb)))
		for _, f := range c.Files {
			out = append(out, L(T(f, tMu)))
		}
		out = append(out, nil, L(T("enter abre el archivo", tSb)))
		return out
	}
	if p := st.memProposalFor(it); p != nil && !search {
		what := map[string]string{"delete": "borrarla", "promote": "pasarlo a regla"}[p.Kind]
		if what == "" {
			what = "unirla"
		}
		out = append(out, L(T("nxy propone "+what, tAc)), yesNo(strings.ToLower(proposalVerb(p)), strings.ToLower(p.Reject.Label)), nil)
	}
	out = append(out, tail...)
	switch {
	case m.confirm == it.key():
		out = append(out, L(T("¿Borrar "+it.id+"?  ", tNeed), keycap("y"), T(" borrar   ", tMu), keycap("n"), T(" no", tMu)))
	case search && it.scope != scope:
		out = append(out, L(T("enter te lleva a "+it.scope, tSb)))
	case search:
		out = append(out, L(T("enter te lleva a la lista", tSb)))
	default:
		out = append(out, exLine())
	}
	return out
}

// proposalVerb is the label of the y of a proposal: "borrar", "promover".
func proposalVerb(p *views.Proposal) string {
	v, _ := proposalKind(p.Kind)
	return v
}

// memScopeBar is the repos and Global; [ ] changes them.
func (st *state) memScopeBar(scope string, aw int) line {
	scopes := st.memScopes()
	chipOf := func(s string) line {
		name := s
		if s == views.ScopeGlobal {
			name = "Global"
		}
		if s == scope {
			return accent(name)
		}
		return L(S(" "+name+" ", "ch-off"))
	}
	var l line
	total := 0
	idx := 0
	for i, s := range scopes {
		if i > 0 {
			if scopes[i-1] == views.ScopeGlobal {
				l = append(l, T("  │  ", tSb))
			} else {
				l = append(l, spaces(1, ""))
			}
		}
		l = append(l, chipOf(s)...)
		if s == scope {
			idx = i
		}
	}
	total = lw(l)
	right := L(B("[ ]", ""), T("  cambiar", tMu), "  ")
	if total+lw(right)+2 > aw { // it does not fit: only the current one
		l = L(chipOf(scope), T(fmt.Sprintf("   %d de %d", idx+1, len(scopes)), tSb))
	}
	return lr(L("  ", l), right, aw)
}

// memTop is what goes above the list: the proposals, the search or the line of Global.
func (st *state) memTop(scope string, aw int) []line {
	m := st.memUI()
	mem := st.data.Memory[scope]
	inner := aw - 6
	switch {
	case m.search:
		return st.memSearchBar(scope, aw)
	case mem.Scope == views.ScopeGlobal:
		return block([]line{lr(L(B("Tuyas, en todos los repos", "")), L(T("se suman a las del repo donde trabaja cada agente", tSb)), inner)}, aw, tBg1, 3)
	}
	var props []line
	for _, p := range mem.Proposals {
		if m.answered[scope+"/"+p.ID] || m.deleted[scope+"/"+p.Target] {
			continue
		}
		label, kt := proposalKind(p.Kind)
		props = append(props, lr(L(tonal(label, kt, 11), "   ", B(p.Target, ""), T("  "+p.Why, tMu)), yesNo(label, strings.ToLower(p.Reject.Label)), inner))
	}
	if len(props) == 0 {
		return nil
	}
	head := lr(L(heading("Propuestas")), L(T("nxy las arma, vos aceptás o no", tSb)), inner)
	return block(append([]line{head}, props...), aw, tBg1, 3)
}

func (memoryScreen) view(st *state, r route, w int) ([]line, int) {
	scope := st.memScope(r)
	mem := st.data.Memory[scope]
	if mem == nil {
		key := "memory/global"
		if st.data.Errs["memory/repo"] != nil {
			key = "memory/repo"
		}
		return st.missing(key, "No hay memoria para mostrar.", w), 0
	}
	m := st.memUI()
	aw := w - 4
	out := []line{nil, st.memScopeBar(scope, w), nil}
	if top := st.memTop(scope, aw); len(top) > 0 {
		out = append(out, indent(top, 2)...)
		out = append(out, nil)
	}

	rows := st.memRows(scope)
	sel := memSel(rows)
	pos := st.memPos(scope, rows)
	selRow := -1
	if len(sel) > 0 {
		selRow = sel[pos]
	}
	two := w >= 100
	lwid, rwid := aw, aw
	if two {
		lwid = min((aw-2)*54/100, 61) // the list keeps its width at 120; the detail takes the rest
		rwid = aw - 2 - lwid
	}
	li := lwid - 6
	var left []line
	focus := len(out)
	for i, row := range rows {
		if i == selRow {
			focus = len(out) + len(left)
		}
		var l line
		if row.kind != "gap" {
			l = st.memRowLine(row, li, m.search)
		}
		left = append(left, pick([]line{l}, lwid, tBg1, i == selRow, 3)...)
	}
	if len(rows) == 0 || (m.search && len(sel) == 0) {
		left = append(left, block([]line{L(T("nada coincide con «"+m.q.Value()+"»", tMu))}, lwid, tBg1, 3)...)
	}
	var right []line
	if selRow >= 0 {
		right = block(append(st.memDetail(rows[selRow], scope, m.search, rwid-6), nil), rwid, tBg1, 3)
	}
	if m.edit && m.eform {
		right = block(append(st.memFormLines(rwid-6), nil), rwid, tBg1, 3)
	}
	if two {
		for _, l := range hcat([][]line{left, right}, []int{lwid, rwid}, 2) {
			out = append(out, L("  ", l))
		}
	} else {
		out = append(out, indent(left, 2)...)
		if len(right) > 0 {
			out = append(out, nil)
			out = append(out, indent(right, 2)...)
		}
	}
	return out, focus
}

func (memoryScreen) key(st *state, r route, k string) intent {
	m := st.memUI()
	scope := st.memScope(r)
	switch {
	case m.search:
		return st.memSearchKey(scope, k)
	case m.edit:
		return st.memEditKey(k)
	case m.confirm != "":
		return st.memConfirmKey(scope, k)
	}
	rows := st.memBrowseRows(scope)
	sel := memSel(rows)
	pos := st.memPos(scope, rows)
	ck := route{screen: "memoria", key: scope}
	var row memRow
	if len(sel) > 0 {
		row = rows[sel[pos]]
	}
	switch k {
	case "up", "k":
		st.setCursor(ck, max(pos-1, 0))
	case "down", "j":
		st.setCursor(ck, min(pos+1, max(len(sel)-1, 0)))
	case "[", "]":
		scopes := st.memScopes()
		for i, s := range scopes {
			if s == scope {
				d := 1
				if k == "[" {
					d = -1
				}
				m.scope = scopes[(i+d+len(scopes))%len(scopes)]
				break
			}
		}
	case "/":
		st.memOpenSearch()
	case "+":
		mem := st.data.Memory[scope]
		if mem == nil {
			return intent{}
		}
		st.memOpenForm(mem.Create.Label, mem.Create, nil)
		return intent{}
	case "u":
		return st.memUndo()
	case "e":
		if row.kind == "item" && row.it.kind != "closed" {
			st.memOpenEdit(row.it)
		}
	case "x":
		if row.kind == "item" && row.it.kind != "closed" {
			m.confirm = row.it.key()
		}
	case "y", "n":
		if row.kind != "item" {
			break
		}
		p := st.memProposalFor(row.it)
		if p == nil {
			return intent{toast: "No hay una propuesta para " + row.it.id + "."}
		}
		pk := scope + "/" + p.ID
		after := func() { m.answered[pk] = true }
		if k == "y" {
			if hasPlaceholder(p.Accept.Argv) {
				st.memOpenForm(p.Accept.Label, p.Accept, after)
				return intent{}
			}
			return intent{toast: st.runAction(actionReq{label: p.Accept.Label, a: p.Accept, after: after})}
		}
		return intent{toast: st.runAction(actionReq{label: "Se queda " + p.Target, a: p.Reject, after: after})}
	case "enter":
		switch row.kind {
		case "head":
			m.fold[scope+"/"+row.sec] = !row.folded
		case "more":
			m.open[scope+"/"+row.sec] = true
		case "item":
			return st.memOpenFile(row.it)
		}
	}
	return intent{}
}

// memTarget sets up a memory target of the dump: the scope, and for the variants the cursor on
// a learning, the search with a query or K3 in edit.
func (st *state) memTarget(target, scope string) {
	m := st.memUI()
	m.scope = scope
	switch target {
	case "memoria.aprendizaje":
		for _, it := range st.memItems(st.data.Memory[scope], scope, "learn") {
			if it.learn.Flag == views.FlagSeen {
				st.memSelect(scope, it.id)
				break
			}
		}
	case "memoria.buscar":
		st.memOpenSearch()
		m.all = true
		m.q = newInput("dto")
	case "memoria.editar":
		if st.memSelect(scope, "K3") {
			if it := st.memFindItem(scope + "/K3"); it != nil {
				st.memOpenEdit(it)
			}
		}
	}
}

// memOpenFile opens the first file of the item that has a view. A closed feature opens its
// files under its own id (the loader brings them); a learning needs an open feature that touches it.
func (st *state) memOpenFile(it *memItem) intent {
	var files []string
	switch it.kind {
	case "learn":
		files = it.learn.Files
	case "closed":
		files = it.closed.Files
	default:
		return intent{}
	}
	for _, f := range files {
		key := it.scope + "/" + f
		if it.kind == "closed" {
			return intent{push: &route{screen: "archivo", key: key, feature: it.closed.ID}}
		}
		_, loaded := st.data.Files[key]
		if feat := st.featureOfFile(key); loaded || feat != "" {
			return intent{push: &route{screen: "archivo", key: key, feature: feat}}
		}
	}
	if len(files) == 0 {
		return intent{}
	}
	return intent{toast: files[0] + " no está en el plan de una feature abierta"}
}
