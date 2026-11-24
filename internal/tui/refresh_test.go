package tui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mananos/nxy.dev/v2/internal/views"
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
	st := newState(Options{Source: src, InRepo: true, Width: 100, Plain: true, Watch: fakeWatch, Schema: schema})
	st.onLoad(cheapMsg{r: st.ld.cheap(st.cheapOpts())})
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

// slowStats is a source whose Stats waits until the test lets it go.
type slowStats struct {
	views.Source
	in   chan struct{}
	gate chan struct{}
}

func (s *slowStats) Stats(ctx context.Context, id string) (views.Stats, error) {
	s.in <- struct{}{}
	<-s.gate
	return s.Source.Stats(ctx, id)
}

func TestSpendOutsideCheapLock(t *testing.T) {
	src := &slowStats{Source: testSource(t), in: make(chan struct{}, 4), gate: make(chan struct{})}
	st := newState(Options{Source: src, InRepo: true, Width: 100, Plain: true})
	cmd := st.spendCmd()
	if cmd == nil || !st.spendBusy {
		t.Fatal("spendCmd must start a spend load")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case <-src.in:
	case <-time.After(2 * time.Second):
		t.Fatal("the spend load never reached Stats")
	}

	others := make(chan struct{})
	go func() {
		defer close(others)
		st.onLoad(cheapMsg{r: st.ld.cheap(st.cheapOpts())})
		id := st.data.Cockpit.Features[0].ID
		st.ld.plan(id)
		st.ld.agents(id)
		st.ld.engineer(id)
		st.ld.file(id, "repo/x.go", nil)
	}()
	select {
	case <-others:
	case <-time.After(2 * time.Second):
		close(src.gate)
		t.Fatal("cheap, plan, agents, engineer and file wait for Stats")
	}
	close(src.gate)
	msg := <-done
	if _, ok := msg.(spendMsg); !ok {
		t.Fatalf("spend load answered %T", msg)
	}
	st.onLoad(msg)
	if st.spendBusy || st.data.Stats == nil {
		t.Fatalf("spendMsg must free the load and bring Stats: busy=%v", st.spendBusy)
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
	st := newState(Options{Source: src, InRepo: true, Width: 100, Plain: true, Watch: fakeWatch, Schema: 5, Ver: 1, HaveVer: true})
	st.onLoad(cheapMsg{r: st.ld.cheap(st.cheapOpts())})
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
	st.onLoad(cheapMsg{r: st.ld.cheap(st.cheapOpts())})

	order = inicioOrder(st.data.Cockpit)
	if got := order[min(st.cursor(r), len(order)-1)].ID; got != want {
		t.Fatalf("cursor on %s, want %s", got, want)
	}
	if order[0].ID != "NEWX" {
		t.Fatalf("the new feature should be first, got %s", order[0].ID)
	}
}

func TestSchemaNewerCallsOnStale(t *testing.T) {
	src := newLiveSource(t)
	m := liveModel(t, src, 5)
	calls := 0
	m.st.onStale = func() { calls++ }
	m.Update(pollMsg{ver: 1, schema: 5})
	if calls != 0 {
		t.Fatal("same schema must not call OnStale")
	}
	m.Update(pollMsg{ver: 2, schema: 6})
	m.Update(pollMsg{ver: 3, schema: 6})
	if calls != 1 {
		t.Fatalf("OnStale called %d times, want 1", calls)
	}
}

// spendSource counts the spend loads (one Stats call each) on top of the live source.
type spendSource struct {
	*liveSource
	spends atomic.Int32
}

func (s *spendSource) Stats(ctx context.Context, id string) (views.Stats, error) {
	s.spends.Add(1)
	return s.liveSource.Stats(ctx, id)
}

func TestSpendRhythm(t *testing.T) {
	src := &spendSource{liveSource: newLiveSource(t)}
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	st := newState(Options{Source: src, InRepo: true, Width: 100, Plain: true, Watch: fakeWatch,
		Schema: 5, Ver: 1, HaveVer: true, Now: func() time.Time { return clock }})
	st.onLoad(cheapMsg{r: st.ld.cheap(st.cheapOpts())})
	m := &model{st: st}
	// the first spend load, as Init does
	feed(m, collect(st.spendCmd()))
	base := src.spends.Load()
	key := func(c rune, text string) tea.Cmd {
		_, cmd := m.Update(tea.KeyPressMsg{Code: c, Text: text})
		return cmd
	}
	spends := func() int32 { return src.spends.Load() - base }

	// on Inicio a db change does not reload the spend
	_, cmd := m.Update(pollMsg{ver: 2, schema: 1})
	feed(m, collect(cmd))
	if spends() != 0 {
		t.Fatalf("spend loads %d on Inicio after a change, want 0", spends())
	}
	// 30 s later the next poll does
	clock = clock.Add(31 * time.Second)
	_, cmd = m.Update(pollMsg{ver: 2, schema: 1})
	feed(m, collect(cmd))
	if spends() != 1 {
		t.Fatalf("spend loads %d after 30 s, want 1", spends())
	}

	// with Gasto on top every change reloads it
	st.stack = []route{{screen: "inicio"}, {screen: "gasto"}}
	_, cmd = m.Update(pollMsg{ver: 3, schema: 1})
	feed(m, collect(cmd))
	if spends() != 2 {
		t.Fatalf("spend loads %d with Gasto on top, want 2", spends())
	}

	// a change on Inicio, then entering Gasto: at once
	st.stack = []route{{screen: "inicio"}}
	_, cmd = m.Update(pollMsg{ver: 4, schema: 1})
	feed(m, collect(cmd))
	st.stack = []route{{screen: "inicio"}, {screen: "gasto"}}
	feed(m, collect(key(tea.KeyDown, "")))
	if spends() != 3 {
		t.Fatalf("spend loads %d after entering Gasto, want 3", spends())
	}

	// r reloads at once
	feed(m, collect(key('r', "r")))
	if spends() != 4 {
		t.Fatalf("spend loads %d after r, want 4", spends())
	}

	// picking a period reloads the spend and not the cheap views
	st.stack = []route{{screen: "inicio"}, {screen: "gasto"}, {screen: "tendencia"}}
	cheap := src.loads.Load()
	feed(m, collect(key(tea.KeyLeft, "")))
	if spends() != 5 {
		t.Fatalf("spend loads %d after picking a period, want 5", spends())
	}
	if src.loads.Load() != cheap {
		t.Fatal("picking a period must not reload the cheap views")
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
	if out := m.st.frame(false); !strings.Contains(out, "nxy se actualizó") {
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
