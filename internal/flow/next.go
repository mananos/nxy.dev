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

func decided(d Decision) func(state, Event) bool {
	return func(_ state, ev Event) bool { return ev.Decision == d }
}

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

// retryAs moves to phase p with the retry attempt used.
func retryAs(p domain.Phase, kind domain.ActionKind, batch bool) func(state, Event) (state, []action) {
	return func(s state, _ Event) (state, []action) {
		s.Phase, s.Attempt = p, 1
		a := action{Kind: kind, Retry: true}
		if batch {
			a.Batch = s.Batch
		}
		return s, []action{a}
	}
}

func startBatch(s state, n int, retry bool) (state, []action) {
	s.Phase, s.Batch, s.Attempt = domain.PhaseRunningBatch, n, 0
	return s, []action{{Kind: domain.DispatchImplementer, Batch: n, Retry: retry}}
}

// nextBatch goes to the following batch, or to the affected tests after the last.
func nextBatch(s state, _ Event) (state, []action) {
	if s.Batch < s.Batches {
		return startBatch(s, s.Batch+1, false)
	}
	s.Phase, s.Attempt = domain.PhaseAffected, 0
	return s, acts(domain.RunAffected)
}

func retryBatch(s state, _ Event) (state, []action) { return startBatch(s, s.Batch, false) }

func planSaved(s state, ev Event) (state, []action) {
	s.Batches, s.Attempt = ev.Batches, 0
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

func approved(s state, _ Event) (state, []action) {
	s.Outcome = domain.OutcomeNone
	return startBatch(s, 1, false)
}

func verifyBatch(s state, _ Event) (state, []action) {
	s.Phase, s.Attempt = domain.PhaseVerifyingBatch, 0
	return s, []action{{Kind: domain.VerifyBatch, Batch: s.Batch}}
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
	return finish(domain.OutcomeSuccess)(s, ev)
}

func chosen(s state, ev Event) (state, []action) {
	if ev.Findings {
		return to(domain.PhaseFixing, domain.DispatchFixer)(s, ev)
	}
	return finish(domain.OutcomeSuccess)(s, ev)
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
		{Requested, always, replan},
	},
	domain.PhasePlanning: {
		{PlanSaved, always, planSaved},
		{PlannerReturned, attempt(0), plannerAgain},
		{PlannerReturned, attempt(1), failTo(domain.PhasePlannerFailed)},
	},
	domain.PhaseQuestions:  {{Answered, always, replan}},
	domain.PhaseNoTests:    {{NoTestsAnswered, always, replan}},
	domain.PhaseChangeNote: {{ChangeNoted, always, replan}},
	domain.PhaseApproval: {
		{Approved, always, approved},
		{ChangeRequested, always, changeRequested},
		{ViewFull, always, viewFull},
	},
	domain.PhasePlannerFailed: {
		{Decided, decided(DecisionRetry), replan},
		{Decided, decided(DecisionStop), stop},
	},
	domain.PhaseRunningBatch: {
		{BatchReturned, always, verifyBatch},
	},
	domain.PhaseVerifyingBatch: {
		{Green, always, nextBatch},
		{Red, attempt(0), retryAs(domain.PhaseRunningBatch, domain.DispatchImplementer, true)},
		{Red, attempt(1), failTo(domain.PhaseBatchFailed)},
	},
	domain.PhaseBatchFailed: {
		{Decided, decided(DecisionRetry), retryBatch},
		{Decided, decided(DecisionContinue), nextBatch},
		{Decided, decided(DecisionStop), stop},
	},
	domain.PhaseAffected: {
		{Green, always, review},
		{Red, attempt(0), retryAs(domain.PhaseAffectedFix, domain.DispatchAffectedFix, false)},
		{Red, attempt(1), failTo(domain.PhaseAffectedFailed)},
	},
	domain.PhaseAffectedFix: {
		{FixesReturned, always, to(domain.PhaseAffected, domain.RunAffected)},
	},
	domain.PhaseAffectedFailed: {
		{Decided, decided(DecisionRetry), to(domain.PhaseAffectedFix, domain.DispatchAffectedFix)},
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
		{FixesReturned, always, finish(domain.OutcomeSuccess)},
	},
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
				return ns, as, nil
			}
		}
	}
	return s, nil, ErrOutOfOrder
}
