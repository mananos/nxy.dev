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

func TestProofOfAndLanes(t *testing.T) {
	f := Feature{Proof: &Proof{Batch: 2, Edits: 5}}
	if p := f.ProofOf(2); p.Edits != 5 {
		t.Errorf("legacy proof %+v", p)
	}
	if p := f.ProofOf(3); p.Batch != 3 || p.Edits != 0 {
		t.Errorf("empty proof %+v", p)
	}
	f.SetProof(Proof{Batch: 3, Edits: 1})
	f.SetProof(Proof{Batch: 3, Edits: 2})
	if f.Proof != nil || len(f.Proofs) != 1 || f.ProofOf(3).Edits != 2 || f.ProofOf(2).Edits != 0 {
		t.Errorf("after SetProof %+v", f)
	}
	f.SetLane(BatchLane{Batch: 2, ToolUse: "a"})
	f.SetLane(BatchLane{Batch: 2, ToolUse: "b", Background: "running"})
	if l, ok := f.LaneOf(2); !ok || l.ToolUse != "b" || len(f.Lanes) != 1 {
		t.Errorf("lane %+v", f.Lanes)
	}
	if _, ok := f.LaneOf(9); ok {
		t.Error("lane 9")
	}
}

func TestStateRunningAndUnlaunched(t *testing.T) {
	s := State{Steps: []Step{{N: 1, Status: StepGreen}, {N: 2, Status: StepRunning, Launched: true}, {N: 3, Status: StepRunning}, {N: 4}}}
	if r := s.Running(); len(r) != 2 || r[0] != 2 || r[1] != 3 {
		t.Errorf("running %v", r)
	}
	if u := s.Unlaunched(); len(u) != 1 || u[0] != 3 {
		t.Errorf("unlaunched %v", u)
	}
}

func TestActionLabelsBatches(t *testing.T) {
	cases := []struct {
		a    Action
		want string
	}{
		{Action{Kind: DispatchImplementer, Batch: 2}, "Lote 2"},
		{Action{Kind: DispatchImplementer, Batch: 2, Batches: []int{2, 3}}, "Lotes 2 y 3"},
		{Action{Kind: DispatchImplementer, Batch: 2, Batches: []int{2, 3, 4}}, "Lotes 2, 3 y 4"},
		{Action{Kind: AwaitBatches, Batch: 2, Batches: []int{2, 3}}, "Esperar lotes 2 y 3"},
		{Action{Kind: AwaitBatches}, "Esperar lotes"},
	}
	for _, c := range cases {
		if got := c.a.Label(); got != c.want {
			t.Errorf("%+v label = %q, want %q", c.a, got, c.want)
		}
	}
	if !(Action{Kind: AwaitBatches}).MainThread() {
		t.Error("AwaitBatches is main thread")
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
