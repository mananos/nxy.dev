package app

import (
	"context"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/plan"
)

func savedRisk(t *testing.T, at string) domain.Batch {
	t.Helper()
	h, st := flowHooks(false)
	p := validPlan()
	p.Batches[0].Change = []domain.Change{{At: at, What: "x"}}
	f := approvalFeature(t, h, p)
	return st.feats[f.ID].Plan.Batches[0]
}

func TestSavePlanRaisesRiskAndShowsWhy(t *testing.T) {
	bt := savedRisk(t, "internal/auth/login.go:12")
	if bt.Risk != domain.RiskHigh || !strings.Contains(bt.RiskWhy, "security") {
		t.Fatalf("batch = %+v", bt)
	}
	cp := plan.Checkpoint(domain.Plan{Batches: []domain.Batch{bt}})
	if !strings.Contains(cp, "Riesgo: alto (subido por nxy: lente security: internal/auth/login.go)") {
		t.Errorf("checkpoint = %q", cp)
	}
	bt = savedRisk(t, "internal/web/UserController.java (new)")
	if bt.Risk != domain.RiskMedium {
		t.Errorf("controller risk = %+v", bt)
	}
	bt = savedRisk(t, "cmd/main.go:3")
	if bt.Risk != domain.RiskLow || bt.RiskWhy != "" {
		t.Errorf("plain risk = %+v", bt)
	}
}

func TestPrepareReviewDepth(t *testing.T) {
	h, _ := flowHooks(false)
	h.Changes = fakeChanges{changed: []string{"cmd/main.go"}}
	low := domain.Feature{ProjectDir: "/p", Plan: &domain.Plan{Batches: []domain.Batch{{Risk: domain.RiskLow}}}}
	rv := h.prepareReview(context.Background(), low)
	if !rv.Quick || len(rv.Lenses) != 1 || rv.Lenses[0].Name != "base" {
		t.Fatalf("low = %+v", rv)
	}
	h.Changes = fakeChanges{changed: []string{"cmd/main.go", "internal/api/routes.go"}}
	rv = h.prepareReview(context.Background(), low)
	names := []string{}
	for _, l := range rv.Lenses {
		names = append(names, l.Name)
	}
	if rv.Quick || rv.Risk != domain.RiskMedium || strings.Join(names, ",") != "api,base" {
		t.Fatalf("medium = %+v (%v)", rv, names)
	}
}

func TestReviewingWithoutReviewIsPreparedLazily(t *testing.T) {
	for _, name := range []string{"SessionStart", "Stop"} {
		h, st := flowHooks(false)
		h.Changes = fakeChanges{changed: []string{"cmd/main.go"}}
		id, _ := st.CreateFeature(context.Background(), domain.Feature{
			Repo: "repo:/p", ProjectDir: "/p", Undelivered: name == "Stop",
			State: domain.State{Phase: domain.PhaseReviewing}, Next: domain.Action{Kind: domain.DispatchReviewer},
		})
		r := h.Handle(context.Background(), HookEvent{Name: name, Source: "resume", ProjectDir: "/p", Cwd: "/p"})
		if r.Next == nil || r.Next.Feature.Review == nil || len(r.Next.Feature.Review.Lenses) == 0 {
			t.Fatalf("%s: response = %+v", name, r)
		}
		if got := st.feats[id].Review; got == nil || len(got.Lenses) == 0 {
			t.Errorf("%s: stored review = %+v", name, got)
		}
	}
}

type fakeReviewFiles struct{ discarded []string }

func (*fakeReviewFiles) Write(string, string) (string, error) { return "", nil }
func (r *fakeReviewFiles) Discard(id string) error {
	r.discarded = append(r.discarded, id)
	return nil
}

func TestStopFeatureDiscardsReviewFile(t *testing.T) {
	h, st := flowHooks(false)
	rf := &fakeReviewFiles{}
	h.ReviewFiles = rf
	id := seed(t, h, st, t.TempDir(), approvalState, approvalNext)
	f, _ := st.feats[id], 0
	if _, _, err := h.StopFeature(context.Background(), f.ProjectDir); err != nil {
		t.Fatal(err)
	}
	if len(rf.discarded) != 1 || rf.discarded[0] != id {
		t.Errorf("discarded = %v", rf.discarded)
	}
}

func TestDoneFeatureIsDeliveredOnce(t *testing.T) {
	h, st := flowHooks(false)
	id, _ := st.CreateFeature(context.Background(), domain.Feature{
		Repo: "repo:/p", ProjectDir: "/p", Undelivered: true,
		State: domain.State{Phase: domain.PhaseDone, Outcome: domain.OutcomeSuccess},
	})
	ev := HookEvent{Name: "Stop", ProjectDir: "/p", Cwd: "/p"}
	if r := h.Handle(context.Background(), ev); r.Next == nil || r.Next.Feature.ID != id {
		t.Fatalf("first = %+v", r)
	}
	if r := h.Handle(context.Background(), ev); r.Next != nil {
		t.Errorf("second = %+v", r)
	}
}
