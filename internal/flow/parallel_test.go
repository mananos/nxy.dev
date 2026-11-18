package flow

import (
	"errors"
	"reflect"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

// feed applies the events in order and returns the last state and actions.
func feed(t *testing.T, s st, evs ...Event) (st, []domain.Action) {
	t.Helper()
	var as []domain.Action
	for i, e := range evs {
		var err error
		if s, as, err = Next(s, e); err != nil {
			t.Fatalf("event %d (%s %d): %v", i, e.Kind, e.Batch, err)
		}
	}
	return s, as
}

func ev(k EventKind, n int) Event { return Event{Kind: k, Batch: n} }

func disp(n int, retry bool, all ...int) domain.Action {
	return domain.Action{Kind: domain.DispatchImplementer, Batch: n, Batches: all, Retry: retry}
}

func await(n int, all ...int) domain.Action {
	return domain.Action{Kind: domain.AwaitBatches, Batch: n, Batches: all}
}

// api(1) web(2 after 1) api(3 after 1) web(4 after 2)
func forkSteps() []domain.Step {
	return []domain.Step{spd(1, "api"), spd(2, "web", 1), spd(3, "api", 1), spd(4, "web", 2)}
}

func atBranch() st {
	return st{Phase: domain.PhaseBranch, Batches: 4, Steps: forkSteps()}
}

func TestPlanSavedKeepsSteps(t *testing.T) {
	got, _ := feed(t, st{Phase: domain.PhasePlanning, Steps: sps(sp(1, green, 0))},
		Event{Kind: PlanSaved, Batches: 4, Steps: forkSteps()})
	if !reflect.DeepEqual(got.Steps, forkSteps()) {
		t.Errorf("steps = %+v", got.Steps)
	}
	got.Phase = domain.PhasePlanning
	got, _ = feed(t, got, Event{Kind: PlanSaved, Batches: 2})
	if got.Steps != nil {
		t.Errorf("a plan without steps keeps old ones: %+v", got.Steps)
	}
}

func TestParallelByDepends(t *testing.T) {
	s, as := feed(t, atBranch(), Event{Kind: BranchReady})
	if !reflect.DeepEqual(as, one(disp(1, false))) {
		t.Fatalf("first: %+v", as)
	}
	s, as = feed(t, s, ev(Launched, 1))
	if !reflect.DeepEqual(as, one(await(1))) {
		t.Fatalf("launched 1: %+v", as)
	}
	s, as = feed(t, s, ev(BatchReturned, 1))
	if !reflect.DeepEqual(as, one(domain.Action{Kind: domain.VerifyBatch, Batch: 1})) {
		t.Fatalf("returned 1: %+v", as)
	}
	// 2 and 3 only wait for 1: both go out at once; 4 waits for 2.
	s, as = feed(t, s, ev(Green, 1))
	if !reflect.DeepEqual(as, one(disp(2, false, 2, 3))) {
		t.Fatalf("green 1: %+v", as)
	}
	if got := s.Running(); !reflect.DeepEqual(got, []int{2, 3}) {
		t.Fatalf("running = %v", got)
	}
	// Launching one re-asks for the one not launched yet.
	s, as = feed(t, s, ev(Launched, 2))
	if !reflect.DeepEqual(as, one(disp(3, false))) {
		t.Fatalf("launched 2: %+v", as)
	}
	s, as = feed(t, s, ev(Launched, 3))
	if !reflect.DeepEqual(as, one(await(2, 2, 3))) {
		t.Fatalf("launched 3: %+v", as)
	}
	// They return in any order; the other one is still awaited.
	s, as = feed(t, s, ev(BatchReturned, 3), ev(Green, 3))
	if !reflect.DeepEqual(as, one(await(2))) || s.Phase != domain.PhaseRunningBatch {
		t.Fatalf("green 3: %+v %s", as, s.Phase)
	}
	s, as = feed(t, s, ev(BatchReturned, 2), ev(Green, 2))
	if !reflect.DeepEqual(as, one(disp(4, false))) {
		t.Fatalf("green 2: %+v", as)
	}
	s, as = feed(t, s, ev(Launched, 4), ev(BatchReturned, 4), ev(Green, 4))
	if s.Phase != domain.PhaseAffected || !reflect.DeepEqual(as, acs(domain.RunAffected)) {
		t.Fatalf("end: %s %+v", s.Phase, as)
	}
}

func TestVerifyingStepsGiveNoActionsWhileOthersRun(t *testing.T) {
	s, _ := feed(t, atBranch(), Event{Kind: BranchReady}, ev(Launched, 1), ev(BatchReturned, 1), ev(Green, 1),
		ev(Launched, 2), ev(Launched, 3), ev(BatchReturned, 3), ev(BatchReturned, 2))
	s, as := feed(t, s, ev(Green, 3))
	if as != nil || s.Phase != domain.PhaseRunningBatch {
		t.Errorf("only 2 is verifying: %s %+v", s.Phase, as)
	}
}

// A retry and a new start dispatched together do not carry Retry (each prompt names its batch).
func TestRetryAndNewStartTogether(t *testing.T) {
	first := spd(1, "api")
	first.Status = domain.StepVerifying
	two := spd(2, "web")
	two.Status, two.Attempt = run, 1
	s := st{Phase: domain.PhaseRunningBatch, Batch: 1, Batches: 3, Steps: []domain.Step{first, two, spd(3, "api", 1)}}
	_, as := feed(t, s, ev(Green, 1))
	if !reflect.DeepEqual(as, one(disp(2, false, 2, 3))) {
		t.Fatalf("actions: %+v", as)
	}
}

func TestRedStopsOnlyDependents(t *testing.T) {
	s, _ := feed(t, atBranch(), Event{Kind: BranchReady}, ev(Launched, 1), ev(BatchReturned, 1), ev(Green, 1),
		ev(Launched, 2), ev(Launched, 3))
	// 2 goes red twice while 3 keeps running: no question yet.
	s, as := feed(t, s, ev(BatchReturned, 2), ev(Red, 2))
	if !reflect.DeepEqual(as, one(disp(2, true))) {
		t.Fatalf("first red: %+v", as)
	}
	s, as = feed(t, s, ev(Launched, 2), ev(BatchReturned, 2), ev(Red, 2))
	if !reflect.DeepEqual(as, one(await(3))) || s.Phase != domain.PhaseRunningBatch {
		t.Fatalf("second red: %s %+v", s.Phase, as)
	}
	// 4 depends on 2: it is not dispatched; the question comes when 3 is done.
	s, as = feed(t, s, ev(BatchReturned, 3), ev(Green, 3))
	if s.Phase != domain.PhaseBatchFailed || s.Batch != 2 || !reflect.DeepEqual(as, acs(domain.AskDecision, domain.Notify)) {
		t.Fatalf("question: %s batch %d %+v", s.Phase, s.Batch, as)
	}
	// Continue unlocks 4.
	c, as := feed(t, s, Event{Kind: Decided, Decision: DecisionContinue})
	if !reflect.DeepEqual(as, one(disp(4, false))) || c.Phase != domain.PhaseRunningBatch {
		t.Fatalf("continue: %s %+v", c.Phase, as)
	}
	// Retry goes back to 2 alone, without the failed attempt counted.
	r, as := feed(t, s, Event{Kind: Decided, Decision: DecisionRetry})
	if !reflect.DeepEqual(as, one(disp(2, false))) {
		t.Fatalf("retry: %+v", as)
	}
	if st2 := findStep(r.Steps, 2); st2.Attempt != 0 || st2.Status != run {
		t.Errorf("step 2 = %+v", st2)
	}
}

func TestSeveralFailedAskOneByOne(t *testing.T) {
	s := st{Phase: domain.PhaseRunningBatch, Batches: 3, Steps: sps(
		domain.Step{N: 1, Status: verif, Attempt: 1},
		domain.Step{N: 2, Status: verif, Attempt: 1},
		domain.Step{N: 3, After: []int{1}},
	)}
	s, _ = feed(t, s, ev(Red, 1))
	s, as := feed(t, s, ev(Red, 2))
	if s.Phase != domain.PhaseBatchFailed || s.Batch != 1 || !reflect.DeepEqual(as, acs(domain.AskDecision, domain.Notify)) {
		t.Fatalf("first question: %s %d %+v", s.Phase, s.Batch, as)
	}
	s, as = feed(t, s, Event{Kind: Decided, Decision: DecisionContinue})
	if s.Phase != domain.PhaseRunningBatch || !reflect.DeepEqual(as, one(disp(3, false))) {
		t.Fatalf("after continue: %s %+v", s.Phase, as)
	}
}

func TestRelaunchDispatchesTheRunningAgain(t *testing.T) {
	s, _ := feed(t, atBranch(), Event{Kind: BranchReady}, ev(Launched, 1), ev(BatchReturned, 1), ev(Green, 1),
		ev(Launched, 2), ev(Launched, 3))
	s, as := feed(t, s, Event{Kind: Relaunch})
	if !reflect.DeepEqual(as, one(disp(2, false, 2, 3))) {
		t.Fatalf("relaunch: %+v", as)
	}
	if got := s.Unlaunched(); !reflect.DeepEqual(got, []int{2, 3}) {
		t.Errorf("unlaunched = %v", got)
	}
}

func TestBatchEventsNeedTheRightStatus(t *testing.T) {
	s, _ := feed(t, atBranch(), Event{Kind: BranchReady})
	for _, e := range []Event{ev(BatchReturned, 2), ev(Green, 1), ev(Red, 1), ev(Launched, 3), ev(BatchReturned, 9)} {
		if _, _, err := Next(s, e); !errors.Is(err, ErrOutOfOrder) {
			t.Errorf("%s %d: err = %v", e.Kind, e.Batch, err)
		}
	}
}

// A state saved before parallel batches (no Steps) goes on as before, one batch after the other.
func TestOldStateWithoutSteps(t *testing.T) {
	s := st{Phase: domain.PhaseRunningBatch, Batch: 1, Batches: 2}
	s, as := feed(t, s, Event{Kind: Launched}, ev(BatchReturned, 0))
	if !reflect.DeepEqual(as, one(domain.Action{Kind: domain.VerifyBatch, Batch: 1})) {
		t.Fatalf("returned: %+v", as)
	}
	s, as = feed(t, s, Event{Kind: Green})
	if !reflect.DeepEqual(as, one(disp(2, false))) || s.Batch != 2 {
		t.Fatalf("green: %+v batch %d", as, s.Batch)
	}
	s, as = feed(t, s, Event{Kind: BatchReturned}, Event{Kind: Green})
	if s.Phase != domain.PhaseAffected || !reflect.DeepEqual(as, acs(domain.RunAffected)) {
		t.Fatalf("end: %s %+v", s.Phase, as)
	}
}
