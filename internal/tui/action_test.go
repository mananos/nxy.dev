package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mananos/nxy.dev/internal/views"
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
	m.st.stack = m.st.targetStack("plan", "F7K9")
	send(m, key("a"))
	if len(f.calls) != 0 || m.st.toast != "llega en F5.4" {
		t.Fatalf("planned ran %v, toast %q", f.calls, m.st.toast)
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
