package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Options configure the prototype. Width only matters for Render.
type Options struct {
	Data  *Data
	Width int
	Dark  bool
	Nerd  bool
	Plain bool
}

// route is one level of the path: a screen and what it shows (a feature, a memory scope, a file).
type route struct {
	screen string
	key    string
}

func (r route) ckey() string { return r.screen + "/" + r.key }

// hint is a key of the footer.
type hint struct{ key, desc string }

// intent is what a screen asks the app to do after a key.
type intent struct {
	push    *route
	pop     bool
	toast   string
	section string // go to the root of this section
	decide  bool   // open the decision window
	// decideFor is the feature whose question the window shows first (empty: the oldest).
	decideFor string
}

// screen is one screen. Screens hold no state: the cursors and the path live in state.
type screen interface {
	id() string
	section() string
	crumb(st *state, r route) string
	view(st *state, r route, w int) (lines []line, focus int)
	keys(st *state, r route) []hint
	key(st *state, r route, k string) intent
}

// capturer is a screen that keeps some keys the app would otherwise take (tab, esc, q while
// typing in a search box).
type capturer interface {
	captures(st *state, r route, k string) bool
}

var registered = map[string]screen{}

func register(s screen) { registered[s.id()] = s }

// section is one of the four fixed tabs; its root screen has the same id.
type section struct{ id, title string }

var sections = []section{{"inicio", "Inicio"}, {"memoria", "Memoria"}, {"gasto", "Gasto"}, {"config", "Config"}}

func sectionTitle(id string) string {
	for _, s := range sections {
		if s.id == id {
			return s.title
		}
	}
	return id
}

// state is everything the prototype remembers while it runs.
type state struct {
	data      *Data
	th        *theme
	ic        icons
	dark      bool
	nerd      bool
	plain     bool
	darkSet   bool // the user toggled the theme: ignore the terminal's answer
	stack     []route
	cur       map[string]int // cursor of each screen
	toast     string
	help      bool
	width     int
	height    int
	editing   bool // a text box is open: the decision window does not jump in
	postponed map[string]bool
	answered  map[string]bool   // decisions answered in this run (the demo only shows the command)
	decOpen   bool              // the decision window is on top
	decCur    string            // feature of the question the window shows
	folded    map[string]bool   // agents folded in the tree
	mem       *memUI            // the memory screen (see memory_search.go)
	cmd       tea.Cmd           // what a key leaves for the program to run (the undo timer)
	cfg       map[string]string // roles changed in this run: "<role>/model" and "<role>/model#src"
	cfgKind   string            // model or effort: what `r` saves in the repo
}

func newState(o Options) *state {
	st := &state{data: o.Data, dark: o.Dark, nerd: o.Nerd, plain: o.Plain, width: max(o.Width, 80), height: 24,
		stack: []route{{screen: "inicio"}}, cur: map[string]int{}, postponed: map[string]bool{},
		answered: map[string]bool{}, folded: map[string]bool{}}
	st.restyle()
	return st
}

func (st *state) restyle() {
	st.th = newTheme(st.dark, st.plain)
	st.ic = iconSet(st.nerd)
}

func (st *state) top() route { return st.stack[len(st.stack)-1] }

func (st *state) lookup(id string) screen {
	if s, ok := registered[id]; ok {
		return s
	}
	panic("tui: screen " + strconv.Quote(id) + " is not registered")
}

func (st *state) cursor(r route) int { return st.cur[r.ckey()] }

func (st *state) setCursor(r route, i int) { st.cur[r.ckey()] = i }

// currentSection is the section of the root of the path.
func (st *state) currentSection() string { return st.lookup(st.stack[0].screen).section() }

func (st *state) goSection(id string) {
	st.stack = []route{{screen: id}}
	st.toast = ""
}

// pop takes one level out; at the root of a section it goes back to Inicio.
func (st *state) pop() {
	switch {
	case len(st.stack) > 1:
		st.stack = st.stack[:len(st.stack)-1]
	case st.stack[0].screen != "inicio":
		st.goSection("inicio")
	}
	st.toast = ""
}

func (st *state) decisionCount() int { return len(st.pending()) }

