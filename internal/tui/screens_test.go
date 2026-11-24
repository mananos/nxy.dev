package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mananos/nxy.dev/v2/internal/views"
)

func testSource(t *testing.T) *jsonSource {
	t.Helper()
	s, err := newJSONSource(os.DirFS("../.."), "testdata/views")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// testData is everything the demo has, to look at what the screens must show.
func testData(t *testing.T) *Data { return testSource(t).d }

func TestScreensFit(t *testing.T) {
	d := testSource(t)
	for _, tg := range DumpTargets() {
		for _, w := range []int{80, 120, 200} {
			for _, nerd := range []bool{false, true} {
				for _, plain := range []bool{true, false} {
					out, err := Render(Options{Source: d, Demo: true, Width: w, Nerd: nerd, Plain: plain}, tg.ID, tg.Key)
					if err != nil {
						t.Fatalf("%s: %v", tg.ID, err)
					}
					if plain && strings.Contains(out, "\x1b") {
						t.Fatalf("%s/%d: ESC in plain", tg.ID, w)
					}
					for i, l := range strings.Split(out, "\n") {
						if n := ansi.StringWidth(l); n > w {
							t.Fatalf("%s/%d nerd=%v plain=%v line %d is %d wide: %q", tg.ID, w, nerd, plain, i+1, n, ansi.Strip(l))
						}
					}
				}
			}
		}
	}
}

func dump(t *testing.T, target string, w int) string {
	t.Helper()
	out, err := Render(Options{Source: testSource(t), Demo: true, Width: w, Plain: true}, target, "")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDemoToday(t *testing.T) {
	if out := dump(t, "inicio", 120); !strings.Contains(out, "hoy $99.97") {
		t.Errorf("inicio demo should show hoy $99.97:\n%s", out)
	}
	if out := dump(t, "tendencia", 120); !strings.Contains(out, "99.97") {
		t.Errorf("tendencia demo should show 99.97:\n%s", out)
	}
}

func TestInicio(t *testing.T) {
	out := dump(t, "inicio", 80)
	for _, chip := range []string{"TE NECESITA", "TRABAJANDO", "VERIFICANDO", "LISTO"} {
		if !strings.Contains(out, " "+chip+" ") {
			t.Errorf("no chip %q", chip)
		}
	}
	a, b, c := strings.Index(out, "TE NECESITA  2"), strings.Index(out, "EN CURSO"), strings.Index(out, "LISTO HOY")
	if a < 0 || b < a || c < b {
		t.Fatalf("groups out of order: %d %d %d", a, b, c)
	}
	if !strings.Contains(out, "3 activos · 1 huérfano") {
		t.Error("no worktrees line")
	}
}

func TestInicioEmpty(t *testing.T) {
	const hint = "/nxy:feature"
	text := func(st *state) string {
		ls, _ := inicioScreen{}.view(st, route{screen: "inicio"}, 100)
		var sb strings.Builder
		for _, l := range ls {
			for _, s := range l {
				sb.WriteString(s.Text)
			}
			sb.WriteString("\n")
		}
		return sb.String()
	}
	st := newState(Options{Width: 100, Plain: true})
	if out := text(st); strings.Contains(out, hint) {
		t.Errorf("without Done there must be no hint:\n%s", out)
	}
	st.data.Loading["cockpit"] = true
	if out := text(st); strings.Contains(out, hint) {
		t.Errorf("loading must show the loading line, not the hint:\n%s", out)
	}
	delete(st.data.Loading, "cockpit")
	st.data.Done["cockpit"] = true
	out := text(st)
	if !strings.Contains(out, "No hay features abiertas") || !strings.Contains(out, hint) || !strings.Contains(out, "/nxy:engineer") {
		t.Errorf("loaded empty cockpit must say how to start one:\n%s", out)
	}
}

// press sends keys to the program the way Bubble Tea does.
func press(m *model, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		case "pgup":
			msg = tea.KeyPressMsg{Code: tea.KeyPgUp}
		case "pgdown":
			msg = tea.KeyPressMsg{Code: tea.KeyPgDown}
		case "home":
			msg = tea.KeyPressMsg{Code: tea.KeyHome}
		case "end":
			msg = tea.KeyPressMsg{Code: tea.KeyEnd}
		case "ctrl+n":
			msg = tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}
		default:
			msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
		}
		m.Update(msg)
	}
}

func testModel(t *testing.T) *model {
	t.Helper()
	m := &model{st: newState(Options{Source: testSource(t), Demo: true, Width: 100, Plain: true, eager: true})}
	m.st.height = 40
	return m
}

func TestNav(t *testing.T) {
	m := testModel(t)
	press(m, "enter")
	if got := m.st.frame(false); !strings.Contains(got, "Inicio › F7K2") {
		t.Fatalf("no path after enter:\n%s", got)
	}
	press(m, "esc")
	if len(m.st.stack) != 1 {
		t.Fatalf("esc did not pop: %v", m.st.stack)
	}
	// the cursor stays where it was after going down and in and out
	press(m, "down", "down", "enter", "esc")
	if got := m.st.cursor(route{screen: "inicio"}); got != 2 {
		t.Fatalf("cursor %d, want 2", got)
	}
	// F7K1 (third in the order of the screen) is working: enter opens its feature
	press(m, "enter")
	if top := m.st.top(); top.screen != "feature" || top.key != "F7K1" {
		t.Fatalf("top %+v", top)
	}
	press(m, "esc", "tab")
	if m.st.currentSection() != "memoria" {
		t.Fatalf("section %q", m.st.currentSection())
	}
	press(m, "esc")
	if m.st.currentSection() != "inicio" || len(m.st.stack) != 1 {
		t.Fatalf("esc at the root of a section did not go to Inicio: %v", m.st.stack)
	}
}

func TestTabWalksSections(t *testing.T) {
	m := testModel(t)
	at := func(sec, scr string) {
		t.Helper()
		if m.st.currentSection() != sec || m.st.top().screen != scr {
			t.Fatalf("at %s/%s, want %s/%s", m.st.currentSection(), m.st.top().screen, sec, scr)
		}
	}
	at("inicio", "inicio")
	press(m, "tab")
	at("memoria", "memoria")
	press(m, "tab")
	at("gasto", "gasto")
	press(m, "tab")
	at("gasto", "tendencia")
	press(m, "tab")
	at("config", "config")
	press(m, "shift+tab")
	at("gasto", "gasto")
	press(m, "tab")
	at("gasto", "tendencia")
	press(m, "shift+tab")
	at("gasto", "gasto")
	press(m, "shift+tab")
	at("memoria", "memoria")
	press(m, "shift+tab")
	at("inicio", "inicio")
}

