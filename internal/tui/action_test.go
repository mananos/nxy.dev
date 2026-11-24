package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mananos/nxy.dev/v2/internal/views"
)

// fakeRun records the commands a button would run.
type fakeRun struct {
	calls [][]string
	out   string
	err   error
}

func (f *fakeRun) run(_ context.Context, argv []string) (string, error) {
	f.calls = append(f.calls, argv)
	return f.out, f.err
}

// settle runs what a key left behind and feeds its messages back, the way the program does;
// timers (the undo window) are given up on.
func settle(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				settle(m, c)
			}
			return
		}
		_, next := m.Update(msg)
		settle(m, next)
	case <-time.After(300 * time.Millisecond):
	}
}

// send is one key through Update, with what it leaves run to the end.
func send(m *model, msg tea.KeyPressMsg) {
	_, cmd := m.Update(msg)
	settle(m, cmd)
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func withRun(m *model, f *fakeRun) *model {
	m.st.run = f.run
	return m
}

func TestActionMemoryDeleteAndRestore(t *testing.T) {
	f := &fakeRun{}
	m := withRun(inMemory(t), f)
	send(m, key("x"))
	send(m, key("y"))
	if want := [][]string{{"nxy", "mem", "rm", "K9"}}; !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("delete ran %v, want %v", f.calls, want)
	}
	if m.st.mem.undo == nil {
		t.Fatal("no undo window after the delete")
	}
	send(m, key("u"))
	if want := []string{"nxy", "mem", "restore", "K9"}; len(f.calls) != 2 || !reflect.DeepEqual(f.calls[1], want) {
		t.Fatalf("undo ran %v, want %v", f.calls, want)
	}
}

// confirming gives the role actions the Confirm the real view brings.
func confirming(m *model) *model {
	for i := range m.st.data.Config.Roles {
		acts := m.st.data.Config.Roles[i].Actions
		for j := range acts {
			acts[j].Confirm = "Guarda en ~/.nxy/config.json"
		}
	}
	return m
}

func TestActionRoleAsksBeforeRunning(t *testing.T) {
	f := &fakeRun{}
	m := confirming(withRun(inConfig(t), f))
	ro := m.st.data.Config.Roles[0]
	next := cycle(ro.Models, ro.Model)
	send(m, key("e"))
	if len(f.calls) != 0 || m.st.confirm == nil {
		t.Fatalf("e should ask first: calls %v confirm %v", f.calls, m.st.confirm)
	}
	if got := m.st.frame(false); !strings.Contains(got, next+" para "+ro.Role) || !strings.Contains(got, "y/n") {
		t.Fatalf("no question in the footer:\n%s", got)
	}
	send(m, key("y"))
	want := []string{"nxy", "config", "set", "roles." + ro.Role + ".model", next}
	if len(f.calls) != 1 || !reflect.DeepEqual(f.calls[0], want) {
		t.Fatalf("ran %v, want %v", f.calls, want)
	}
}

func TestActionRolePendingAfterNo(t *testing.T) {
	f := &fakeRun{}
	m := confirming(withRun(inConfig(t), f))
	ro := m.st.data.Config.Roles[0]
	first := cycle(ro.Models, ro.Model)
	second := cycle(ro.Models, first)
	send(m, key("e"))
	send(m, key("n"))
	send(m, key("e"))
	send(m, key("y"))
	if len(f.calls) != 1 || f.calls[0][4] != second {
		t.Fatalf("after n the next e should move on from %q: %v", first, f.calls)
	}
}

func TestActionNoRepoNoSave(t *testing.T) {
	f := &fakeRun{}
	m := withRun(inConfig(t), f)
	for i := range m.st.data.Config.Roles {
		ro := &m.st.data.Config.Roles[i]
		var keep []views.Action
		for _, a := range ro.Actions {
			if !strings.HasSuffix(a.ID, "_repo") {
				keep = append(keep, a)
			}
		}
		ro.Actions = keep
	}
	for _, h := range (configScreen{}).keys(m.st, route{screen: "config"}) {
		if h.key == "r" {
			t.Fatal("r is offered outside a repo")
		}
	}
	send(m, key("r"))
	if len(f.calls) != 0 || m.st.confirm != nil {
		t.Fatalf("r ran something outside a repo: %v", f.calls)
	}
}

func lifeFeature(id, action string) *views.Cockpit {
	return &views.Cockpit{Features: []views.CockpitFeature{{
		ID: id, Status: views.StatusWorking,
		Actions: []views.Action{{ID: action, Argv: []string{"nxy", "feature", action, id}}},
	}}}
}

