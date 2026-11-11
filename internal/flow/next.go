// Package flow decides what happens next in a feature. It is pure: a state and
// an event go in, a new state and the actions to perform come out.
package flow

import (
	"errors"

	"github.com/mananos/nxy.dev/internal/domain"
)

// ErrOutOfOrder means the event does not apply to the current state; the state
// is returned unchanged.
var ErrOutOfOrder = errors.New("flow: event out of order")

type (
	state  = domain.State
	action = domain.Action
)

type row struct {
	on    EventKind
	guard func(state, Event) bool
	apply func(state, Event) (state, []action)
}

func always(state, Event) bool { return true }

func engineer(_ state, ev Event) bool { return ev.Engineer }

func decided(d Decision) func(state, Event) bool {
	return func(_ state, ev Event) bool { return ev.Decision == d }
}

func blocked(_ state, ev Event) bool { return ev.Blocked }

func attempt(n int) func(state, Event) bool {
	return func(s state, _ Event) bool { return s.Attempt == n }
}

func acts(kinds ...domain.ActionKind) []action {
	out := make([]action, len(kinds))
	for i, k := range kinds {
		out[i] = action{Kind: k}
	}
	return out
}

// to builds an apply that moves to phase p (attempt reset) and emits kinds.
func to(p domain.Phase, kinds ...domain.ActionKind) func(state, Event) (state, []action) {
	return func(s state, _ Event) (state, []action) {
		s.Phase, s.Attempt = p, 0
		return s, acts(kinds...)
	}
}

func finish(o domain.Outcome) func(state, Event) (state, []action) {
	return func(s state, _ Event) (state, []action) {
		s.Phase, s.Attempt, s.Outcome = domain.PhaseDone, 0, o
		return s, acts(domain.Close)
	}
}

// closeOK is the end of a feature that went well: the docs that name the change are looked for
// first, and the feature closes after that step.
var closeOK = to(domain.PhaseDocsCheck, domain.CheckDocs)

// closeSuccess ends a feature that went well: with rule candidates it asks which to keep first.
func closeSuccess(s state, ev Event) (state, []action) {
	if ev.Candidates {
		return to(domain.PhaseRules, domain.AskRules, domain.Notify)(s, ev)
	}
	return finish(domain.OutcomeSuccess)(s, ev)
}

func docsChecked(s state, ev Event) (state, []action) {
	if ev.Docs {
		return to(domain.PhaseDocs, domain.AskDocs, domain.Notify)(s, ev)
	}
	return closeSuccess(s, ev)
}

func docsAnswered(s state, ev Event) (state, []action) {
	if ev.Update {
		return to(domain.PhaseDocumenting, domain.DispatchDocumenter)(s, ev)
	}
	return closeSuccess(s, ev)
}

// retryAs moves to phase p with the retry attempt used.
func retryAs(p domain.Phase, kind domain.ActionKind) func(state, Event) (state, []action) {
	return func(s state, _ Event) (state, []action) {
		s.Phase, s.Attempt = p, 1
		return s, []action{{Kind: kind, Retry: true}}
	}
}

// batchOf is the batch an event is about: the one it names, or the one in focus.
func batchOf(s state, ev Event) int {
	if ev.Batch > 0 {
		return ev.Batch
	}
	return s.Batch
}

func findStep(ss []domain.Step, n int) *domain.Step {
	for i := range ss {
		if ss[i].N == n {
			return &ss[i]
		}
	}
	return nil
}

