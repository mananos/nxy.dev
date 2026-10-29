package app

import (
	"context"
	"errors"
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
		if f.Repo == repo && f.State.Phase != domain.PhaseDone && (!found || f.ID > best.ID) {
			best, found = f, true
		}
	}
	return best, found, nil
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

func (fakeRepos) Key(dir string) string { return "repo:" + dir }

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
		if !strings.Contains(r.Deny, "usage: /nxy:feature") || r.Next != nil {
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
	ev.ProjectDir, ev.Cwd = "/q", "/q"
	if r := h.Handle(context.Background(), ev); r.Next != nil {
		t.Errorf("other repo got %+v", r)
	}
}

func TestSessionStartLegacyNotice(t *testing.T) {
	h, _ := flowHooks(true)
	r := h.Handle(context.Background(), HookEvent{Name: "SessionStart", ProjectDir: "/p"})
	if !strings.Contains(r.Notice, "nxy 1.x is still enabled") {
		t.Errorf("notice = %q", r.Notice)
	}
	h, _ = flowHooks(false)
	if r := h.Handle(context.Background(), HookEvent{Name: "SessionStart", ProjectDir: "/p"}); r.Notice != "" {
		t.Errorf("notice = %q", r.Notice)
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