func TestEnterOnDesignOpensEngineer(t *testing.T) {
	m := testModel(t)
	press(m, "down", "enter") // second of "Te necesita": F7K9, in design
	if top := m.st.top(); top.screen != "ingeniero" || top.key != "F7K9" {
		t.Fatalf("top %+v", top)
	}
}

func TestWorktreesAndKeysDrop(t *testing.T) {
	m := testModel(t)
	press(m, "w")
	if got := m.st.frame(false); !strings.Contains(got, "Inicio › Worktrees") || !strings.Contains(got, "huérfano") {
		t.Fatalf("worktrees:\n%s", got)
	}
	press(m, "esc")
	// at 40 columns the keys fall from the end and ? stays
	m.st.width = 50
	foot := strings.Split(m.st.frame(false), "\n")
	last := foot[len(foot)-1]
	if !strings.Contains(last, "ayuda") || strings.Contains(last, "worktrees") {
		t.Fatalf("footer %q", last)
	}
}

func TestDecisionBadgeNarrow(t *testing.T) {
	d := testSource(t)
	wide, _ := Render(Options{Source: d, Demo: true, Width: 120, Plain: true}, "inicio", "")
	narrow, _ := Render(Options{Source: d, Demo: true, Width: 80, Plain: true}, "inicio", "")
	if !strings.Contains(wide, "DECISIONES") {
		t.Error("wide bar has no decisions label")
	}
	if first := strings.Split(narrow, "\n")[0]; strings.Contains(first, "DECISI") || !strings.Contains(first, "● 2") {
		t.Errorf("narrow bar: %q", first)
	}
}

func TestFeature(t *testing.T) {
	out, err := Render(Options{Source: testSource(t), Demo: true, Width: 100, Plain: true}, "feature", "F7K2")
	if err != nil {
		t.Fatal(err)
	}
	var marked []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "  ›") {
			marked = append(marked, l)
		}
	}
	if len(marked) != 1 || !strings.Contains(marked[0], "3  Componente de nota parcial") {
		t.Fatalf("the marked row is not batch 3: %q", marked)
	}
	if !strings.Contains(out, "recibió K7 (no la citó) y A5") {
		t.Errorf("the red block does not name K7:\n%s", out)
	}
}

func TestDecisionQueue(t *testing.T) {
	out := dump(t, "decision", 100)
	if !strings.Contains(out, " 1 de 2 ") || !strings.Contains(out, "Lote 3 falló") {
		t.Fatalf("the window does not start with F7K2:\n%s", out)
	}

	m := testModel(t)
	press(m, "ctrl+n")
	if !m.st.decOpen {
		t.Fatal("ctrl+n did not open the window")
	}
	press(m, "tab")
	if got := m.st.frame(false); !strings.Contains(got, " 2 de 2 ") || !strings.Contains(got, "Elegí una bifurcación") {
		t.Fatalf("tab did not go to F7K9:\n%s", got)
	}
	press(m, "esc")
	if !m.st.decOpen || m.st.decCur != "F7K2" {
		t.Fatalf("esc on the first should show the other one: open=%v cur=%s", m.st.decOpen, m.st.decCur)
	}
	press(m, "esc")
	if m.st.decOpen || len(m.st.stack) != 1 || m.st.top().screen != "inicio" {
		t.Fatalf("esc on both should close and stay: %+v", m.st.stack)
	}

	// a text box open: the window does not jump in
	m = testModel(t)
	m.st.editing = true
	press(m, "ctrl+n")
	if m.st.decOpen {
		t.Fatal("the window opened while editing")
	}

	// a fork is read here and answered in Ingeniero
	m = testModel(t)
	press(m, "ctrl+n", "tab", "1")
	if !m.st.decOpen || len(m.st.answered) != 0 {
		t.Fatalf("1 answered a fork: %v", m.st.answered)
	}
	press(m, "enter")
	if m.st.decOpen || m.st.top().screen != "ingeniero" || m.st.top().key != "F7K9" {
		t.Fatalf("enter on a fork: %+v", m.st.stack)
	}
	if got := m.st.frame(false); !strings.Contains(got, "Inicio › F7K9 › Ingeniero") {
		t.Fatalf("path:\n%s", got)
	}

	// answering the first leaves the other
	m = testModel(t)
	press(m, "ctrl+n", "enter")
	if !m.st.answered["ASK-F7K2"] || !m.st.decOpen || m.st.decCur != "F7K9" || m.st.decisionCount() != 1 {
		t.Fatalf("after answering F7K2: %v open=%v cur=%s", m.st.answered, m.st.decOpen, m.st.decCur)
	}
	if !strings.Contains(m.st.toast, "nxy action decide --ask ASK-F7K2 --answer Reintentar") {
		t.Errorf("toast %q", m.st.toast)
	}
}

func TestPlanApproval(t *testing.T) {
	out := dump(t, "plan", 100)
	if strings.Contains(out, "esperando tu aprobación") {
		t.Fatal("the default plan (F7K2) is running, not waiting")
	}
	out, err := Render(Options{Source: testSource(t), Demo: true, Width: 100, Plain: true}, "plan", "F7K9")
	if err != nil {
		t.Fatal(err)
	}
	ls := strings.Split(out, "\n")
	node, risk, detail := -1, -1, -1
	for i, l := range ls {
		switch {
		case strings.Contains(l, "1 Tabla") && node < 0:
			node = i
		case strings.Contains(l, "riesgo alto") && risk < 0:
			risk = i
		case strings.Contains(l, "Lote 1 ·"):
			detail = i
		}
	}
	if node < 0 || risk != node+1 || detail < risk {
		t.Fatalf("the risk is not under the graph (node %d, risk %d, detail %d):\n%s", node, risk, detail, out)
	}
	if !strings.Contains(ls[node], "›") {
		t.Errorf("the cursor is not on batch 1: %q", ls[node])
	}

	prep := func() *model {
		m := testModel(t)
		m.st.stack = m.st.targetStack("plan", "F7K9")
		return m
	}
	m := prep()
	if got := m.st.planBatch(m.st.top(), m.st.data.Plans["F7K9"]); got != 0 {
		t.Fatalf("the cursor starts on %d, want 0", got)
	}
	press(m, "a")
	if !strings.Contains(m.st.toast, "nxy action decide --ask ASK-F7K9-PLAN --answer Aprobar") {
		t.Errorf("toast %q", m.st.toast)
	}

	// the other question can open the window, but not the one of the plan that is on screen
	press(m, "ctrl+n")
	if !m.st.decOpen || m.st.decCur != "F7K2" {
		t.Fatalf("open=%v cur=%s", m.st.decOpen, m.st.decCur)
	}
	press(m, "tab")
	if m.st.decCur != "F7K2" {
		t.Errorf("tab went to the question of the plan on screen: %s", m.st.decCur)
	}
	m = prep()
	m.st.answered["ASK-F7K2"] = true
	press(m, "ctrl+n")
	if m.st.decOpen || !strings.Contains(m.st.toast, "se contesta acá") {
		t.Fatalf("the window jumped over the plan waiting for approval: open=%v toast=%q", m.st.decOpen, m.st.toast)
	}
	// on any other screen it does open
	m = prep()
	m.st.answered["ASK-F7K2"] = true
	press(m, "esc")
	press(m, "ctrl+n")
	if !m.st.decOpen || m.st.decCur != "F7K9" {
		t.Fatalf("on the feature screen the window should open: open=%v cur=%s", m.st.decOpen, m.st.decCur)
	}
}