// steps is the schedule of the state, as a copy that can be changed. A state saved before parallel
// batches has none: it is rebuilt as one batch after the other around the batch in focus.
func steps(s state) []domain.Step {
	if len(s.Steps) > 0 {
		return append([]domain.Step(nil), s.Steps...)
	}
	out := make([]domain.Step, s.Batches)
	for i := range out {
		n := i + 1
		out[i].N = n
		if n > 1 {
			out[i].After = []int{n - 1}
		}
		switch {
		case n < s.Batch:
			out[i].Status = domain.StepGreen
		case n == s.Batch:
			switch s.Phase {
			case domain.PhaseRunningBatch:
				out[i].Status, out[i].Attempt = domain.StepRunning, s.Attempt
			case domain.PhaseVerifyingBatch:
				out[i].Status, out[i].Attempt = domain.StepVerifying, s.Attempt
			case domain.PhaseBatchFailed:
				out[i].Status, out[i].Attempt = domain.StepFailed, s.Attempt
			default:
				out[i].Status = domain.StepGreen
			}
		}
	}
	return out
}

// hasStep guards on the status of the batch of the event.
func hasStep(status domain.StepStatus) func(state, Event) bool {
	return func(s state, ev Event) bool {
		st := findStep(steps(s), batchOf(s, ev))
		return st != nil && st.Status == status
	}
}

func and(gs ...func(state, Event) bool) func(state, Event) bool {
	return func(s state, ev Event) bool {
		for _, g := range gs {
			if !g(s, ev) {
				return false
			}
		}
		return true
	}
}

func ready(ss []domain.Step, st domain.Step) bool {
	for _, n := range st.After {
		d := findStep(ss, n)
		if d != nil && d.Status != domain.StepGreen && d.Status != domain.StepAccepted {
			return false
		}
	}
	return true
}

// schedule starts every batch whose dependencies are done and says what to do: dispatch the ones
// not launched yet, wait for the launched ones, ask about a failed batch once nothing runs or
// starts, or go to the affected tests when every batch is done.
func schedule(s state, ss []domain.Step) (state, []action) {
	for i := range ss {
		if ss[i].Status == domain.StepPending && ready(ss, ss[i]) {
			ss[i].Status, ss[i].Launched, ss[i].Attempt = domain.StepRunning, false, 0
		}
	}
	var unlaunched, running []int
	verifying, failed := false, 0
	for _, st := range ss {
		switch st.Status {
		case domain.StepRunning:
			running = append(running, st.N)
			if !st.Launched {
				unlaunched = append(unlaunched, st.N)
			}
		case domain.StepVerifying:
			verifying = true
		case domain.StepFailed:
			if failed == 0 {
				failed = st.N
			}
		}
	}
	s.Steps, s.Attempt = ss, 0
	switch {
	case len(unlaunched) > 0:
		s.Phase, s.Batch = domain.PhaseRunningBatch, unlaunched[0]
		// Retry is only set for a single batch; with 2+ at once it stays false (each prompt names its batch).
		a := action{Kind: domain.DispatchImplementer, Batch: unlaunched[0]}
		if len(unlaunched) > 1 {
			a.Batches = unlaunched
		} else {
			a.Retry = findStep(ss, unlaunched[0]).Attempt > 0
		}
		return s, []action{a}
	case len(running) > 0:
		s.Phase, s.Batch = domain.PhaseRunningBatch, running[0]
		a := action{Kind: domain.AwaitBatches, Batch: running[0]}
		if len(running) > 1 {
			a.Batches = running
		}
		return s, []action{a}
	case verifying:
		s.Phase = domain.PhaseRunningBatch
		return s, nil
	case failed > 0:
		s.Phase, s.Batch = domain.PhaseBatchFailed, failed
		return s, acts(domain.AskDecision, domain.Notify)
	}
	s.Phase = domain.PhaseAffected
	return s, acts(domain.RunAffected)
}

// mark changes the batch of the event and schedules what follows.
func mark(f func(*domain.Step)) func(state, Event) (state, []action) {
	return func(s state, ev Event) (state, []action) {
		ss := steps(s)
		if st := findStep(ss, batchOf(s, ev)); st != nil {
			f(st)
		}
		return schedule(s, ss)
	}
}

