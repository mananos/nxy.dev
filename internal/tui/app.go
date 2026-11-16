package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mananos/nxy.dev/internal/views"
)

// Options configure the TUI. Width only matters for Render.
type Options struct {
	Source views.Source
	InRepo bool // the folder is inside a repo: Memory has the repo and Config the repo layer
	Demo   bool // sample data: ctrl+n simulates a decision
	Width  int
	Dark   bool
	Nerd   bool
	Plain  bool
	// FileFeature is the feature of the file a dump of archivo shows (empty: the one whose plan
	// touches it).
	FileFeature string
	// Watch polls the db: its data version (it changes with every write of another process) and
	// its schema version. Nil (demo, golden): no refresh.
	Watch func(ctx context.Context) (version int64, schema int, err error)
	// Ver is the data_version read before the first load (HaveVer): a change after it reloads.
	Ver     int64
	HaveVer bool
	// Schema is the newest schema this binary knows; a db above it means nxy was updated.
	Schema int
	// OnStale is called once when the TUI goes stale (the db is from a newer nxy); the caller stops
	// the live-TUI heartbeat there so the hooks fall back to the normal dialog.
	OnStale func()
	// Run runs a command of nxy (argv[0] is "nxy") and returns what it printed. Nil (demo): the
	// buttons only say what they would run.
	Run func(ctx context.Context, argv []string) (out string, err error)
	// Getenv, GOOS and LookPath are the system the editor and the clipboard look at; nil: the
	// real ones (tests replace them).
	Getenv   func(string) string
	GOOS     string
	LookPath func(string) (string, error)

	eager bool // load every view up front (tests that drive the screens with keys)
}

// route is one level of the path: a screen and what it shows (a feature, a memory scope, a file).
type route struct {
	screen string
	key    string
	// feature is the feature of a file ("archivo"): its key is the repo and the path only.
	feature string
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
	answered  map[string]bool         // decisions answered in this run (the demo only shows the command)
	decOpen   bool                    // the decision window is on top
	decCur    string                  // feature of the question the window shows
	decIdx    map[string]int          // feature: question of a multi-question ask the window is on
	decAns    map[string][]string     // feature: answers already given to the earlier questions
	decAsk    map[string]string       // feature: the AskID that progress (decIdx, decAns, decMark) belongs to
	decMark   map[string]map[int]bool // feature: options marked with space in a multiSelect question
	folded    map[string]bool         // agents folded in the tree
	mem       *memUI                  // the memory screen (see memory_search.go)
	cmd       tea.Cmd                 // what a key leaves for the program to run (the undo timer)
	cfgKind   string                  // model or effort: what `r` saves in the repo
	ld        *loader                 // where the views come from
	cheapBusy bool                    // a cheap load is in flight
	demo      bool
	dumpFeat  string // the feature of the file a dump shows

	watch      func(ctx context.Context) (int64, int, error)
	schema     int
	haveVer    bool  // lastVer is the first reading
	lastVer    int64 // last data_version seen
	stale      bool  // the db is from a newer nxy
	onStale    func()
	cheapDirty bool  // a refresh came while a cheap load was in flight
	liveOn     bool  // the one-second tick is armed

	run      func(ctx context.Context, argv []string) (string, error)
	confirm  *actionReq        // an action waits for y/n
	busy     map[string]string // row key → the running line of a command in flight
	toastErr bool              // the toast is a failure
	errNext  bool              // the toast runAction just returned is a failure
	cfgPend  map[string]string // value picked with e and not saved: "role/kind" or "setting/id"

	getenv   func(string) string
	goos     string
	lookPath func(string) (string, error)
}

func newState(o Options) *state {
	st := &state{data: newData(), dark: o.Dark, nerd: o.Nerd, plain: o.Plain, width: max(o.Width, 80), height: 24,
		stack: []route{{screen: "inicio"}}, cur: map[string]int{}, postponed: map[string]bool{},
		answered: map[string]bool{}, decIdx: map[string]int{}, decAns: map[string][]string{}, decAsk: map[string]string{},decMark: map[string]map[int]bool{}, folded: map[string]bool{}, demo: o.Demo, dumpFeat: o.FileFeature,
		watch: o.Watch, schema: o.Schema, run: o.Run, busy: map[string]string{}, cfgPend: map[string]string{}}
	st.haveVer, st.lastVer = o.HaveVer, o.Ver
	st.onStale = o.OnStale
	st.getenv, st.goos, st.lookPath = o.Getenv, o.GOOS, o.LookPath
	if st.getenv == nil {
		st.getenv = os.Getenv
	}
	if st.goos == "" {
		st.goos = runtime.GOOS
	}
	if st.lookPath == nil {
		st.lookPath = exec.LookPath
	}
	// the screens read the cockpit without asking: until it loads it is an empty one
	st.data.Cockpit = &views.Cockpit{}
	if o.Source != nil {
		st.ld = &loader{ctx: context.Background(), src: o.Source, inRepo: o.InRepo}
	}
	st.restyle()
	if o.eager {
		st.loadAll()
	}
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
	st.toast, st.toastErr = "", false
}

// pop takes one level out; at the root of a section it goes back to Inicio.
func (st *state) pop() {
	switch {
	case len(st.stack) > 1:
		st.stack = st.stack[:len(st.stack)-1]
	case st.stack[0].screen != "inicio":
		st.goSection("inicio")
	}
	st.toast, st.toastErr = "", false
}