func TestPlanGraph(t *testing.T) {
	wide, err := Render(Options{Source: testSource(t), Demo: true, Width: 100, Plain: true}, "plan", "F7K2")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wide, "┬") || !strings.Contains(wide, "┘") || !strings.Contains(wide, "├") {
		t.Errorf("the graph at 100 has no connectors:\n%s", wide)
	}
	narrow, err := Render(Options{Source: testSource(t), Demo: true, Width: 80, Plain: true}, "plan", "F7K2")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(narrow, "┬") || !strings.Contains(narrow, "espera a 2,3") {
		t.Errorf("the graph at 80 should fall to rows:\n%s", narrow)
	}
	if !strings.Contains(wide, "aprobado hace 21m · el 2 y el 3 en paralelo") {
		t.Errorf("no summary of the plan:\n%s", wide)
	}
}

func TestShorten(t *testing.T) {
	for in, want := range map[string]string{
		"Componente de nota parcial":  "Componente",
		"Servicio de notas parciales": "Servicio",
		"Integración y e2e":           "Integración",
		"Job y tests":                 "Job y tests",
	} {
		if got := shorten(in, 14); got != want {
			t.Errorf("shorten(%q) = %q, want %q", in, got, want)
		}
	}
}

const componentKey = "facturacion-web/src/app/notas/nota-credito.component.ts"

func fileModel(t *testing.T) *model {
	m := testModel(t)
	m.st.stack = m.st.targetStack("archivo", componentKey)
	return m
}

func TestFileHunks(t *testing.T) {
	out := dump(t, "archivo", 100)
	if !strings.Contains(out, "99 líneas sin cambios") {
		t.Fatalf("no gap between the blocks:\n%s", out)
	}
	if !strings.Contains(out, "Inicio › F7K2 › Plan › nota-credito.component.ts") {
		t.Errorf("no path:\n%s", out)
	}
	m := fileModel(t)
	marked := func() string {
		for _, l := range strings.Split(m.st.frame(true), "\n") {
			if strings.Contains(l, "▸") {
				return l
			}
		}
		return ""
	}
	if l := marked(); !strings.Contains(l, "36") {
		t.Fatalf("the first block should be marked: %q", l)
	}
	press(m, "n")
	if l := marked(); !strings.Contains(l, "141") {
		t.Fatalf("n did not move to the second block: %q", l)
	}
	press(m, "N")
	if l := marked(); !strings.Contains(l, "36") {
		t.Fatalf("N did not go back: %q", l)
	}
	press(m, "v")
	if got := m.st.frame(true); !strings.Contains(got, "readonly titulo") || strings.Contains(got, "sin cambios") {
		t.Errorf("v did not show the whole file:\n%s", got)
	}
	m.st.getenv = func(string) string { return "" }
	m.st.lookPath, m.st.goos = noPath, "linux"
	press(m, "o")
	if !strings.Contains(m.st.toast, "nota-credito.component.ts:36") {
		t.Errorf("toast %q", m.st.toast)
	}
}

func TestRereadToastEnds(t *testing.T) {
	m := fileModel(t)
	press(m, "r")
	if !strings.Contains(m.st.toast, "Releyendo") {
		t.Fatalf("r did not say it is reading: %q", m.st.toast)
	}
	res := newResult()
	res.keys = append(res.keys, "file/"+componentKey)
	m.Update(topMsg{r: res})
	if strings.Contains(m.st.toast, "Releyendo") || m.st.toast == "" || m.st.toastErr {
		t.Errorf("the notice did not end: %q err=%v", m.st.toast, m.st.toastErr)
	}

	press(m, "r")
	res = newResult()
	res.fail("file/"+componentKey, fmt.Errorf("boom"))
	m.Update(topMsg{r: res})
	if !strings.Contains(m.st.toast, "boom") || !m.st.toastErr {
		t.Errorf("the error did not show: %q err=%v", m.st.toast, m.st.toastErr)
	}

	m.st.stack = m.st.targetStack("plan", "F7K2")
	press(m, "r")
	if !strings.Contains(m.st.toast, "Releyendo") {
		t.Fatalf("plan r: %q", m.st.toast)
	}
	res = newResult()
	res.keys = append(res.keys, "plan/F7K2")
	m.Update(topMsg{r: res})
	if strings.Contains(m.st.toast, "Releyendo") || m.st.toast == "" {
		t.Errorf("plan notice did not end: %q", m.st.toast)
	}
}

// scrollFile is a 40 line file with an added line, a pure deletion in the middle and a deletion
// after the last line.
func scrollFile(t *testing.T) *model {
	m := fileModel(t)
	m.st.height = 16
	var c []string
	for i := 1; i <= 40; i++ {
		c = append(c, fmt.Sprintf("line %02d", i))
	}
	m.st.data.Files[componentKey] = &views.File{
		Path: "src/app/notas/nota-credito.component.ts", Repo: "facturacion-web", Content: c,
		Hunks: []views.Hunk{
			{OldStart: 10, NewStart: 10, Lines: []views.DiffLine{{Kind: views.DiffAdd, New: 10, Text: "line 10"}}},
			{OldStart: 19, NewStart: 20, Lines: []views.DiffLine{{Kind: views.DiffDel, Old: 19, Text: "gone mid"}}},
			{OldStart: 40, NewStart: 41, Lines: []views.DiffLine{{Kind: views.DiffDel, Old: 40, Text: "gone end"}}},
		},
		ImportedBy: []views.FileRef{{Path: "src/a.ts", Line: 3}, {Path: "src/b.ts", Line: 5}},
	}
	press(m, "v")
	return m
}

func rowOf(m *model, s string) int {
	for i, l := range strings.Split(m.st.frame(false), "\n") {
		if strings.Contains(l, s) {
			return i
		}
	}
	return -1
}