var (
	// launched: the implementer of the batch is up; what is still not launched is asked again.
	launched = mark(func(st *domain.Step) { st.Launched = true })
	// batchGreen: the proof is green.
	batchGreen = mark(func(st *domain.Step) { st.Status = domain.StepGreen })
	// batchRed: the first red retries the batch; a second one (counted per batch) fails it.
	batchRed = mark(func(st *domain.Step) {
		if st.Attempt == 0 {
			st.Status, st.Attempt, st.Launched = domain.StepRunning, 1, false
			return
		}
		st.Status = domain.StepFailed
	})
	retryBatch  = mark(func(st *domain.Step) { st.Status, st.Attempt, st.Launched = domain.StepRunning, 0, false })
	acceptBatch = mark(func(st *domain.Step) { st.Status = domain.StepAccepted })
)

// batchReturned sends the batch to its proof.
func batchReturned(s state, ev Event) (state, []action) {
	ss := steps(s)
	n := batchOf(s, ev)
	findStep(ss, n).Status = domain.StepVerifying
	s.Steps, s.Attempt = ss, 0
	s.Phase = domain.PhaseRunningBatch
	return s, []action{{Kind: domain.VerifyBatch, Batch: n}}
}

// relaunch: the implementers that were launched are gone with the old process.
func relaunch(s state, _ Event) (state, []action) {
	ss := steps(s)
	for i := range ss {
		if ss[i].Status == domain.StepRunning {
			ss[i].Launched = false
		}
	}
	return schedule(s, ss)
}

// reverify runs the affected tests again after a fix, keeping the attempt: a
// second red must reach affected_failed, not retry forever.
func reverify(s state, _ Event) (state, []action) {
	s.Phase = domain.PhaseAffected
	return s, acts(domain.RunAffected)
}

// verifyFixes runs the affected tests after the review fixes, keeping the
// attempt: a second red must reach fixes_failed.
func verifyFixes(s state, _ Event) (state, []action) {
	s.Phase = domain.PhaseFixVerify
	return s, acts(domain.RunAffected)
}

func planSaved(s state, ev Event) (state, []action) {
	s.Batches, s.Attempt = ev.Batches, 0
	// Without steps the schedule is one batch after the other (see steps).
	s.Steps = append([]domain.Step(nil), ev.Steps...)
	if len(s.Steps) == 0 {
		s.Steps = nil
	}
	switch {
	case ev.Questions:
		s.Phase = domain.PhaseQuestions
		return s, acts(domain.AskQuestions, domain.Notify)
	case ev.NoTests:
		s.Phase = domain.PhaseNoTests
		return s, acts(domain.AskNoTests, domain.Notify)
	}
	s.Phase = domain.PhaseApproval
	return s, acts(domain.AskApproval, domain.Notify)
}

// approved asks for the branch (and the base of each repo) before the first batch.
func approved(s state, _ Event) (state, []action) {
	s.Outcome = domain.OutcomeNone
	s.Phase = domain.PhaseBranch
	return s, acts(domain.AskBranch, domain.Notify)
}

func branchReady(s state, _ Event) (state, []action) {
	s.Batch = 0
	return schedule(s, steps(s))
}

func reviewed(s state, ev Event) (state, []action) {
	if ev.HighUnproven {
		return to(domain.PhaseRefuting, domain.DispatchRefuter)(s, ev)
	}
	return afterFindings(s, ev)
}

// afterFindings asks which findings to fix, or closes when there are none.
func afterFindings(s state, ev Event) (state, []action) {
	if ev.Findings {
		return to(domain.PhaseChoosingFindings, domain.AskFindings, domain.Notify)(s, ev)
	}
	return closeOK(s, ev)
}

func chosen(s state, ev Event) (state, []action) {
	if ev.Findings {
		return to(domain.PhaseFixing, domain.DispatchFixer)(s, ev)
	}
	return closeOK(s, ev)
}

