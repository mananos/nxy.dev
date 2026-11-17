package app

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

// memStore is an in-memory Store whose Transition keeps the real contract: fn runs against a
// copy and the result is stored only if the version did not change meanwhile.
type memStore struct {
	feats   map[string]domain.Feature
	version map[string]int
	n       int
}

func newMemStore() *memStore {
	return &memStore{feats: map[string]domain.Feature{}, version: map[string]int{}}
}

func (m *memStore) CreateFeature(_ context.Context, f domain.Feature) (string, error) {
	for _, held := range f.Held() {
		for _, g := range m.feats {
			if slices.Contains(g.Held(), held) {
				return "", ErrActiveFeature
			}
		}
	}
	m.n++
	f.ID = "F" + strconv.Itoa(m.n)
	m.feats[f.ID] = f
	m.version[f.ID] = 1
	return f.ID, nil
}

func (m *memStore) Feature(_ context.Context, id string) (domain.Feature, error) {
	f, ok := m.feats[id]
	if !ok {
		return domain.Feature{}, errors.New("not found")
	}
	return f, nil
}

func (m *memStore) ActiveFeature(_ context.Context, repo string) (domain.Feature, bool, error) {
	var best domain.Feature
	found := false
	for _, f := range m.feats {
		if slices.Contains(f.Held(), repo) && (!found || f.ID > best.ID) {
			best, found = f, true
		}
	}
	return best, found, nil
}

func (m *memStore) LatestFeature(_ context.Context, repo string) (domain.Feature, bool, error) {
	var best domain.Feature
	found := false
	for _, f := range m.feats {
		if f.Repo == repo && (!found || f.ID > best.ID) {
			best, found = f, true
		}
	}
	return best, found, nil
}

