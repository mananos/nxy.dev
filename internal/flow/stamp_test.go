package flow

import (
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

func at(k EventKind, n int, t int64) Event { return Event{Kind: k, Batch: n, At: t} }

func TestStampTimesOfSteps(t *testing.T) {
	s, _ := feed(t, atBranch(), at(BranchReady, 0, 100), at(Launched, 1, 101), at(BatchReturned, 1, 110))
	if st := findStep(s.Steps, 1); st.StartedAt != 100 || st.EndedAt != 0 {
		t.Fatalf("running: %+v", st)
	}
	// Green ends 1 and starts 2 and 3 together.
	s, _ = feed(t, s, at(Green, 1, 200))
	if st := findStep(s.Steps, 1); st.StartedAt != 100 || st.EndedAt != 200 {
		t.Fatalf("green: %+v", st)
	}
	for _, n := range []int{2, 3} {
		if st := findStep(s.Steps, n); st.StartedAt != 200 || st.EndedAt != 0 {
			t.Fatalf("parallel start %d: %+v", n, st)
		}
	}
	// Red once retries: the start of the first attempt stays and there is no end.
	s, _ = feed(t, s, at(Launched, 2, 201), at(BatchReturned, 2, 210), at(Red, 2, 220))
	if st := findStep(s.Steps, 2); st.StartedAt != 200 || st.EndedAt != 0 || st.Attempt != 1 {
		t.Fatalf("auto retry: %+v", st)
	}
	// Red twice fails and ends it.
	s, _ = feed(t, s, at(Launched, 2, 221), at(BatchReturned, 2, 230), at(Red, 2, 240))
	if st := findStep(s.Steps, 2); st.Status != domain.StepFailed || st.StartedAt != 200 || st.EndedAt != 240 {
		t.Fatalf("red twice: %+v", st)
	}
}

func TestStampUserRetryClearsEnd(t *testing.T) {
	s, _ := feed(t, atBranch(), at(BranchReady, 0, 100), at(Launched, 1, 101),
		at(BatchReturned, 1, 110), at(Red, 1, 120), at(Launched, 1, 121), at(BatchReturned, 1, 130), at(Red, 1, 140))
	if st := findStep(s.Steps, 1); st.Status != domain.StepFailed || st.EndedAt != 140 {
		t.Fatalf("failed: %+v", st)
	}
	s, _ = feed(t, s, Event{Kind: Decided, Decision: DecisionRetry, Batch: 1, At: 300})
	if st := findStep(s.Steps, 1); st.Status != domain.StepRunning || st.StartedAt != 100 || st.EndedAt != 0 {
		t.Fatalf("user retry: %+v", st)
	}
}

func setStart(s st, n int, v int64) st {
	steps := append([]domain.Step(nil), s.Steps...)
	for i := range steps {
		if steps[i].N == n {
			steps[i].StartedAt = v
		}
	}
	s.Steps = steps
	return s
}

func TestStampLegacyRunningKeepsNoStart(t *testing.T) {
	s, _ := feed(t, atBranch(), at(BranchReady, 0, 100), at(Launched, 1, 101), at(BatchReturned, 1, 110), at(Green, 1, 200))
	for _, n := range []int{2, 3} {
		if st := findStep(s.Steps, n); st.Status != domain.StepRunning {
			t.Fatalf("setup %d: %+v", n, st)
		}
	}
	s = setStart(s, 2, 0) // saved before the times existed
	s, _ = feed(t, s, at(Launched, 3, 250))
	if st := findStep(s.Steps, 2); st.Status != domain.StepRunning || st.StartedAt != 0 {
		t.Fatalf("unrelated event: %+v", st)
	}
	s, _ = feed(t, s, at(Launched, 2, 251), at(BatchReturned, 2, 260), at(Green, 2, 300))
	if st := findStep(s.Steps, 2); st.StartedAt != 0 || st.EndedAt != 300 {
		t.Fatalf("ended: %+v", st)
	}
}

func TestStampLegacyFailedRetryKeepsNoStart(t *testing.T) {
	s, _ := feed(t, atBranch(), at(BranchReady, 0, 100), at(Launched, 1, 101),
		at(BatchReturned, 1, 110), at(Red, 1, 120), at(Launched, 1, 121), at(BatchReturned, 1, 130), at(Red, 1, 140))
	s = setStart(s, 1, 0)
	s, _ = feed(t, s, Event{Kind: Decided, Decision: DecisionRetry, Batch: 1, At: 300})
	if st := findStep(s.Steps, 1); st.Status != domain.StepRunning || st.StartedAt != 0 || st.EndedAt != 0 {
		t.Fatalf("user retry: %+v", st)
	}
}

func TestStampAcceptEndsIt(t *testing.T) {
	s, _ := feed(t, atBranch(), at(BranchReady, 0, 100), at(Launched, 1, 101),
		at(BatchReturned, 1, 110), at(Red, 1, 120), at(Launched, 1, 121), at(BatchReturned, 1, 130), at(Red, 1, 140))
	s, _ = feed(t, s, Event{Kind: Decided, Decision: DecisionContinue, Batch: 1, At: 400})
	if st := findStep(s.Steps, 1); st.Status != domain.StepAccepted || st.EndedAt != 400 {
		t.Fatalf("accepted: %+v", st)
	}
}

func TestStampZeroLeavesStepsUntouched(t *testing.T) {
	s, _ := feed(t, atBranch(), Event{Kind: BranchReady}, ev(Launched, 1), ev(BatchReturned, 1), ev(Green, 1))
	for _, st := range s.Steps {
		if st.StartedAt != 0 || st.EndedAt != 0 {
			t.Fatalf("stamped without At: %+v", st)
		}
	}
}
