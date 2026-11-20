package app

import (
	"context"
	"slices"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

// coRepos gives every folder the same repo key but its own checkout, like worktrees of one repo.
type coRepos struct{ fakeRepos }

func (coRepos) Key(string) string          { return "repo:shared" }
func (coRepos) Checkout(dir string) string { return "co:" + dir }

func sessionHooks() (Hooks, *memStore, *fakeSessions) {
	h, st := flowHooks(false)
	ss := newFakeSessions()
	h.Sessions = ss
	return h, st, ss
}

func promptFrom(session, dir, text string) HookEvent {
	ev := prompt(text)
	ev.SessionID, ev.ProjectDir, ev.Cwd = session, dir, dir
	return ev
}

func TestStartFeatureSecondOfSameSessionIsRejected(t *testing.T) {
	h, st, ss := sessionHooks()
	ctx := context.Background()
	first := h.Handle(ctx, promptFrom("s1", "/p", "/nxy:feature one"))
	if first.Next == nil {
		t.Fatalf("first = %+v", first)
	}
	if got := ss.rows["s1"].FeatureID; got != first.Next.Feature.ID {
		t.Errorf("session link = %q, want %q", got, first.Next.Feature.ID)
	}
	// even from another folder: the session already has a feature
	if r := h.Handle(ctx, promptFrom("s1", "/q", "/nxy:feature two")); r.Deny == "" {
		t.Errorf("second of the same session = %+v", r)
	}
	if len(st.feats) != 1 {
		t.Errorf("features = %d, want 1", len(st.feats))
	}
}

func TestStartFeatureOfAnotherSessionInTheSameFolder(t *testing.T) {
	h, st, ss := sessionHooks()
	ctx := context.Background()
	first := h.Handle(ctx, promptFrom("s1", "/p", "/nxy:feature one"))
	second := h.Handle(ctx, promptFrom("s2", "/p", "/nxy:feature two"))
	if second.Next == nil || second.Deny != "" {
		t.Fatalf("second = %+v", second)
	}
	g := st.feats[second.Next.Feature.ID]
	if len(g.Holds) != 0 || g.Home != "repo:/p" {
		t.Errorf("second feature home = %q holds = %v, want home and no holds", g.Home, g.Holds)
	}
	if ss.rows["s2"].FeatureID != g.ID {
		t.Errorf("session s2 link = %q", ss.rows["s2"].FeatureID)
	}
	// each session finds its own feature
	for sid, want := range map[string]string{"s1": first.Next.Feature.ID, "s2": g.ID} {
		f, ok := h.featureFor(ctx, HookEvent{SessionID: sid, ProjectDir: "/p", Cwd: "/p"})
		if !ok || f.ID != want {
			t.Errorf("%s finds %q (%v), want %q", sid, f.ID, ok, want)
		}
	}
	// a session that is unknown cannot tell it is another one: the busy folder refuses
	if r := h.Handle(ctx, promptFrom("", "/p", "/nxy:feature three")); r.Deny == "" {
		t.Errorf("no session = %+v", r)
	}
}

func TestStartFeatureReopenedSessionIsRejected(t *testing.T) {
	h, st, ss := sessionHooks()
	ctx := context.Background()
	if first := h.Handle(ctx, promptFrom("s1", "/p", "/nxy:feature one")); first.Next == nil {
		t.Fatalf("first = %+v", first)
	}
	// the old session ended and the user reopened Claude Code: a new session id, same folder
	_ = ss.EndSession(ctx, "s1", "other", 1)
	if r := h.Handle(ctx, promptFrom("s2", "/p", "/nxy:feature two")); r.Deny == "" {
		t.Errorf("reopened session = %+v", r)
	}
	if len(st.feats) != 1 {
		t.Errorf("features = %d, want 1", len(st.feats))
	}
}

func TestFeatureForFollowsTheSessionLink(t *testing.T) {
	h, st, ss := sessionHooks()
	ctx := context.Background()
	id, _ := st.CreateFeature(ctx, domain.Feature{
		Repo: "repo:/p", Home: "repo:/p", Holds: []string{"repo:/wt"}, ProjectDir: "/p", State: runningState, Next: runningNext,
	})
	ss.rows["s1"] = SessionState{ID: "s1", FeatureID: id}
	// a subagent runs with the main session's id, in a folder that holds nothing
	if f, ok := h.featureFor(ctx, HookEvent{SessionID: "s1", AgentID: "a1", ProjectDir: "/elsewhere"}); !ok || f.ID != id {
		t.Errorf("by link = %q %v", f.ID, ok)
	}
	// without the link it goes by the checkout it holds, then by home
	if f, ok := h.featureFor(ctx, HookEvent{ProjectDir: "/wt"}); !ok || f.ID != id {
		t.Errorf("by hold = %q %v", f.ID, ok)
	}
	if f, ok := h.featureFor(ctx, HookEvent{ProjectDir: "/p"}); !ok || f.ID != id {
		t.Errorf("by home = %q %v", f.ID, ok)
	}
	// paused: not found
	_ = h.Store.Transition(ctx, id, func(f domain.Feature) (domain.Feature, error) { f.Paused = true; return f, nil })
	if _, ok := h.featureFor(ctx, HookEvent{SessionID: "s1", ProjectDir: "/p"}); ok {
		t.Error("a paused feature was found")
	}
}

func TestFeatureForSeparatesWorktreesOfOneRepoAndAdoptsOldOnes(t *testing.T) {
	h, st := flowHooks(false)
	h.Repos = coRepos{}
	ctx := context.Background()
	// before the checkouts: held by the repo key, no home
	old, _ := st.CreateFeature(ctx, domain.Feature{Repo: "repo:shared", ProjectDir: "/main", State: runningState, Next: runningNext})
	// after: holds its own worktree
	wt, err := st.CreateFeature(ctx, domain.Feature{
		Repo: "repo:shared", ProjectDir: "/main", Home: "co:/other", Holds: []string{"co:/wt"}, State: runningState, Next: runningNext,
	})
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := h.featureFor(ctx, HookEvent{ProjectDir: "/wt"}); !ok || f.ID != wt {
		t.Errorf("worktree finds %q %v, want %q", f.ID, ok, wt)
	}
	f, ok := h.featureFor(ctx, HookEvent{ProjectDir: "/main"})
	if !ok || f.ID != old {
		t.Fatalf("main finds %q %v, want %q", f.ID, ok, old)
	}
	if f.Home != "co:/main" || !slices.Equal(f.Holds, []string{"co:/main"}) {
		t.Errorf("adopted home = %q holds = %v", f.Home, f.Holds)
	}
	// adopted: now held by the checkout, and the repo key is free
	if g, found, _ := st.ActiveFeature(ctx, "co:/main"); !found || g.ID != old {
		t.Errorf("checkout hold = %q %v", g.ID, found)
	}
	if _, found, _ := st.ActiveFeature(ctx, "repo:shared"); found {
		t.Error("the repo key is still held")
	}
}

func TestSessionStartAfterClearKeepsTheFeature(t *testing.T) {
	h, st, ss := sessionHooks()
	ctx := context.Background()
	id, _ := st.CreateFeature(ctx, domain.Feature{
		Repo: "repo:/p", Home: "repo:/p", Holds: []string{"repo:/wt"}, ProjectDir: "/p", State: runningState, Next: runningNext,
	})
	ss.rows["old"] = SessionState{ID: "old", FeatureID: id, EndReason: "clear", EndedAt: 1}
	ss.recent = &SessionState{ID: "old", FeatureID: id}
	ev := HookEvent{Name: "SessionStart", Source: "clear", SessionID: "new", ProjectDir: "/other", Cwd: "/other"}
	r := h.Handle(ctx, ev)
	if r.Next == nil || r.Next.Feature.ID != id {
		t.Fatalf("response = %+v", r)
	}
	if ss.rows["new"].FeatureID != id {
		t.Errorf("new session link = %q", ss.rows["new"].FeatureID)
	}
}