func TestActionSpacePausesAndResumes(t *testing.T) {
	f := &fakeRun{}
	m := withRun(testModel(t), f)
	m.st.data.Cockpit = lifeFeature("FEAT-A", "pause")
	send(m, key("space"))
	m.st.data.Cockpit = lifeFeature("FEAT-B", "resume")
	m.st.stack = []route{{screen: "inicio"}, {screen: "feature", key: "FEAT-B"}}
	send(m, key("space"))
	want := [][]string{{"nxy", "feature", "pause", "FEAT-A"}, {"nxy", "feature", "resume", "FEAT-B"}}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("ran %v, want %v", f.calls, want)
	}
	m.st.data.Cockpit = &views.Cockpit{Features: []views.CockpitFeature{{ID: "FEAT-C", Status: views.StatusDone}}}
	m.st.stack = []route{{screen: "inicio"}}
	send(m, key("space"))
	if len(f.calls) != 2 {
		t.Fatalf("a feature without the action ran %v", f.calls)
	}
}

func TestActionPlannedRunsNothing(t *testing.T) {
	f := &fakeRun{}
	m := withRun(testModel(t), f)
	got := m.st.runAction(actionReq{label: "x", a: views.Action{Argv: []string{"nxy", "x"}, Planned: true}})
	if len(f.calls) != 0 || got != "todavía no existe" {
		t.Fatalf("planned ran %v, toast %q", f.calls, got)
	}
}

func TestActionPlaceholderRunsNothing(t *testing.T) {
	f := &fakeRun{}
	m := withRun(testModel(t), f)
	m.st.runAction(actionReq{label: "Nueva regla", a: views.Action{Argv: []string{"nxy", "mem", "rule", "{text}"}}})
	if len(f.calls) != 0 || !m.st.errNext {
		t.Fatalf("ran %v, errNext=%v", f.calls, m.st.errNext)
	}
	m.st.apply(intent{toast: "x"})
	if !m.st.toastErr {
		t.Fatal("the toast is not red")
	}
}

func TestActionFailureShowsFirstLine(t *testing.T) {
	f := &fakeRun{out: "\n  \nno se pudo: falta algo\nsegunda línea\n", err: errors.New("exit status 1")}
	m := withRun(testModel(t), f)
	m.st.data.Cockpit = lifeFeature("FEAT-A", "pause")
	send(m, key("space"))
	if m.st.toast != "no se pudo: falta algo" || !m.st.toastErr {
		t.Fatalf("toast %q err=%v", m.st.toast, m.st.toastErr)
	}
}

func TestActionInstallFailureShowsTheError(t *testing.T) {
	out := "Instalar rtk\n  paso 1\ncurl: (22) The requested URL returned error: 404\nFalló en: descargar rtk\nPara correrlo en una terminal: x\n"
	f := &fakeRun{out: out, err: errors.New("exit status 1")}
	m := withRun(inConfig(t), f)
	c := m.st.data.Config
	c.Tools = []views.Tool{{Name: "rtk", Install: &views.Action{Argv: []string{"nxy", "setup", "run", "rtk", "--yes"}, Confirm: "Instala rtk"}}}
	m.st.setCursor(route{screen: "config"}, len(c.Roles)+len(c.Settings))
	_, _ = m.Update(key("i"))
	send(m, key("y"))
	want := "curl: (22) The requested URL returned error: 404 · Falló en: descargar rtk"
	if m.st.toast != want || !m.st.toastErr {
		t.Fatalf("toast %q err=%v", m.st.toast, m.st.toastErr)
	}
	if got := m.st.frame(false); !strings.Contains(got, "curl: (22)") {
		t.Fatalf("row has no error:\n%s", got)
	}
}

func TestActionStaleRunsNothing(t *testing.T) {
	f := &fakeRun{}
	m := withRun(testModel(t), f)
	m.st.data.Cockpit = lifeFeature("FEAT-A", "pause")
	m.st.stale = true
	send(m, key("space"))
	if len(f.calls) != 0 || m.st.toast != staleText {
		t.Fatalf("stale ran %v, toast %q", f.calls, m.st.toast)
	}
}