func changeRequested(s state, ev Event) (state, []action) {
	if ev.HasNote {
		return to(domain.PhasePlanning, domain.DispatchPlanner)(s, ev)
	}
	return to(domain.PhaseChangeNote, domain.AskChangeNote)(s, ev)
}

func plannerAgain(s state, _ Event) (state, []action) {
	s.Phase, s.Attempt = domain.PhasePlanning, 1
	return s, []action{{Kind: domain.DispatchPlanner, Retry: true}}
}

func viewFull(s state, _ Event) (state, []action) {
	return s, acts(domain.OpenPlan, domain.AskApproval)
}

var (
	replan = to(domain.PhasePlanning, domain.DispatchPlanner)
	review = to(domain.PhaseReviewing, domain.DispatchReviewer)
	stop   = finish(domain.OutcomeStopped)
)

// failTo moves to a *_failed phase and asks the user what to do.
func failTo(p domain.Phase) func(state, Event) (state, []action) {
	return to(p, domain.AskDecision, domain.Notify)
}

var table = map[domain.Phase][]row{
	domain.PhaseNone: {
		{Requested, engineer, to(domain.PhaseDesigning, domain.DesignDebate)},
		{Requested, always, replan},
	},
	domain.PhaseDesigning: {
		{DesignClosed, always, replan},
	},
	domain.PhasePlanning: {
		{PlanSaved, always, planSaved},
		{GapsFound, always, to(domain.PhaseDesigning, domain.DesignDebate, domain.Notify)},
		{PlannerReturned, blocked, to(domain.PhaseRepoClash, domain.AskClash, domain.Notify)},
		{PlannerReturned, attempt(0), plannerAgain},
		{PlannerReturned, attempt(1), failTo(domain.PhasePlannerFailed)},
	},
	domain.PhaseQuestions: {
		{ToEngineer, always, to(domain.PhaseDesigning, domain.DesignDebate)},
		{Answered, always, replan},
	},
	domain.PhaseNoTests:    {{NoTestsAnswered, always, replan}},
	domain.PhaseChangeNote: {{ChangeNoted, always, replan}},
	domain.PhaseApproval: {
		{Approved, always, approved},
		{ChangeRequested, always, changeRequested},
		{ViewFull, always, viewFull},
	},
	domain.PhaseBranch: {{BranchReady, always, branchReady}},
	domain.PhasePlannerFailed: {
		{Decided, decided(DecisionRetry), replan},
		{Decided, decided(DecisionStop), stop},
	},
	// The plan waits in the feature: pausing the other feature or going to a worktree takes the
	// same road as a saved plan, without another planner; dropping the repo re-plans.
	domain.PhaseRepoClash: {
		{ClashAnswered, always, planSaved},
		{Answered, always, replan},
	},
	// The batches run in parallel when their dependencies are done: the phase stays here while
	// any is running or being verified, each with its own status and retry in State.Steps.
	domain.PhaseRunningBatch: {
		{Launched, hasStep(domain.StepRunning), launched},
		{Relaunch, always, relaunch},
		{BatchReturned, hasStep(domain.StepRunning), batchReturned},
		{Green, hasStep(domain.StepVerifying), batchGreen},
		{Red, hasStep(domain.StepVerifying), batchRed},
	},
	// Only a state saved by an older binary is here; it goes on as running_batch.
	domain.PhaseVerifyingBatch: {
		{Green, hasStep(domain.StepVerifying), batchGreen},
		{Red, hasStep(domain.StepVerifying), batchRed},
	},
	domain.PhaseBatchFailed: {
		{Decided, and(decided(DecisionRetry), hasStep(domain.StepFailed)), retryBatch},
		{Decided, and(decided(DecisionContinue), hasStep(domain.StepFailed)), acceptBatch},
		{Decided, decided(DecisionStop), stop},
	},
	domain.PhaseAffected: {
		{Green, always, review},
		{Red, attempt(0), retryAs(domain.PhaseAffectedFix, domain.DispatchAffectedFix)},
		{Red, attempt(1), failTo(domain.PhaseAffectedFailed)},
		{Unverified, always, failTo(domain.PhaseAffectedUnverified)},
	},
	domain.PhaseAffectedFix: {
		{FixesReturned, always, reverify},
	},
	domain.PhaseAffectedFailed: {
		{Decided, decided(DecisionRetry), to(domain.PhaseAffectedFix, domain.DispatchAffectedFix)},
		{Decided, decided(DecisionContinue), review},
		{Decided, decided(DecisionStop), stop},
	},
	domain.PhaseAffectedUnverified: {
		{Decided, decided(DecisionRetry), to(domain.PhaseAffected, domain.RunAffected)},
		{Decided, decided(DecisionContinue), review},
		{Decided, decided(DecisionStop), stop},
	},
	domain.PhaseReviewing: {
		{ReviewDone, always, reviewed},
	},
	domain.PhaseRefuting: {
		{RefuterDone, always, afterFindings},
	},
	domain.PhaseChoosingFindings: {
		{Chosen, always, chosen},
	},
	domain.PhaseFixing: {
		{FixesReturned, always, verifyFixes},
	},
	domain.PhaseFixVerify: {
		{Green, always, closeOK},
		{Red, attempt(0), retryAs(domain.PhaseFixing, domain.DispatchFixer)},
		{Red, attempt(1), failTo(domain.PhaseFixesFailed)},
		{Unverified, always, failTo(domain.PhaseFixesFailed)},
	},
	domain.PhaseFixesFailed: {
		{Decided, decided(DecisionRetry), to(domain.PhaseFixing, domain.DispatchFixer)},
		{Decided, decided(DecisionContinue), closeOK},
		{Decided, decided(DecisionStop), stop},
	},
	domain.PhaseDocsCheck:   {{DocsChecked, always, docsChecked}},
	domain.PhaseDocs:        {{DocsAnswered, always, docsAnswered}},
	domain.PhaseDocumenting: {{DocumenterReturned, always, closeSuccess}},
	domain.PhaseRules:       {{RulesAnswered, always, finish(domain.OutcomeSuccess)}},
}