func (st *state) apply(in intent) {
	if in.toast != "" {
		st.toast = in.toast
	}
	if in.section != "" {
		st.goSection(in.section)
	}
	if in.push != nil {
		st.stack = append(st.stack, *in.push)
		st.toast = ""
	}
	if in.pop {
		st.pop()
	}
	if in.decide {
		st.openDecision(in.decideFor)
	}
}

// onKey handles one key and tells whether the program should quit.
func (st *state) onKey(k string) (quit bool) {
	if k == "ctrl+c" {
		return true
	}
	if st.help {
		st.help = false
		return false
	}
	if st.decOpen && st.decKey(k) {
		return false
	}
	r := st.top()
	scr := st.lookup(r.screen)
	if c, ok := scr.(capturer); ok && c.captures(st, r, k) {
		st.apply(scr.key(st, r, k))
		return false
	}
	switch k {
	case "q":
		return true
	case "?":
		st.help = true
	case "ctrl+t":
		st.dark, st.darkSet = !st.dark, true
		st.restyle()
	case "ctrl+g":
		st.nerd = !st.nerd
		st.restyle()
	case "ctrl+n":
		st.openDecision("")
	case "tab", "shift+tab":
		d := 1
		if k == "shift+tab" {
			d = -1
		}
		for i, s := range sections {
			if s.id == st.currentSection() {
				st.goSection(sections[(i+d+len(sections))%len(sections)].id)
				break
			}
		}
	case "esc":
		st.pop()
	default:
		st.apply(scr.key(st, r, k))
	}
	return false
}

// Target is a screen of the dump: --dump <ID> --feature <Key>.
type Target struct{ ID, Key string }

// DumpTargets lists what `nxy tui --demo --dump` can print.
func DumpTargets() []Target {
	return []Target{
		{ID: "inicio"}, {ID: "worktrees"},
		{ID: "gasto"}, {ID: "tendencia"}, {ID: "tendencia.semana"}, {ID: "config"},
		{ID: "feature", Key: "F7K2"}, {ID: "feature", Key: "F7K5"},
		{ID: "agentes", Key: "F7K2"}, {ID: "agentes", Key: "F7K5"},
		{ID: "decision"},
		{ID: "ingeniero", Key: "F7K9"}, {ID: "ingeniero.hoy", Key: "F7K9"}, {ID: "ingeniero.claude", Key: "F7K9"},
		{ID: "plan", Key: "F7K2"}, {ID: "plan", Key: "F7K9"},
		{ID: "memoria", Key: "facturacion-api"}, {ID: "memoria", Key: "global"}, {ID: "memoria", Key: "nxy.dev"},
		{ID: "memoria.aprendizaje", Key: "facturacion-api"}, {ID: "memoria.buscar", Key: "facturacion-api"}, {ID: "memoria.editar", Key: "facturacion-api"},
		{ID: "archivo", Key: "facturacion-web/src/app/notas/nota-credito.component.ts"},
		{ID: "archivo", Key: "facturacion-api/src/main/java/ar/dev/facturacion/nota/NotaCreditoService.java"},
		{ID: "archivo.entero", Key: "facturacion-web/src/app/notas/nota-credito.component.ts"},
	}
}

// targetStack is the path that leads to a target; key is what the target shows.
func (st *state) targetStack(id, key string) []route {
	switch id {
	case "inicio", "decision":
		return []route{{screen: "inicio"}}
	case "memoria", "memoria.aprendizaje", "memoria.buscar", "memoria.editar":
		return []route{{screen: "memoria", key: key}}
	case "gasto", "tendencia", "config":
		return []route{{screen: id}}
	case "tendencia.semana":
		return []route{{screen: "tendencia"}}
	case "agentes":
		return []route{{screen: "inicio"}, {screen: "feature", key: key}, {screen: "agentes", key: key}}
	case "ingeniero", "ingeniero.hoy", "ingeniero.claude":
		return []route{{screen: "inicio"}, {screen: "ingeniero", key: key}}
	case "plan":
		return []route{{screen: "inicio"}, {screen: "feature", key: key}, {screen: "plan", key: key}}
	case "archivo", "archivo.entero":
		if f := st.featureOfFile(key); f != "" {
			return []route{{screen: "inicio"}, {screen: "feature", key: f}, {screen: "plan", key: f}, {screen: "archivo", key: key}}
		}
		return []route{{screen: "inicio"}, {screen: "archivo", key: key}}
	}
	return []route{{screen: "inicio"}, {screen: id, key: key}}
}