func TestFileScroll(t *testing.T) {
	m := scrollFile(t)
	if rowOf(m, "line 01") < 0 {
		t.Fatalf("line 01 should show at the start:\n%s", m.st.frame(false))
	}
	press(m, "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j")
	if rowOf(m, "line 01") >= 0 {
		t.Errorf("j did not scroll:\n%s", m.st.frame(false))
	}
	press(m, "up", "k")
	a := rowOf(m, "line 10")
	press(m, "down")
	if b := rowOf(m, "line 10"); b != a-1 {
		t.Errorf("down moved %d -> %d, want one row", a, b)
	}
	press(m, "end")
	if rowOf(m, "gone end") < 0 {
		t.Fatalf("end must show the last row:\n%s", m.st.frame(false))
	}
	last := rowOf(m, "gone end")
	press(m, "pgdown")
	rowOf(m, "")
	press(m, "down")
	rowOf(m, "")
	press(m, "j")
	if rowOf(m, "gone end") != last {
		t.Errorf("scrolled past the end")
	}
	press(m, "pgup")
	if rowOf(m, "gone end") == last {
		t.Errorf("pgup did not go back")
	}
	press(m, "home")
	if rowOf(m, "line 01") < 0 {
		t.Errorf("home did not go to the top")
	}
	press(m, "pgdown")
	if rowOf(m, "line 01") >= 0 {
		t.Errorf("pgdown should move a page:\n%s", m.st.frame(false))
	}
}

func TestFileJump(t *testing.T) {
	m := scrollFile(t)
	if !strings.Contains(m.st.frame(false), "cambio 1/3") {
		t.Errorf("no change counter:\n%s", m.st.frame(false))
	}
	// head: top bar and crumb, then two rows above the change
	press(m, "n")
	if r := rowOf(m, "gone mid"); r != 4 {
		t.Errorf("n did not reach the pure deletion (row %d):\n%s", r, m.st.frame(false))
	}
	if !strings.Contains(m.st.frame(false), "cambio 2/3") {
		t.Errorf("counter after n:\n%s", m.st.frame(false))
	}
	press(m, "n")
	if rowOf(m, "gone end") < 0 || !strings.Contains(m.st.frame(false), "cambio 3/3") {
		t.Errorf("n did not reach the deletion at the end:\n%s", m.st.frame(false))
	}
	press(m, "N")
	if r := rowOf(m, "gone mid"); r != 4 {
		t.Errorf("N back to the deletion: row %d", r)
	}
	press(m, "N")
	if r := rowOf(m, "line 10"); r != 4 {
		t.Errorf("N left the change at row %d, want 4:\n%s", r, m.st.frame(false))
	}
	press(m, "n")
	// free scroll from there (the frame is drawn between keys)
	rowOf(m, "")
	press(m, "down")
	if r := rowOf(m, "gone mid"); r != 3 {
		t.Errorf("down after a jump: row %d, want 3", r)
	}
	// the diff view too
	press(m, "v", "N")
	if r := rowOf(m, "line 10"); r < 0 {
		t.Errorf("N in the diff:\n%s", m.st.frame(false))
	}
}

func TestFileFooterFits100(t *testing.T) {
	m := scrollFile(t)
	m.st.data.Files[componentKey].Path = "src/Nota.java"
	m.st.width = 100
	lines := strings.Split(m.st.frame(false), "\n")
	foot := lines[len(lines)-1]
	for _, want := range []string{"o  editor", "r  releer", "?  ayuda"} {
		if !strings.Contains(foot, want) {
			t.Errorf("footer at 100 columns lacks %q:\n%s", want, foot)
		}
	}
}

func TestFileImporterKeys(t *testing.T) {
	m := scrollFile(t)
	m.st.width = 200
	if got := m.st.frame(false); !strings.Contains(got, "abre a.ts") {
		t.Fatalf("footer should name the file:\n%s", got)
	}
	press(m, "i")
	if got := m.st.frame(false); !strings.Contains(got, "abre b.ts") {
		t.Errorf("i did not move the mark:\n%s", got)
	}
	press(m, "I")
	if got := m.st.frame(false); !strings.Contains(got, "abre a.ts") {
		t.Errorf("I did not move the mark back:\n%s", got)
	}
	press(m, "i", "enter")
	if got := m.st.frame(false); !strings.Contains(got, "b.ts") || m.st.top().screen != "archivo" || !strings.HasSuffix(m.st.top().key, "src/b.ts") {
		t.Errorf("enter did not open the marked file: %+v", m.st.top())
	}
	m = scrollFile(t)
	m.st.data.Files[componentKey].ImportedBy = nil
	if got := m.st.frame(false); strings.Contains(got, "enter abre") || strings.Contains(got, "importador") {
		t.Errorf("no importers, no importer keys:\n%s", got)
	}
}

// the whole file and the blocks of the diff tell the same story
func TestFileContentMatchesHunks(t *testing.T) {
	for k, f := range testData(t).Files {
		if len(f.Content) == 0 {
			continue
		}
		for _, h := range f.Hunks {
			for _, d := range h.Lines {
				if d.Kind == views.DiffDel {
					continue
				}
				if d.New < 1 || d.New > len(f.Content) || f.Content[d.New-1] != d.Text {
					t.Errorf("%s: line %d of the diff is not line %d of the content", k, d.New, d.New)
				}
			}
		}
	}
}

func TestPlanImpactOpensFile(t *testing.T) {
	m := testModel(t)
	m.st.stack = m.st.targetStack("plan", "F7K2")
	press(m, "enter")
	if top := m.st.top(); top.screen != "archivo" || top.key != componentKey {
		t.Fatalf("top %+v", top)
	}
	// a file that does not load opens anyway and shows the error where the code goes
	m.st.stack = m.st.targetStack("plan", "F7K2")
	press(m, "down", "enter")
	top := m.st.top()
	if top.screen != "archivo" || top.feature != "F7K2" || !strings.HasSuffix(top.key, "nota-parcial.model.ts") {
		t.Fatalf("top %+v", top)
	}
	m.st.loadRoute(top)
	if got := m.st.frame(true); !strings.Contains(got, "No se pudo cargar") {
		t.Fatalf("no error block:\n%s", got)
	}
}

func TestHighlight(t *testing.T) {
	var kw *seg
	segs := highlight("typescript", "x.ts", "export class Foo { run() {} }")
	for i := range segs {
		if segs[i].Text == "export" {
			kw = &segs[i]
		}
	}
	if kw == nil || kw.Fg != tVer {
		t.Fatalf("export is not a keyword color: %+v", segs)
	}
	var joined string
	for _, s := range segs {
		joined += s.Text
	}
	if joined != "export class Foo { run() {} }" {
		t.Errorf("the segments do not add up to the code: %q", joined)
	}
	if got := highlight("", "nofile.zzz", "hello"); len(got) != 1 || got[0].Fg != tFg {
		t.Errorf("without a lexer the text should be plain: %+v", got)
	}
}