// common rows apply to every non-terminal phase, so they are not repeated.
var common = []row{
	{Stop, func(s state, _ Event) bool {
		return s.Phase != domain.PhaseNone && s.Phase != domain.PhaseDone
	}, stop},
}

// Next applies ev to s. It returns the new state and the actions to perform, or
// ErrOutOfOrder (with s unchanged) when the event does not fit the state.
func Next(s domain.State, ev Event) (domain.State, []domain.Action, error) {
	for _, rows := range [][]row{table[s.Phase], common} {
		for _, r := range rows {
			if r.on == ev.Kind && r.guard(s, ev) {
				ns, as := r.apply(s, ev)
				if ev.At != 0 && len(ns.Steps) > 0 {
					ns.Steps = stamp(steps(s), ns.Steps, ev.At)
				}
				return ns, as, nil
			}
		}
	}
	return s, nil, ErrOutOfOrder
}

// stamp returns after (a copy) with the times of the steps that changed status: a step that
// starts running keeps the start of its first attempt, one that ends (green, failed or accepted)
// gets the end, and one that goes from failed back to running loses it.
func stamp(before, after []domain.Step, at int64) []domain.Step {
	out := append([]domain.Step(nil), after...)
	for i := range out {
		prev := domain.StepPending
		if b := findStep(before, out[i].N); b != nil {
			prev = b.Status
		}
		st := &out[i]
		switch st.Status {
		case domain.StepRunning:
			if st.StartedAt == 0 {
				st.StartedAt = at
			}
			if prev == domain.StepFailed {
				st.EndedAt = 0
			}
		case domain.StepGreen, domain.StepFailed, domain.StepAccepted:
			if prev != st.Status {
				st.EndedAt = at
			}
		}
	}
	return out
}