// validKeys is the sorted list of keys a keyed target accepts, taken from the loaded data.
func (d *Data) validKeys(target string) []string {
	var out []string
	switch {
	case strings.HasPrefix(target, "memoria"):
		for k := range d.Memory {
			out = append(out, k)
		}
	case strings.HasPrefix(target, "archivo"):
		for k := range d.Files {
			out = append(out, k)
		}
	case target == "decision":
		for k := range d.Decisions {
			out = append(out, k)
		}
	default:
		for _, f := range d.Cockpit.Features {
			out = append(out, f.ID)
		}
	}
	sort.Strings(out)
	return out
}

// Render draws a target without running a program (dump and tests). key picks the feature,
// the memory scope or the file; empty is the default of the target.
func Render(o Options, target, key string) (string, error) {
	if o.Data == nil || o.Data.Cockpit == nil {
		return "", fmt.Errorf("tui: no data")
	}
	if o.Width < 40 {
		return "", fmt.Errorf("tui: width %d is too small", o.Width)
	}
	found, keyed := false, false
	for _, t := range DumpTargets() {
		if t.ID == target {
			if !found && key == "" {
				key = t.Key
			}
			found = true
			keyed = keyed || t.Key != ""
		}
	}
	if !found {
		return "", fmt.Errorf("tui: unknown target %q", target)
	}
	if keyed {
		valid := o.Data.validKeys(target)
		i := sort.SearchStrings(valid, key)
		if i >= len(valid) || valid[i] != key {
			return "", fmt.Errorf("tui: %q is not a valid key for %q; valid: %s", key, target, strings.Join(valid, ", "))
		}
	}
	st := newState(o)
	if target == "decision" {
		st.openDecision(key)
		return st.frame(true), nil
	}
	st.stack = st.targetStack(target, key)
	if target == "archivo.entero" {
		st.cur[st.top().ckey()+"#v"] = 1
	}
	if strings.HasPrefix(target, "memoria") {
		st.memTarget(target, key)
	}
	switch target {
	case "tendencia.semana":
		st.cur[st.top().ckey()+"#g"] = 1
	case "ingeniero.hoy":
		st.cur[st.top().ckey()+"#h"] = 1
	case "ingeniero.claude":
		st.cur[st.top().ckey()+"#c"] = 1
	}
	return st.frame(true), nil
}

// model is the Bubble Tea program.
type model struct{ st *state }

// Run opens the interactive prototype.
func Run(o Options) error {
	if o.Data == nil || o.Data.Cockpit == nil {
		return fmt.Errorf("tui: no data")
	}
	_, err := tea.NewProgram(&model{st: newState(o)}).Run()
	return err
}

func (m *model) Init() tea.Cmd { return tea.RequestBackgroundColor }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.st.width, m.st.height = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		if !m.st.darkSet {
			m.st.dark = msg.IsDark()
			m.st.restyle()
		}
	case tea.KeyPressMsg:
		if m.st.onKey(msg.String()) {
			return m, tea.Quit
		}
		return m, m.st.takeCmd()
	case memExpireMsg:
		m.st.onMemExpire(msg.seq)
	}
	return m, nil
}

func (m *model) View() tea.View {
	v := tea.NewView(m.st.frame(false))
	v.AltScreen = true
	v.BackgroundColor = m.st.th.bgColor()
	v.WindowTitle = "nxy"
	if n := m.st.decisionCount(); n > 0 {
		v.WindowTitle = fmt.Sprintf("● nxy: %s", decisionsText(n))
	}
	return v
}

func decisionsText(n int) string {
	if n == 1 {
		return "1 decisión"
	}
	return fmt.Sprintf("%d decisiones", n)
}