func TestHighlightMultiLineComment(t *testing.T) {
	got := highlightLines("typescript", "x.ts", []string{"/* first", "   second */", "let x = 1"})
	if len(got) != 3 {
		t.Fatalf("want 3 lines, got %d", len(got))
	}
	for _, s := range got[1] {
		if strings.TrimSpace(s.Text) != "" && s.Fg != tSb {
			t.Errorf("second line of the comment lost its color: %+v", got[1])
		}
	}
	if len(got[1]) == 0 || strings.TrimSpace(got[1][len(got[1])-1].Text) != "second */" {
		t.Errorf("unexpected segments: %+v", got[1])
	}
}

func TestLearnedPlain(t *testing.T) {
	var walk func(n *views.AgentNode)
	walk = func(n *views.AgentNode) {
		for _, s := range n.Learned {
			if strings.HasPrefix(s, "learned:") {
				t.Errorf("%s: %q starts with learned:", n.ID, s)
			}
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	for _, a := range testData(t).Agents {
		walk(&a.Root)
	}
}

func TestEngineerDiagramProblemNotices(t *testing.T) {
	join := func(d views.Diagram) string {
		var sb strings.Builder
		for _, l := range engDiagram(d, 100) {
			for _, s := range l {
				sb.WriteString(s.Text)
			}
			sb.WriteString("\n")
		}
		return sb.String()
	}
	cases := []struct {
		d    views.Diagram
		want string
	}{
		{views.Diagram{Source: "a -> b", Problem: "missing"}, "d2 no está: instalalo desde Config"},
		{views.Diagram{Source: "a -> b", Problem: "error", Error: "syntax x"}, "el dibujo tiene un error: syntax x"},
		{views.Diagram{Source: "a -> b", Problem: "slow"}, "d2 tardó: se reintenta solo"},
	}
	for _, c := range cases {
		got := join(c.d)
		if !strings.Contains(got, c.want) {
			t.Fatalf("%s: %q", c.d.Problem, got)
		}
		if c.d.Problem != "missing" && strings.Contains(got, "instalá") {
			t.Fatalf("%s must not ask to install d2: %q", c.d.Problem, got)
		}
	}
}

func TestEngineerNoAnchor(t *testing.T) {
	m := testModel(t)
	m.st.stack = m.st.targetStack("ingeniero", "F7K9")
	got := m.st.frame(true)
	if strings.Contains(got, "elegiría B") || strings.Contains(got, "salvo que fallen") {
		t.Fatalf("Claude's opinion is visible without pressing c:\n%s", got)
	}
	if !strings.Contains(got, "ninguna elegida") {
		t.Fatalf("no 'ninguna elegida':\n%s", got)
	}
	if strings.Contains(got, "(elegida)") || strings.Contains(got, "✓ A") || strings.Contains(got, "✓ B") {
		t.Fatalf("an option comes chosen:\n%s", got)
	}
	press(m, "c")
	if got := m.st.frame(true); !strings.Contains(got, "elegiría B, salvo que fallen") {
		t.Fatalf("c did not show the opinion:\n%s", got)
	}
	press(m, "h")
	got = m.st.frame(true)
	if !strings.Contains(got, "ASÍ ES HOY") || !strings.Contains(got, "PagoRepo") {
		t.Fatalf("h did not show how it is today:\n%s", got)
	}
}

// inMemory opens the Memoria section of a test program (facturacion-api, the first repo).
func inMemory(t *testing.T) *model {
	t.Helper()
	m := testModel(t)
	press(m, "tab")
	if m.st.top().screen != "memoria" {
		t.Fatalf("top %+v", m.st.top())
	}
	return m
}

func TestMemoryRareOnly(t *testing.T) {
	out := dump(t, "memoria", 100)
	var left []string
	for _, l := range strings.Split(out, "\n") {
		left = append(left, ansi.Cut(l, 0, 52)) // the list is the left column
	}
	list := strings.Join(left, "\n")
	list = list[strings.Index(list, "REGLAS  "):] // under the proposals
	for _, mark := range []string{"sin citar", "visto 3", "obsoleto", "↓ 9 más", "▸ FEATURES CERRADAS"} {
		if !strings.Contains(list, mark) {
			t.Errorf("the list lacks %q:\n%s", mark, list)
		}
	}
	for _, bad := range []string{"entregada", "citada", "veces"} {
		if strings.Contains(list, bad) {
			t.Errorf("the list shows %q, only what is odd goes there:\n%s", bad, list)
		}
	}
	// what is marked comes first, and the numbers live in the detail
	if a, b := strings.Index(list, "K9"), strings.Index(list, "K3"); a < 0 || b < a {
		t.Errorf("K9 (sin citar) should come before K3:\n%s", list)
	}
	if !strings.Contains(out, "entregada") || !strings.Contains(out, "9 veces") {
		t.Errorf("the detail lacks the counters:\n%s", out)
	}
}

func TestMemoryScopes(t *testing.T) {
	g := dump(t, "memoria.aprendizaje", 100)
	if !strings.Contains(g, "nxy propone pasarlo a regla") || !strings.Contains(g, "F6A1 · F7C3 · F7K2") {
		t.Fatalf("learning detail:\n%s", g)
	}
	out, err := Render(Options{Source: testSource(t), Demo: true, Width: 100, Plain: true}, "memoria", "global")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Tuyas, en todos los repos", "REGLAS TUYAS", "de la 1.x", "no hay globales", "gana el repo"} {
		if !strings.Contains(out, want) {
			t.Errorf("global lacks %q:\n%s", want, out)
		}
	}
	m := inMemory(t)
	press(m, "]")
	if !strings.Contains(m.st.frame(true), "K12") {
		t.Fatalf("] did not go to facturacion-web:\n%s", m.st.frame(true))
	}
	press(m, "[", "[")
	if !strings.Contains(m.st.frame(true), "REGLAS TUYAS") {
		t.Fatalf("[ [ did not wrap to Global:\n%s", m.st.frame(true))
	}
}

func TestMemoryOpenFileOfClosedFeature(t *testing.T) {
	m := testModel(t)
	closed := &memItem{scope: "zz-repo", kind: "closed", id: "F0", closed: &views.ClosedFeature{ID: "FEAT-X", Files: []string{"zz/none.go"}}}
	in := m.st.memOpenFile(closed)
	if in.push == nil || in.push.screen != "archivo" || in.push.key != "zz-repo/zz/none.go" || in.push.feature != "FEAT-X" {
		t.Fatalf("closed feature should open its file: %+v", in)
	}
	loose := &memItem{scope: "zz-repo", kind: "learn", id: "A0", learn: &views.Learning{Files: []string{"zz/none.go"}}}
	in = m.st.memOpenFile(loose)
	if in.push != nil || !strings.Contains(in.toast, "zz/none.go no está en el plan de una feature abierta") || strings.Contains(strings.ToLower(in.toast), "demo") {
		t.Fatalf("loose learning toast: %+v", in)
	}
}

func TestMemorySearch(t *testing.T) {
	m := inMemory(t)
	press(m, "/", "d", "t", "o")
	if !m.st.editing {
		t.Fatal("the search is a text box: editing should be on")
	}
	got := m.st.frame(true)
	if strings.Contains(got, "K12") || !strings.Contains(got, "3 resultados") {
		t.Fatalf("this repo only: K3, A4 and F6A1:\n%s", got)
	}
	press(m, "tab")
	got = m.st.frame(true)
	for _, want := range []string{"4 resultados", "K3", "K12", "A4", "F6A1", "facturacion-web", "facturacion-api"} {
		if !strings.Contains(got, want) {
			t.Errorf("search in all lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "K9") || strings.Contains(got, "K8") {
		t.Errorf("search shows what does not match:\n%s", got)
	}
	// enter on K12 goes to its repo
	press(m, "down", "enter")
	if m.st.editing || m.st.memScope(m.st.top()) != "facturacion-web" {
		t.Fatalf("enter did not go to facturacion-web: scope %q editing %v", m.st.memScope(m.st.top()), m.st.editing)
	}
	if row, ok := m.st.memSelected(m.st.top()); !ok || row.it == nil || row.it.id != "K12" {
		t.Fatalf("the cursor is not on K12: %+v", row)
	}
	// esc closes the search and leaves the list as it was
	m = inMemory(t)
	before := m.st.frame(true)
	press(m, "/", "a", "esc")
	if m.st.editing || m.st.frame(true) != before {
		t.Fatalf("esc did not leave the list as it was:\n%s", m.st.frame(true))
	}
}

func TestMemoryEditBlocksDecision(t *testing.T) {
	m := inMemory(t)
	press(m, "e")
	if !m.st.editing || !m.st.mem.edit {
		t.Fatal("e did not open the edit")
	}
	press(m, "ctrl+n")
	m.st.openDecision("")
	if m.st.decOpen {
		t.Fatal("the decision window opened while editing")
	}
	press(m, "tab", "x", "esc")
	if m.st.editing || m.st.mem.edit {
		t.Fatal("esc did not cancel the edit")
	}
	press(m, "ctrl+n")
	if !m.st.decOpen {
		t.Fatal("the window should open once the edit is closed")
	}

	// enter saves: the toast shows the Planned command with the new text
	m = inMemory(t)
	press(m, "e", "!", "enter")
	if !strings.Contains(m.st.toast, "nxy mem edit K9 --text Los logs de facturación usan el MDC con el id de factura!") || strings.Contains(m.st.toast, "todavía no existe") {
		t.Fatalf("toast %q", m.st.toast)
	}
	out := dump(t, "memoria.editar", 100)
	if !strings.Contains(out, "editando") || !strings.Contains(out, "sigue siendo K3, con sus contadores") {
		t.Fatalf("edit dump:\n%s", out)
	}
}

func TestMemoryDeleteUndo(t *testing.T) {
	m := inMemory(t)
	press(m, "x")
	if !strings.Contains(m.st.frame(true), "¿Borrar K9?") {
		t.Fatalf("x did not ask:\n%s", m.st.frame(true))
	}
	press(m, "n")
	if m.st.mem.confirm != "" || len(m.st.mem.deleted) != 0 {
		t.Fatal("n should cancel")
	}
	press(m, "x")
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"}); cmd == nil {
		t.Fatal("deleting should leave the undo timer")
	}
	if !strings.Contains(m.st.toast, "$ nxy mem rm K9") || m.st.mem.undo == nil {
		t.Fatalf("toast %q", m.st.toast)
	}
	if got := m.st.frame(true); strings.Contains(got, "K9   Los logs") {
		t.Fatalf("K9 still on the list:\n%s", got)
	}
	press(m, "u")
	if got := m.st.frame(true); !strings.Contains(got, "K9   Los logs") || m.st.mem.undo != nil {
		t.Fatalf("u did not bring K9 back:\n%s", got)
	}
	// the undo window only lasts so long: after it u does nothing
	press(m, "x", "y")
	m.st.onMemExpire(m.st.mem.undo.seq)
	if m.st.mem.undo != nil {
		t.Fatal("the undo window should be over")
	}
}

func TestMemoryProposalAndCreate(t *testing.T) {
	m := inMemory(t)
	press(m, "y")
	if !strings.Contains(m.st.toast, "nxy mem rm K9") {
		t.Fatalf("y on K9: %q", m.st.toast)
	}
	if strings.Contains(m.st.frame(true), "se entregó 9 veces") {
		t.Fatal("the answered proposal is still on top")
	}
	// + opens a form: text, question and applies to; an empty paths drops its flag
	press(m, "+")
	if !m.st.mem.edit || len(m.st.mem.efields) != 3 {
		t.Fatalf("+ should open a 3 field form: %+v", m.st.mem.ekeys)
	}
	press(m, "enter")
	if !m.st.mem.edit || !strings.Contains(m.st.toast, "Falta") {
		t.Fatalf("an empty text keeps the form open: %q", m.st.toast)
	}
	typeKeys(m, "usar tabs")
	press(m, "tab")
	typeKeys(m, "indentacion")
	press(m, "enter")
	if m.st.mem.edit {
		t.Fatal("enter should close the form")
	}
	if !strings.Contains(m.st.toast, "$ nxy mem rule usar tabs --question indentacion") || strings.Contains(m.st.toast, "--paths") || strings.Contains(m.st.toast, "{") {
		t.Fatalf("+ toast %q", m.st.toast)
	}
	// with paths filled in the flag stays
	press(m, "+")
	typeKeys(m, "a")
	press(m, "tab")
	typeKeys(m, "b")
	press(m, "tab")
	typeKeys(m, "x/**")
	press(m, "enter")
	if !strings.Contains(m.st.toast, "--paths x/**") {
		t.Fatalf("paths toast %q", m.st.toast)
	}
	// esc runs nothing
	press(m, "+", "esc")
	if m.st.mem.edit {
		t.Fatal("esc should close the form")
	}
	// global: roles is a fourth field and an empty one drops --roles
	press(m, "[", "+")
	if len(m.st.mem.efields) != 4 {
		t.Fatalf("global form fields %v", m.st.mem.ekeys)
	}
	typeKeys(m, "g")
	press(m, "tab")
	typeKeys(m, "q")
	press(m, "enter")
	if !strings.Contains(m.st.toast, "--global") || strings.Contains(m.st.toast, "--roles") || strings.Contains(m.st.toast, "--paths") {
		t.Fatalf("global + toast %q", m.st.toast)
	}
}

func typeKeys(m *model, s string) {
	for _, r := range s {
		press(m, string(r))
	}
}

func TestFillForm(t *testing.T) {
	got, un := fillForm([]string{"nxy", "mem", "promote", "A2", "--question", "{question}"}, map[string]string{"question": " por qué "})
	if un || strings.Join(got, "|") != "nxy|mem|promote|A2|--question|por qué" {
		t.Fatalf("got %q", got)
	}
}

func TestFillFormKeepsBracesLiteral(t *testing.T) {
	got, un := fillForm([]string{"mem", "add", "{text}", "--question", "{question}"},
		map[string]string{"text": "usa {question} y {text}", "question": "¿{x}?"})
	if un || strings.Join(got, "|") != "mem|add|usa {question} y {text}|--question|¿{x}?" {
		t.Fatalf("got %q unfilled=%v", got, un)
	}
	if _, un := fillForm([]string{"a", "{nope}"}, map[string]string{"text": "x"}); !un {
		t.Fatal("a placeholder without value must be reported")
	}
}

func TestStatsOrder(t *testing.T) {
	out := dump(t, "gasto", 100)
	out = out[strings.Index(out, "Sesión"):]
	last := -1
	for _, name := range []string{"Gasto", "Cache", "Llamadas", "Turnos"} {
		i := strings.Index(out, name)
		if i < 0 || i <= last {
			t.Fatalf("%s out of order (%d after %d):\n%s", name, i, last, out)
		}
		last = i
	}
}

func TestStatsTab(t *testing.T) {
	m := testModel(t)
	m.st.stack = []route{{screen: "gasto"}}
	press(m, "tab")
	if top := m.st.top(); top.screen != "tendencia" || m.st.currentSection() != "gasto" {
		t.Fatalf("tab in Gasto should open Tendencia: %+v", top)
	}
	press(m, "enter")
	if !strings.Contains(m.st.frame(true), "ÚLTIMAS 8 SEMANAS") {
		t.Fatal("enter did not group by week")
	}
	press(m, "shift+tab")
	if m.st.top().screen != "gasto" {
		t.Fatal("shift+tab in Tendencia should go back to the session")
	}
}

// trendRec records the anchor each Trend call asks for.
type trendRec struct {
	views.Source
	anchors []time.Time
}

func (r *trendRec) Trend(ctx context.Context, anchor time.Time) (views.Trend, error) {
	r.anchors = append(r.anchors, anchor)
	return r.Source.Trend(ctx, anchor)
}

func TestTrendPeriodKeys(t *testing.T) {
	rec := &trendRec{Source: testSource(t)}
	m := &model{st: newState(Options{Source: rec, Demo: true, Width: 100, Plain: true, eager: true})}
	m.st.height = 40
	m.st.stack = []route{{screen: "tendencia"}}
	// last is the anchor the next cheap load asks for.
	last := func() time.Time {
		m.st.ld.cheap(m.st.cheapOpts())
		return rec.anchors[len(rec.anchors)-1]
	}
	near := func(a time.Time, days int) bool {
		d := time.Since(a) - time.Duration(days)*24*time.Hour
		return d > -time.Minute && d < time.Minute
	}
	noToast := func() {
		if strings.Contains(strings.ToLower(m.st.toast), "demo") {
			t.Fatalf("toast mentions the demo: %q", m.st.toast)
		}
	}
	if a := last(); !a.IsZero() {
		t.Fatalf("anchor at start should be zero: %v", a)
	}
	press(m, "left")
	noToast()
	if m.st.trendBack != 1 || !near(last(), 7) {
		t.Fatalf("left: back=%d anchor=%v", m.st.trendBack, rec.anchors[len(rec.anchors)-1])
	}
	press(m, "enter") // by week: the step is 8 weeks
	if !near(last(), 56) {
		t.Fatalf("week anchor: %v", rec.anchors[len(rec.anchors)-1])
	}
	press(m, "enter", "enter", "enter") // back to day
	press(m, "right")
	if m.st.trendBack != 0 || !last().IsZero() {
		t.Fatalf("right: back=%d", m.st.trendBack)
	}
	press(m, "right")
	if m.st.trendBack != 0 {
		t.Fatalf("right below zero: %d", m.st.trendBack)
	}
	for i := 0; i < 13; i++ {
		press(m, "left")
	}
	if m.st.trendBack != 12 || !near(last(), 84) {
		t.Fatalf("limit: back=%d", m.st.trendBack)
	}
	press(m, "tab")
	if m.st.top().screen == "tendencia" || m.st.trendBack != 0 || !last().IsZero() {
		t.Fatalf("leaving should reset: screen=%s back=%d", m.st.top().screen, m.st.trendBack)
	}
	noToast()
}

// statsRec records the session each Stats call asks for.
type statsRec struct {
	views.Source
	ids []string
}

func (r *statsRec) Stats(ctx context.Context, id string) (views.Stats, error) {
	r.ids = append(r.ids, id)
	return r.Source.Stats(ctx, id)
}

func TestStatsPickSession(t *testing.T) {
	rec := &statsRec{Source: testSource(t)}
	m := &model{st: newState(Options{Source: rec, Demo: true, Width: 100, Plain: true, eager: true})}
	m.st.height = 40
	m.st.stack = []route{{screen: "gasto"}}
	want := m.st.data.Stats.Sessions[0]

	press(m, "s")
	if !strings.Contains(m.st.frame(true), want.Label) {
		t.Fatalf("s did not open the list:\n%s", m.st.frame(true))
	}
	press(m, "down", "enter")
	if m.st.statsPick || !m.st.cheapBusy {
		t.Fatalf("enter should close the list and reload: pick=%v busy=%v", m.st.statsPick, m.st.cheapBusy)
	}
	m.st.cheapBusy = false
	m.st.onLoad(m.st.cheapCmd()().(cheapMsg))
	if got := rec.ids[len(rec.ids)-1]; got != want.ID {
		t.Fatalf("Stats asked for %q, want %q", got, want.ID)
	}
	m.st.cheapBusy = false
	m.st.onLoad(m.st.cheapCmd()().(cheapMsg)) // a refresh keeps it
	if got := rec.ids[len(rec.ids)-1]; got != want.ID {
		t.Fatalf("refresh asked for %q, want %q", got, want.ID)
	}
	press(m, "s", "esc")
	if m.st.statsPick || m.st.statsSession != want.ID {
		t.Fatalf("esc should only close the list: pick=%v session=%q", m.st.statsPick, m.st.statsSession)
	}
	if strings.Contains(strings.ToLower(m.st.toast), "demo") {
		t.Fatalf("toast mentions the demo: %q", m.st.toast)
	}
}

func inConfig(t *testing.T) *model {
	t.Helper()
	m := testModel(t)
	m.st.stack = []route{{screen: "config"}}
	return m
}

func TestConfigRepoAction(t *testing.T) {
	m := inConfig(t)
	press(m, "down", "down") // planner, scout, implementer
	press(m, "e")
	if !strings.Contains(m.st.toast, "roles.implementer.model") || strings.Contains(m.st.toast, "{value}") || strings.Contains(m.st.toast, "--repo") {
		t.Fatalf("e toast %q", m.st.toast)
	}
	if !strings.Contains(m.st.toast, "opus") { // sonnet -> opus
		t.Fatalf("e did not cycle the model: %q", m.st.toast)
	}
	press(m, "r")
	if !strings.Contains(m.st.toast, "--repo") || strings.Contains(m.st.toast, "{value}") {
		t.Fatalf("r toast %q", m.st.toast)
	}
}

// The decision and config toasts both come from actionToast: same format, Planned note included.
func TestActionToastShared(t *testing.T) {
	a := views.Action{Argv: []string{"nxy", "config", "set", "x", "{value}"}, Planned: true}
	want := "correría · $ nxy config set x opus · el comando todavía no existe"
	if got := actionToast("correría", withValue(a, "opus")); got != want {
		t.Fatalf("config toast %q, want %q", got, want)
	}
	d := views.Action{Argv: []string{"nxy", "action", "decide"}, Planned: true}
	if got := actionToast("decidir", d); got != "decidir · $ nxy action decide · el comando todavía no existe" {
		t.Fatalf("decision toast %q", got)
	}
	m := inConfig(t)
	press(m, "down", "down")
	press(m, "e")
	if got := m.st.toast; !strings.Contains(got, " · $ nxy config set roles.") {
		t.Fatalf("config toast %q", got)
	}
}

func TestConfigRoles(t *testing.T) {
	for _, w := range []int{80, 120} {
		out := dump(t, "config", w)
		for _, no := range []string{"tester", "librarian"} {
			if strings.Contains(out, no) {
				t.Fatalf("%d: config shows %q", w, no)
			}
		}
		for _, yes := range []string{"planner", "scout", "implementer", "reviewer-quick", "refuter", "documenter"} {
			if !strings.Contains(out, yes) {
				t.Fatalf("%d: no role %q", w, yes)
			}
		}
	}
}

func TestToastWraps(t *testing.T) {
	m := testModel(t)
	m.st.width = 80
	m.st.toast = "decidir · $ nxy action decide --feature F7K2 --option 1 --note una nota bastante larga para que no entre en una sola línea de ochenta columnas FINAL"
	out := m.st.frame(false)
	if !strings.Contains(out, "FINAL") {
		t.Fatalf("toast cut:\n%s", out)
	}
	for i, l := range strings.Split(out, "\n") {
		if n := ansi.StringWidth(l); n > 80 {
			t.Fatalf("line %d is %d wide", i+1, n)
		}
	}
}

func TestLookupUnknownPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("no panic for an unregistered screen")
		}
	}()
	testModel(t).st.lookup("no-existe")
}