func (st *state) decisionCount() int { return len(st.pending()) }

// waitingCount is the open questions a hook is holding for the TUI: the ones the title announces.
func (st *state) waitingCount() int {
	n := 0
	for _, d := range st.pending() {
		if d.AskID != "" {
			n++
		}
	}
	return n
}

func (st *state) apply(in intent) {
	if in.toast != "" {
		st.toast, st.toastErr = in.toast, st.errNext
	}
	st.errNext = false
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
	if st.confirm != nil {
		st.confirmKey(k)
		return false
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
		if st.demo {
			st.openDecision("")
		}
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

// loadsKey is for a dump of a feature that is not in the cockpit (a plan, the agents or the
// engineer of a feature that is not active): true when the source has the view of the target.
func (st *state) loadsKey(target, key string) bool {
	switch {
	case target == "plan":
		st.data.merge(st.ld.plan(key))
		return st.data.Plans[key] != nil
	case target == "agentes":
		st.data.merge(st.ld.agents(key))
		return st.data.Agents[key] != nil
	case strings.HasPrefix(target, "ingeniero"):
		st.data.merge(st.ld.engineer(key))
		return st.data.Engineer[key] != nil
	}
	return false
}

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
		f := st.dumpFeat
		if f == "" {
			f = st.featureOfFile(key)
		}
		if f != "" {
			return []route{{screen: "inicio"}, {screen: "feature", key: f}, {screen: "plan", key: f}, {screen: "archivo", key: key, feature: f}}
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
		seen := map[string]bool{}
		for k := range d.Files {
			seen[k] = true
		}
		for _, p := range d.Plans {
			for _, f := range p.Impact.Files {
				seen[f.Repo+"/"+f.Path] = true
			}
		}
		for k := range seen {
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
	if o.Source == nil {
		return "", fmt.Errorf("tui: no data")
	}
	if o.Width < 40 {
		return "", fmt.Errorf("tui: width %d is too small", o.Width)
	}
	found := false
	var defaults []string
	for _, t := range DumpTargets() {
		if t.ID == target {
			found = true
			if t.Key != "" {
				defaults = append(defaults, t.Key)
			}
		}
	}
	if !found {
		return "", fmt.Errorf("tui: unknown target %q", target)
	}
	st := newState(o)
	st.onLoad(cheapMsg{r: st.ld.cheap()})
	if err := st.data.Errs["cockpit"]; err != nil {
		return "", fmt.Errorf("tui: %w", err)
	}
	if len(defaults) > 0 {
		isFile := strings.HasPrefix(target, "archivo")
		if isFile && (key == "" || st.dumpFeat == "") {
			for _, f := range st.data.Cockpit.Features {
				st.data.merge(st.ld.plan(f.ID))
			}
		}
		valid := st.data.validKeys(target)
		has := func(k string) bool {
			i := sort.SearchStrings(valid, k)
			return i < len(valid) && valid[i] == k
		}
		switch {
		case key == "":
			for _, k := range defaults {
				if has(k) {
					key = k
					break
				}
			}
			if key == "" && len(valid) > 0 {
				key = valid[0]
			}
			if key == "" {
				return "", fmt.Errorf("tui: nothing to show for %q", target)
			}
		case !isFile && !has(key) && !st.loadsKey(target, key):
			return "", fmt.Errorf("tui: %q is not a valid key for %q; valid: %s", key, target, strings.Join(valid, ", "))
		}
	}
	if target == "decision" {
		st.openDecision(key)
		return st.frame(true), nil
	}
	st.stack = st.targetStack(target, key)
	st.loadRoute(st.top())
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

// Run opens the interactive TUI.
func Run(o Options) error {
	if o.Source == nil {
		return fmt.Errorf("tui: no data")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := newState(o)
	st.ld.ctx = ctx
	_, err := tea.NewProgram(&model{st: st}).Run()
	return err
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, m.st.cheapCmd(), m.st.topCmd(false), m.st.pollCmd())
}

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
		// the screen the key led to may need its view
		return m, tea.Batch(m.st.takeCmd(), m.st.topCmd(false))
	case cheapMsg:
		m.st.onLoad(msg)
		cmds := []tea.Cmd{m.st.liveTickCmd()}
		if m.st.cheapDirty {
			m.st.cheapDirty = false
			cmds = append(cmds, m.st.cheapCmd())
		}
		return m, tea.Batch(cmds...)
	case topMsg:
		m.st.onLoad(msg)
	case pollTick:
		return m, m.st.pollCmd()
	case pollMsg:
		return m, m.st.onPoll(msg)
	case liveTick:
		return m, m.st.onLive()
	case actionDoneMsg:
		return m, tea.Batch(m.st.onActionDone(msg), m.st.takeCmd())
	case editorDoneMsg:
		if msg.err != nil {
			m.st.toast, m.st.toastErr = "el editor falló: "+msg.err.Error(), true
		}
	case clipDoneMsg:
		if msg.err != nil {
			m.st.toast, m.st.toastErr = "no se pudo copiar con "+msg.name+": "+msg.err.Error(), true
		}
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
	if n := m.st.waitingCount(); n > 0 {
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
