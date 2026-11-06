package domain

import "testing"

func TestFeatureHeld(t *testing.T) {
	old := Feature{Repo: "k"}
	if h := old.Held(); len(h) != 1 || h[0] != "k" {
		t.Errorf("old feature held %v", h)
	}
	if h := (Feature{Repo: "k", Home: "h"}).Held(); h != nil {
		t.Errorf("home without holds held %v", h)
	}
	if h := (Feature{Repo: "k", Home: "h", Holds: []string{"h"}}).Held(); len(h) != 1 || h[0] != "h" {
		t.Errorf("holds %v", h)
	}
	if h := (Feature{Holds: []string{"h"}, Paused: true}).Held(); h != nil {
		t.Errorf("paused held %v", h)
	}
	done := Feature{Holds: []string{"h"}}
	done.State.Phase = PhaseDone
	if h := done.Held(); h != nil {
		t.Errorf("done held %v", h)
	}
}

func TestFeatureRepoList(t *testing.T) {
	f := Feature{Repo: "k", ProjectDir: "/p", Base: "abc"}
	l := f.RepoList()
	if len(l) != 1 || l[0].Key != "k" || l[0].Root != "/p" || l[0].Base != "abc" || f.UsesWorktrees() {
		t.Errorf("implicit repo %+v", l)
	}
	f.Repos = []FeatureRepo{{Name: "a"}, {Name: "b"}}
	if _, ok := f.RepoByName("b"); !ok || f.UsesWorktrees() || !f.WantsWorktrees() {
		t.Error("two repos: wanted before the branch step, not yet in worktrees")
	}
	f.InWorktree = true
	if !f.UsesWorktrees() {
		t.Error("InWorktree")
	}
}