func TestActionInstallShowsRunning(t *testing.T) {
	f := &fakeRun{}
	m := withRun(inConfig(t), f)
	c := m.st.data.Config
	c.Tools = []views.Tool{{Name: "rtk", Install: &views.Action{Argv: []string{"nxy", "setup", "run", "rtk", "--yes"}, Confirm: "Instala rtk"}}}
	m.st.setCursor(route{screen: "config"}, len(c.Roles)+len(c.Settings))
	_, _ = m.Update(key("i"))
	if m.st.confirm == nil {
		t.Fatal("i should ask first")
	}
	_, _ = m.Update(key("y"))
	if got := m.st.frame(false); !strings.Contains(got, "instalando rtk…") {
		t.Fatalf("no running line:\n%s", got)
	}
}

// decModel is a model with only d pending and its window open.
func decModel(t *testing.T, f *fakeRun, d *views.Decision) *model {
	t.Helper()
	m := withRun(testModel(t), f)
	m.st.data.Decisions = map[string]*views.Decision{d.FeatureID: d}
	m.st.openDecision("")
	if !m.st.decOpen {
		t.Fatal("the window did not open")
	}
	return m
}

func opts(labels ...string) []views.Option {
	var out []views.Option
	for _, l := range labels {
		out = append(out, views.Option{Label: l})
	}
	return out
}

func TestDecisionMultiQuestionBuildsOneDecide(t *testing.T) {
	f := &fakeRun{}
	d := &views.Decision{FeatureID: "FX", AskID: "ASK-1", Header: "h", Question: "q1", Options: opts("A", "B"),
		Rest: []views.DecisionQuestion{
			{Header: "h2", Question: "q2", MultiSelect: true, Options: opts("X", "Y", "Z")},
			{Header: "h3", Question: "q3", Options: opts("P", "Q")},
		}}
	m := decModel(t, f, d)
	send(m, key("2"))
	send(m, key("enter"))
	if len(f.calls) != 0 || !strings.Contains(m.st.frame(false), "pregunta 2 de 3") {
		t.Fatalf("ran %v early:\n%s", f.calls, m.st.frame(false))
	}
	// multiSelect: mark X and Z with space
	send(m, key("space"))
	send(m, key("down"))
	send(m, key("down"))
	send(m, key("space"))
	send(m, key("enter"))
	send(m, key("enter"))
	want := [][]string{{"nxy", "action", "decide", "--ask", "ASK-1", "--answer", "B", "--answer", "X, Z", "--answer", "P"}}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("ran %v, want %v", f.calls, want)
	}
	if !m.st.answered["ASK-1"] {
		t.Error("not marked as answered")
	}
}

func TestDecisionReaskSameFeatureShows(t *testing.T) {
	f := &fakeRun{}
	mk := func(ask string) *views.Decision {
		return &views.Decision{FeatureID: "FX", AskID: ask, Question: "q", Options: opts("Ver completo", "Aprobar")}
	}
	m := decModel(t, f, mk("ASK-1"))
	send(m, key("1"))
	send(m, key("enter"))
	if len(f.calls) != 1 || !m.st.answered["ASK-1"] {
		t.Fatalf("ran %v, answered %v", f.calls, m.st.answered)
	}
	m.st.data.Decisions = map[string]*views.Decision{"FX": mk("ASK-2")}
	m.st.openDecision("")
	if !m.st.decOpen || m.st.decisionCount() != 1 {
		t.Fatalf("the repeated question is hidden: open=%v count=%d toast=%q", m.st.decOpen, m.st.decisionCount(), m.st.toast)
	}
	send(m, key("2"))
	send(m, key("enter"))
	if len(f.calls) != 2 || !reflect.DeepEqual(f.calls[1], []string{"nxy", "action", "decide", "--ask", "ASK-2", "--answer", "Aprobar"}) {
		t.Fatalf("ran %v", f.calls)
	}
}

func TestDecisionProgressResetsWhenAskIDChanges(t *testing.T) {
	f := &fakeRun{}
	mk := func(ask string) *views.Decision {
		return &views.Decision{FeatureID: "FX", AskID: ask, Question: "q1", Options: opts("A", "B"),
			Rest: []views.DecisionQuestion{{Question: "q2", Options: opts("X", "Y")}}}
	}
	m := decModel(t, f, mk("ASK-1"))
	send(m, key("2"))
	send(m, key("enter"))
	if !strings.Contains(m.st.frame(false), "pregunta 2 de 2") {
		t.Fatalf("did not advance:\n%s", m.st.frame(false))
	}
	m.st.data.Decisions = map[string]*views.Decision{"FX": mk("ASK-2")}
	if got := m.st.frame(false); !strings.Contains(got, "pregunta 1 de 2") {
		t.Fatalf("progress kept for another ask:\n%s", got)
	}
	send(m, key("1"))
	send(m, key("enter"))
	send(m, key("1"))
	send(m, key("enter"))
	want := [][]string{{"nxy", "action", "decide", "--ask", "ASK-2", "--answer", "A", "--answer", "X"}}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("ran %v, want %v", f.calls, want)
	}
}