func TestRenderUnknownKey(t *testing.T) {
	o := Options{Source: testSource(t), Demo: true, Width: 100, Plain: true}
	if _, err := Render(o, "feature", "ZZZZ"); err == nil || !strings.Contains(err.Error(), "F7K2") {
		t.Fatalf("err %v", err)
	}
	if _, err := Render(o, "memoria", "nada"); err == nil {
		t.Fatal("memory scope accepted")
	}
}

func TestDecisionWindowFitsHeight(t *testing.T) {
	m := testModel(t)
	m.st.height = 20
	press(m, "ctrl+n")
	if !m.st.decOpen {
		t.Fatal("window not open")
	}
	if n := len(strings.Split(m.st.frame(false), "\n")); n != 20 {
		t.Fatalf("frame is %d lines, terminal 20", n)
	}
	m.st.toast = "uno dos tres cuatro cinco seis siete ocho nueve diez once doce trece catorce quince dieciseis diecisiete dieciocho diecinueve veinte veintiuno veintidos"
	if n := len(strings.Split(m.st.frame(false), "\n")); n != 20 {
		t.Fatalf("frame with toast is %d lines", n)
	}
}

func trimW(l string) int { return ansi.StringWidth(strings.TrimRight(l, " ")) }

// TestFullWidth: the content takes the whole width; the right group of a card stays at the edge
// and the impact panel of the plan grows while the left column keeps its width.
func TestFullWidth(t *testing.T) {
	lineWith := func(out, s string) string {
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, s) {
				return l
			}
		}
		t.Fatalf("no line with %q", s)
		return ""
	}
	inicio := dump(t, "inicio", 200)
	if n := trimW(lineWith(inicio, "$4.62")); n < 190 {
		t.Errorf("cost column ends at %d of 200", n)
	}
	d := testSource(t)
	plan := func(w int) string {
		out, err := Render(Options{Source: d, Demo: true, Width: w, Plain: true}, "plan", "F7K2")
		if err != nil {
			t.Fatal(err)
		}
		return lineWith(out, "Impacto")
	}
	p120, p200 := plan(120), plan(200)
	if strings.Index(p120, "Impacto") != strings.Index(p200, "Impacto") {
		t.Error("the left column of the plan moved")
	}
	if trimW(p200)-strings.Index(p200, "Impacto") <= trimW(p120)-strings.Index(p120, "Impacto") {
		t.Error("the impact panel is not wider at 200 than at 120")
	}
}

// TestFrameHeight: the interactive frame is exactly the height of the terminal, no line is wider
// than the width and the key bar is the last line.
func TestFrameHeight(t *testing.T) {
	m := testModel(t)
	m.st.width, m.st.height = 200, 55
	for _, plain := range []bool{true, false} {
		m.st.plain = plain
		m.st.restyle()
		lines := strings.Split(m.st.frame(false), "\n")
		if len(lines) != 55 {
			t.Fatalf("plain=%v: %d lines, want 55", plain, len(lines))
		}
		for i, l := range lines {
			if n := ansi.StringWidth(l); n > 200 {
				t.Fatalf("line %d is %d wide", i+1, n)
			}
		}
		if last := ansi.Strip(lines[54]); !strings.Contains(last, "ayuda") {
			t.Fatalf("last line is not the key bar: %q", last)
		}
	}
}
