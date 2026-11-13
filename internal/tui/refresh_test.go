package tui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mananos/nxy.dev/internal/views"
)

// liveSource is the demo with a cockpit the test can change and a count of the cockpit loads.
type liveSource struct {
	*jsonSource
	cockpit views.Cockpit
	loads   atomic.Int32
}

func newLiveSource(t *testing.T) *liveSource {
	t.Helper()
	js := testSource(t)
	return &liveSource{jsonSource: js, cockpit: *js.d.Cockpit}
}

func (s *liveSource) Cockpit(context.Context) (views.Cockpit, error) {
	s.loads.Add(1)
	return s.cockpit, nil
}

func fakeWatch(context.Context) (int64, int, error) { return 1, 1, nil }

func liveModel(t *testing.T, src views.Source, schema int) *model {
	t.Helper()
	st := newState(Options{Source: src, InRepo: true, Width: 100, Dark: true, Plain: true, Watch: fakeWatch, Schema: schema})
	st.onLoad(cheapMsg{r: st.ld.cheap()})
	return &model{st: st}
}

// collect runs a command and what it batches, and returns the messages that arrive soon; ticks
// (which wait longer) are left out.
func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 32)
	var launch func(c tea.Cmd)
	launch = func(c tea.Cmd) {
		go func() {
			m := c()
			if b, ok := m.(tea.BatchMsg); ok {
				for _, x := range b {
					if x != nil {
						launch(x)
					}
				}
				return
			}
			ch <- m
		}()
	}
	launch(cmd)
	var out []tea.Msg
	timeout := time.After(250 * time.Millisecond)
	for {
		select {
		case m := <-ch:
			out = append(out, m)
		case <-timeout:
			return out
		}
	}
}

func feed(m *model, msgs []tea.Msg) {
	for _, x := range msgs {
		m.Update(x)
	}
}

func hasCheap(msgs []tea.Msg) bool {
	for _, x := range msgs {
		if _, ok := x.(cheapMsg); ok {
			return true
		}
	}
	return false
}

func newNeedsYou(id string) views.CockpitFeature {
	return views.CockpitFeature{ID: id, Title: "nueva", Status: views.StatusNeedsYou, Stage: views.StageBatches}
}

func TestRefresh(t *testing.T) {
	src := newLiveSource(t)
	m := liveModel(t, src, 5)
	n := len(m.st.data.Cockpit.Features)

	// the first reading is the baseline: nothing to load
	_, cmd := m.Update(pollMsg{ver: 1, schema: 1})
	if hasCheap(collect(cmd)) {
		t.Fatal("the first reading must not reload")
	}
	// same version: nothing either
	_, cmd = m.Update(pollMsg{ver: 1, schema: 1})
	if hasCheap(collect(cmd)) {
		t.Fatal("same data_version must not reload")
	}

	src.cockpit.Features = append([]views.CockpitFeature{newNeedsYou("NEWX")}, src.cockpit.Features...)
	before := src.loads.Load()
	_, cmd = m.Update(pollMsg{ver: 2, schema: 1})
	msgs := collect(cmd)
	if !hasCheap(msgs) {
		t.Fatal("a new data_version must reload the cheap views")
	}
	feed(m, msgs)
	if src.loads.Load() != before+1 {
		t.Fatalf("cockpit loads %d, want %d", src.loads.Load(), before+1)
	}
	if got := len(m.st.data.Cockpit.Features); got != n+1 {
		t.Fatalf("features %d, want %d", got, n+1)
	}
}

func TestRefreshForgetsFarViews(t *testing.T) {
	src := newLiveSource(t)
	m := liveModel(t, src, 5)
	m.st.data.Done["plan/FEAT-P"] = true
	m.st.data.Done["agents/FEAT-A"] = true
	m.st.haveVer, m.st.lastVer = true, 1
	m.Update(pollMsg{ver: 2, schema: 1})
	if m.st.data.Done["plan/FEAT-P"] || m.st.data.Done["agents/FEAT-A"] {
		t.Fatal("far views must be asked again after a data change")
	}
}