func (m *memStore) Features(_ context.Context, flt FeatureFilter) ([]domain.Feature, error) {
	var out []domain.Feature
	for _, f := range m.feats {
		if flt.Home != "" && f.Home != flt.Home && (f.Home != "" || f.Repo != flt.Home) {
			continue
		}
		if flt.Active || flt.Paused {
			if f.State.Phase == domain.PhaseDone || f.Paused != flt.Paused {
				continue
			}
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func (m *memStore) Transition(_ context.Context, id string, fn func(domain.Feature) (domain.Feature, error)) error {
	for range 3 {
		f, ok := m.feats[id]
		if !ok {
			return errors.New("not found")
		}
		v := m.version[id]
		nf, err := fn(f)
		if err != nil {
			return err
		}
		if m.version[id] != v {
			continue
		}
		nf.ID = id
		m.feats[id] = nf
		m.version[id] = v + 1
		return nil
	}
	return errors.New("conflict")
}

type fakeRepos struct{}

func (fakeRepos) Key(dir string) string      { return "repo:" + dir }
func (fakeRepos) Checkout(dir string) string { return "repo:" + dir }
func (fakeRepos) Root(dir string) (string, bool) {
	return dir, true
}

type fakeLegacy bool

func (l fakeLegacy) Active(string) bool { return bool(l) }

func flowHooks(legacy bool) (Hooks, *memStore) {
	st := newMemStore()
	return Hooks{Store: st, Repos: fakeRepos{}, Legacy: fakeLegacy(legacy)}, st
}

func prompt(p string) HookEvent {
	return HookEvent{Name: "UserPromptSubmit", Prompt: p, ProjectDir: "/p", Cwd: "/p"}
}

func TestStartFeatureCapturesRequestVerbatim(t *testing.T) {
	h, st := flowHooks(false)
	req := "add  a flag\n\n  with   spaces\n- and a list \n"
	r := h.Handle(context.Background(), prompt("/nxy:feature "+req))
	if r.Deny != "" || r.Next == nil {
		t.Fatalf("response = %+v", r)
	}
	f := r.Next.Feature
	if f.Request != req {
		t.Errorf("request = %q, want %q", f.Request, req)
	}
	if f.State.Phase != domain.PhasePlanning || f.Next.Kind != domain.DispatchPlanner {
		t.Errorf("state = %+v next = %+v", f.State, f.Next)
	}
	if f.Repo != "repo:/p" || f.ProjectDir != "/p" || f.ID == "" {
		t.Errorf("feature = %+v", f)
	}
	if got := st.feats[f.ID]; got.Request != req {
		t.Errorf("stored request = %q", got.Request)
	}
}

func TestStartFeatureNewlineSeparator(t *testing.T) {
	h, _ := flowHooks(false)
	r := h.Handle(context.Background(), prompt("/nxy:feature\nline one\nline two"))
	if r.Next == nil || r.Next.Feature.Request != "line one\nline two" {
		t.Fatalf("response = %+v", r)
	}
}

func TestStartFeatureEmptyIsDenied(t *testing.T) {
	for _, p := range []string{"/nxy:feature", "/nxy:feature ", "/nxy:feature  \n "} {
		h, st := flowHooks(false)
		r := h.Handle(context.Background(), prompt(p))
		if !strings.Contains(r.Deny, "uso: /nxy:feature") || r.Next != nil {
			t.Errorf("%q: response = %+v", p, r)
		}
		if len(st.feats) != 0 {
			t.Errorf("%q: created a feature", p)
		}
	}
}

func TestStartFeatureSecondIsRejected(t *testing.T) {
	h, st := flowHooks(false)
	first := h.Handle(context.Background(), prompt("/nxy:feature one"))
	r := h.Handle(context.Background(), prompt("/nxy:feature two"))
	id := first.Next.Feature.ID
	for _, want := range []string{id, "in progress", "Trabajando", "Planificar", "`nxy feature stop`"} {
		if !strings.Contains(r.Deny, want) {
			t.Errorf("deny %q lacks %q", r.Deny, want)
		}
	}
	if len(st.feats) != 1 {
		t.Errorf("features = %d, want 1", len(st.feats))
	}
	// another repo is independent
	other := prompt("/nxy:feature three")
	other.ProjectDir = "/q"
	if r := h.Handle(context.Background(), other); r.Next == nil {
		t.Errorf("other repo response = %+v", r)
	}
}

// racingStore hides the active feature from the first ActiveFeature call and refuses CreateFeature,
// like a second prompt that lost the unique index race.
type racingStore struct {
	*memStore
	hidden bool
}

func (r *racingStore) ActiveFeature(ctx context.Context, repo string) (domain.Feature, bool, error) {
	if !r.hidden {
		r.hidden = true
		return domain.Feature{}, false, nil
	}
	return r.memStore.ActiveFeature(ctx, repo)
}

func (r *racingStore) CreateFeature(context.Context, domain.Feature) (string, error) {
	return "", ErrActiveFeature
}

func TestStartFeatureRaceIsRejected(t *testing.T) {
	h, st := flowHooks(false)
	first := h.Handle(context.Background(), prompt("/nxy:feature one"))
	h.Store = &racingStore{memStore: st}
	r := h.Handle(context.Background(), prompt("/nxy:feature two"))
	for _, want := range []string{first.Next.Feature.ID, "in progress", "`nxy feature stop`"} {
		if !strings.Contains(r.Deny, want) {
			t.Errorf("deny %q lacks %q", r.Deny, want)
		}
	}
	if len(st.feats) != 1 {
		t.Errorf("features = %d, want 1", len(st.feats))
	}
}

func TestOtherPromptsCreateNothing(t *testing.T) {
	for _, p := range []string{"hello", "/nxy:featurex y", "/nxy:status", " /nxy:feature a", ""} {
		h, st := flowHooks(false)
		if r := h.Handle(context.Background(), prompt(p)); r != (HookResponse{}) {
			t.Errorf("%q: response = %+v", p, r)
		}
		if len(st.feats) != 0 {
			t.Errorf("%q: created a feature", p)
		}
	}
}

func TestSessionStartReinjectsStep(t *testing.T) {
	h, _ := flowHooks(false)
	ev := HookEvent{Name: "SessionStart", ProjectDir: "/p", Cwd: "/p"}
	if r := h.Handle(context.Background(), ev); r != (HookResponse{}) {
		t.Fatalf("without feature: %+v", r)
	}
	started := h.Handle(context.Background(), prompt("/nxy:feature go"))
	r := h.Handle(context.Background(), ev)
	if r.Next == nil || r.Next.Feature.ID != started.Next.Feature.ID || r.Next.Feature.Next.Kind != domain.DispatchPlanner {
		t.Errorf("response = %+v", r)
	}
	if want := "nxy: feature " + started.Next.Feature.ID + " pendiente (" + r.Next.Feature.Next.Label() + ") — escribí cualquier cosa para seguir"; r.Notice != want || r.Next.Feature.Next.Label() == "" {
		t.Errorf("notice = %q, want %q", r.Notice, want)
	}
	ev.ProjectDir, ev.Cwd = "/q", "/q"
	if r := h.Handle(context.Background(), ev); r.Next != nil {
		t.Errorf("other repo got %+v", r)
	}
}

func TestSessionStartLegacyNotice(t *testing.T) {
	h, _ := flowHooks(true)
	r := h.Handle(context.Background(), HookEvent{Name: "SessionStart", ProjectDir: "/p"})
	if !strings.Contains(r.Notice, "la 1.x de nxy sigue activada") {
		t.Errorf("notice = %q", r.Notice)
	}
	h, _ = flowHooks(false)
	if r := h.Handle(context.Background(), HookEvent{Name: "SessionStart", ProjectDir: "/p"}); r.Notice != "" {
		t.Errorf("notice = %q", r.Notice)
	}
}

func TestSessionStartBothNotices(t *testing.T) {
	h, _ := flowHooks(true)
	h.Handle(context.Background(), prompt("/nxy:feature go"))
	r := h.Handle(context.Background(), HookEvent{Name: "SessionStart", ProjectDir: "/p", Cwd: "/p"})
	lines := strings.Split(r.Notice, "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "nxy: feature ") || lines[1] != legacyNotice {
		t.Errorf("notice = %q", r.Notice)
	}
}

func TestSessionStartBackgroundPlannerWaits(t *testing.T) {
	h, st := flowHooks(false)
	started := h.Handle(context.Background(), prompt("/nxy:feature go"))
	id := started.Next.Feature.ID
	f := st.feats[id]
	f.Background = rolePlanner
	st.feats[id] = f
	r := h.Handle(context.Background(), HookEvent{Name: "SessionStart", Source: "clear", ProjectDir: "/p", Cwd: "/p"})
	if r.Next != nil || !strings.HasPrefix(r.Context, backgroundWait(id, rolePlanner)) || !strings.Contains(r.Context, "para retomar") || !strings.Contains(r.Notice, "planner sigue corriendo") || strings.Contains(r.Notice, "pendiente") {
		t.Errorf("response = %+v", r)
	}
	r = h.Handle(context.Background(), HookEvent{Name: "SessionStart", Source: "resume", ProjectDir: "/p", Cwd: "/p"})
	if r.Next != nil || !strings.HasPrefix(r.Context, backgroundWait(id, rolePlanner)) || !strings.Contains(r.Context, "para retomar") || r.Notice != "" {
		t.Errorf("resume = %+v", r)
	}
}

func TestSessionStartStartupClearsBackground(t *testing.T) {
	for _, bg := range []string{rolePlanner, backgroundEnded} {
		h, st := flowHooks(false)
		id := h.Handle(context.Background(), prompt("/nxy:feature go")).Next.Feature.ID
		f := st.feats[id]
		f.Background = bg
		st.feats[id] = f
		r := h.Handle(context.Background(), HookEvent{Name: "SessionStart", Source: "startup", ProjectDir: "/p", Cwd: "/p"})
		if r.Next == nil || strings.Contains(r.Context, "still running") || !strings.Contains(r.Context, "para retomar") || !strings.Contains(r.Notice, "pendiente") || st.feats[id].Background != "" {
			t.Errorf("bg=%q response = %+v stored=%q", bg, r, st.feats[id].Background)
		}
	}
}

func TestSessionStartResumeNoPendingNotice(t *testing.T) {
	h, _ := flowHooks(false)
	h.Handle(context.Background(), prompt("/nxy:feature go"))
	r := h.Handle(context.Background(), HookEvent{Name: "SessionStart", Source: "resume", ProjectDir: "/p", Cwd: "/p"})
	if r.Next == nil || r.Notice != "" {
		t.Errorf("response = %+v", r)
	}
}

func TestMutateFalseStoresNothing(t *testing.T) {
	h, st := flowHooks(false)
	id := h.Handle(context.Background(), prompt("/nxy:feature go")).Next.Feature.ID
	v := st.version[id]
	_, applied, err := h.mutate(context.Background(), id, func(g *domain.Feature) bool { g.Background = "x"; return false })
	if err != nil || applied || st.version[id] != v || st.feats[id].Background != "" {
		t.Errorf("applied=%v err=%v version %d->%d bg=%q", applied, err, v, st.version[id], st.feats[id].Background)
	}
}

func TestNilStoreSaysNothing(t *testing.T) {
	h := Hooks{}
	for _, ev := range []HookEvent{prompt("/nxy:feature x"), {Name: "SessionStart", ProjectDir: "/p"}} {
		if r := h.Handle(context.Background(), ev); r != (HookResponse{}) {
			t.Errorf("%s: %+v", ev.Name, r)
		}
	}
}