func TestDecisionReleaseWithT(t *testing.T) {
	f := &fakeRun{}
	rel := views.Action{ID: "release", Argv: []string{"nxy", "action", "decide", "--ask", "ASK-1", "--release"}}
	m := decModel(t, f, &views.Decision{FeatureID: "FX", AskID: "ASK-1", Question: "q", Options: opts("A"), Release: rel})
	send(m, key("t"))
	if want := [][]string{rel.Argv}; !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("ran %v, want %v", f.calls, want)
	}
}

func TestDecisionWithoutAskIDRunsNothing(t *testing.T) {
	f := &fakeRun{}
	m := decModel(t, f, &views.Decision{FeatureID: "FX", Question: "q", Options: opts("A", "B")})
	send(m, key("enter"))
	if len(f.calls) != 0 || m.st.toast != terminalText || m.st.answered["FX"] {
		t.Fatalf("ran %v, toast %q, answered %v", f.calls, m.st.toast, m.st.answered)
	}
	send(m, key("t"))
	if len(f.calls) != 0 || m.st.toast != terminalText {
		t.Fatalf("t ran %v, toast %q", f.calls, m.st.toast)
	}
}

func TestDecisionLateAnswerReloadsInRed(t *testing.T) {
	f := &fakeRun{out: "ya contestada\n", err: errors.New("exit status 1")}
	m := decModel(t, f, &views.Decision{FeatureID: "FX", AskID: "ASK-1", Question: "q", Options: opts("A")})
	send(m, key("enter"))
	if !m.st.toastErr || m.st.toast != "ya contestada" || m.st.answered["FX"] {
		t.Fatalf("toast %q red=%v answered=%v", m.st.toast, m.st.toastErr, m.st.answered)
	}
}

func TestPlanApprovalActions(t *testing.T) {
	f := &fakeRun{}
	m := withRun(testModel(t), f)
	m.st.stack = m.st.targetStack("plan", "F7K9")
	send(m, key("a"))
	send(m, key("c"))
	send(m, key("x"))
	if m.st.confirm == nil {
		t.Fatal("x should ask y/n first")
	}
	if len(f.calls) != 2 {
		t.Fatalf("ran %v before the confirmation", f.calls)
	}
	send(m, key("y"))
	want := [][]string{
		{"nxy", "action", "decide", "--ask", "ASK-F7K9-PLAN", "--answer", "Aprobar"},
		{"nxy", "action", "decide", "--ask", "ASK-F7K9-PLAN", "--answer", "Cambiar"},
		{"nxy", "feature", "stop", "--feature", "F7K9"},
	}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("ran %v, want %v", f.calls, want)
	}
	if m.st.toast != "feature parada" {
		t.Errorf("toast %q", m.st.toast)
	}

	// without a waiting hook a and c only say where to answer
	f2 := &fakeRun{}
	m = withRun(testModel(t), f2)
	m.st.stack = m.st.targetStack("plan", "F7K9")
	p := m.st.data.Plans["F7K9"]
	for i := range p.Actions {
		if p.Actions[i].ID != "stop" {
			p.Actions[i].Argv = nil
		}
	}
	send(m, key("a"))
	if len(f2.calls) != 0 || m.st.toast != terminalText {
		t.Fatalf("a ran %v, toast %q", f2.calls, m.st.toast)
	}
}

func TestEngineerEnterNeedsAskID(t *testing.T) {
	f := &fakeRun{}
	m := withRun(testModel(t), f)
	m.st.stack = m.st.targetStack("ingeniero", "F7K9")
	e := m.st.data.Engineer["F7K9"]
	send(m, key("enter"))
	if len(f.calls) != 1 || !reflect.DeepEqual(f.calls[0][:5], []string{"nxy", "action", "decide", "--ask", "ASK-F7K9"}) {
		t.Fatalf("enter with AskID ran %v", f.calls)
	}
	e = m.st.data.Engineer["F7K9"]
	e.AskID = ""
	send(m, key("enter"))
	if len(f.calls) != 1 || !strings.Contains(m.st.toast, "elegí en el chat") {
		t.Fatalf("enter without AskID ran %v, toast %q", f.calls, m.st.toast)
	}
}