func TestRefreshBaseVersion(t *testing.T) {
	src := newLiveSource(t)
	st := newState(Options{Source: src, InRepo: true, Width: 100, Dark: true, Plain: true, Watch: fakeWatch, Schema: 5, Ver: 1, HaveVer: true})
	st.onLoad(cheapMsg{r: st.ld.cheap()})
	m := &model{st: st}
	_, cmd := m.Update(pollMsg{ver: 2, schema: 1})
	if !hasCheap(collect(cmd)) {
		t.Fatal("a change after the base version must reload on the first poll")
	}
}

func TestCursorFollowsID(t *testing.T) {
	src := newLiveSource(t)
	m := liveModel(t, src, 5)
	st := m.st
	r := route{screen: "inicio"}
	st.onKey("down")
	st.onKey("down")
	order := inicioOrder(st.data.Cockpit)
	if len(order) < 3 {
		t.Fatalf("the demo has %d features, need 3", len(order))
	}
	want := order[st.cursor(r)].ID

	src.cockpit.Features = append([]views.CockpitFeature{newNeedsYou("NEWX")}, src.cockpit.Features...)
	st.onLoad(cheapMsg{r: st.ld.cheap()})

	order = inicioOrder(st.data.Cockpit)
	if got := order[min(st.cursor(r), len(order)-1)].ID; got != want {
		t.Fatalf("cursor on %s, want %s", got, want)
	}
	if order[0].ID != "NEWX" {
		t.Fatalf("the new feature should be first, got %s", order[0].ID)
	}
}

func TestSchemaNewer(t *testing.T) {
	src := newLiveSource(t)
	m := liveModel(t, src, 5)
	m.Update(pollMsg{ver: 1, schema: 5})
	if m.st.stale {
		t.Fatal("same schema is not stale")
	}
	before := src.loads.Load()
	_, cmd := m.Update(pollMsg{ver: 2, schema: 6})
	if cmd != nil {
		t.Fatal("a newer schema must not ask for anything else")
	}
	if !m.st.stale {
		t.Fatal("a newer schema must mark the db stale")
	}
	if out := m.st.frame(false); !strings.Contains(out,"nxy se actualizó") {
		t.Fatalf("no notice in the frame:\n%s", out)
	}
	_, cmd = m.Update(pollMsg{ver: 3, schema: 6})
	if cmd != nil || src.loads.Load() != before {
		t.Fatal("nothing loads after the notice")
	}
}

func TestLiveTick(t *testing.T) {
	src := newLiveSource(t)
	m := liveModel(t, src, 5)
	st := m.st
	st.data.Cockpit = &views.Cockpit{Features: []views.CockpitFeature{{ID: "A", Status: views.StatusDone}}}
	if st.liveTickCmd() != nil {
		t.Fatal("no tick without a feature working")
	}
	st.data.Cockpit.Features[0].Status = views.StatusVerifying
	if st.liveTickCmd() == nil {
		t.Fatal("a feature verifying needs the tick")
	}
	if st.liveTickCmd() != nil {
		t.Fatal("the tick is armed only once")
	}
	if st.onLive() == nil {
		t.Fatal("a tick with a feature working reloads and re-arms")
	}
	st.data.Cockpit.Features[0].Status = views.StatusDone
	if st.onLive() != nil {
		t.Fatal("no work, no more ticks")
	}

	// without Watch (demo, golden) there are no ticks at all
	st2 := newState(Options{Source: src, Width: 100})
	st2.data.Cockpit = &views.Cockpit{Features: []views.CockpitFeature{{ID: "A", Status: views.StatusWorking}}}
	if st2.liveTickCmd() != nil || st2.pollCmd() != nil {
		t.Fatal("no Watch, no ticks")
	}
}
