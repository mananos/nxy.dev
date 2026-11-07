package flow

import (
	"errors"
	"reflect"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

type st = domain.State

func acs(kinds ...domain.ActionKind) []domain.Action {
	out := make([]domain.Action, len(kinds))
	for i, k := range kinds {
		out[i] = domain.Action{Kind: k}
	}
	return out
}

const (
	pend     = domain.StepPending
	run      = domain.StepRunning
	verif    = domain.StepVerifying
	green    = domain.StepGreen
	failed   = domain.StepFailed
	accepted = domain.StepAccepted
)

// sp is a step in a plan with one batch after the other.
func sp(n int, status domain.StepStatus, attempt int) domain.Step {
	s := domain.Step{N: n, Status: status, Attempt: attempt}
	if n > 1 {
		s.After = []int{n - 1}
	}
	return s
}

func sps(ss ...domain.Step) []domain.Step { return ss }

// spd is a step with explicit dependencies.
func spd(n int, repo string, after ...int) domain.Step {
	return domain.Step{N: n, Repo: repo, After: after}
}

func one(a domain.Action) []domain.Action { return []domain.Action{a} }

type tcase struct {
	name string
	from st
	ev   Event
	to   st
	want []domain.Action
}

func TestNextTable(t *testing.T) {
	impl := func(n int, retry bool) []domain.Action {
		return one(domain.Action{Kind: domain.DispatchImplementer, Batch: n, Retry: retry})
	}
	cases := []tcase{
		{"requested", st{}, Event{Kind: Requested},
			st{Phase: domain.PhasePlanning}, acs(domain.DispatchPlanner)},
		{"engineer requested", st{}, Event{Kind: Requested, Engineer: true},
			st{Phase: domain.PhaseDesigning}, acs(domain.DesignDebate)},
		{"design closed", st{Phase: domain.PhaseDesigning}, Event{Kind: DesignClosed},
			st{Phase: domain.PhasePlanning}, acs(domain.DispatchPlanner)},
		{"gaps found", st{Phase: domain.PhasePlanning}, Event{Kind: GapsFound},
			st{Phase: domain.PhaseDesigning}, acs(domain.DesignDebate, domain.Notify)},
		{"offer accepted", st{Phase: domain.PhaseQuestions}, Event{Kind: ToEngineer},
			st{Phase: domain.PhaseDesigning}, acs(domain.DesignDebate)},
		{"plan saved with questions", st{Phase: domain.PhasePlanning}, Event{Kind: PlanSaved, Questions: true, NoTests: true, Batches: 3},
			st{Phase: domain.PhaseQuestions, Batches: 3}, acs(domain.AskQuestions, domain.Notify)},
		{"plan saved no tests", st{Phase: domain.PhasePlanning}, Event{Kind: PlanSaved, NoTests: true, Batches: 3},
			st{Phase: domain.PhaseNoTests, Batches: 3}, acs(domain.AskNoTests, domain.Notify)},
		{"plan saved clean", st{Phase: domain.PhasePlanning, Attempt: 1}, Event{Kind: PlanSaved, Batches: 2},
			st{Phase: domain.PhaseApproval, Batches: 2}, acs(domain.AskApproval, domain.Notify)},
		{"planner returned first", st{Phase: domain.PhasePlanning}, Event{Kind: PlannerReturned},
			st{Phase: domain.PhasePlanning, Attempt: 1}, one(domain.Action{Kind: domain.DispatchPlanner, Retry: true})},
		{"planner returned second", st{Phase: domain.PhasePlanning, Attempt: 1}, Event{Kind: PlannerReturned},
			st{Phase: domain.PhasePlannerFailed}, acs(domain.AskDecision, domain.Notify)},
		{"planner returned blocked", st{Phase: domain.PhasePlanning, Attempt: 1}, Event{Kind: PlannerReturned, Blocked: true},
			st{Phase: domain.PhaseRepoClash}, acs(domain.AskClash, domain.Notify)},
		{"clash answered clean", st{Phase: domain.PhaseRepoClash}, Event{Kind: ClashAnswered, Batches: 2},
			st{Phase: domain.PhaseApproval, Batches: 2}, acs(domain.AskApproval, domain.Notify)},
		{"clash answered with questions", st{Phase: domain.PhaseRepoClash}, Event{Kind: ClashAnswered, Questions: true, Batches: 2},
			st{Phase: domain.PhaseQuestions, Batches: 2}, acs(domain.AskQuestions, domain.Notify)},
		{"clash answered no tests", st{Phase: domain.PhaseRepoClash}, Event{Kind: ClashAnswered, NoTests: true, Batches: 2},
			st{Phase: domain.PhaseNoTests, Batches: 2}, acs(domain.AskNoTests, domain.Notify)},
		{"clash drop replans", st{Phase: domain.PhaseRepoClash}, Event{Kind: Answered},
			st{Phase: domain.PhasePlanning}, acs(domain.DispatchPlanner)},
		{"answered", st{Phase: domain.PhaseQuestions}, Event{Kind: Answered},
			st{Phase: domain.PhasePlanning}, acs(domain.DispatchPlanner)},
		{"no tests answered", st{Phase: domain.PhaseNoTests}, Event{Kind: NoTestsAnswered},
			st{Phase: domain.PhasePlanning}, acs(domain.DispatchPlanner)},
		{"change noted", st{Phase: domain.PhaseChangeNote}, Event{Kind: ChangeNoted},
			st{Phase: domain.PhasePlanning}, acs(domain.DispatchPlanner)},
		{"approved", st{Phase: domain.PhaseApproval, Batches: 3}, Event{Kind: Approved},
			st{Phase: domain.PhaseBranch, Batches: 3}, acs(domain.AskBranch, domain.Notify)},
		{"branch ready", st{Phase: domain.PhaseBranch, Batches: 3}, Event{Kind: BranchReady},
			st{Phase: domain.PhaseRunningBatch, Batch: 1, Batches: 3, Steps: sps(sp(1, run, 0), sp(2, pend, 0), sp(3, pend, 0))}, impl(1, false)},
		{"change with note", st{Phase: domain.PhaseApproval, Batches: 3}, Event{Kind: ChangeRequested, HasNote: true},
			st{Phase: domain.PhasePlanning, Batches: 3}, acs(domain.DispatchPlanner)},
		{"change without note", st{Phase: domain.PhaseApproval, Batches: 3}, Event{Kind: ChangeRequested},
			st{Phase: domain.PhaseChangeNote, Batches: 3}, acs(domain.AskChangeNote)},
		{"view full", st{Phase: domain.PhaseApproval, Batches: 3}, Event{Kind: ViewFull},
			st{Phase: domain.PhaseApproval, Batches: 3}, acs(domain.OpenPlan, domain.AskApproval)},
		{"planner failed retry", st{Phase: domain.PhasePlannerFailed}, Event{Kind: Decided, Decision: DecisionRetry},
			st{Phase: domain.PhasePlanning}, acs(domain.DispatchPlanner)},
		{"planner failed stop", st{Phase: domain.PhasePlannerFailed}, Event{Kind: Decided, Decision: DecisionStop},
			st{Phase: domain.PhaseDone, Outcome: domain.OutcomeStopped}, acs(domain.Close)},
		{"batch returned", st{Phase: domain.PhaseRunningBatch, Batch: 2, Batches: 3, Attempt: 1}, Event{Kind: BatchReturned},
			st{Phase: domain.PhaseRunningBatch, Batch: 2, Batches: 3, Steps: sps(sp(1, green, 0), sp(2, verif, 1), sp(3, pend, 0))},
			one(domain.Action{Kind: domain.VerifyBatch, Batch: 2})},
		{"green next batch", st{Phase: domain.PhaseVerifyingBatch, Batch: 1, Batches: 2}, Event{Kind: Green},
			st{Phase: domain.PhaseRunningBatch, Batch: 2, Batches: 2, Steps: sps(sp(1, green, 0), sp(2, run, 0))}, impl(2, false)},
		{"green last batch", st{Phase: domain.PhaseVerifyingBatch, Batch: 2, Batches: 2}, Event{Kind: Green},
			st{Phase: domain.PhaseAffected, Batch: 2, Batches: 2, Steps: sps(sp(1, green, 0), sp(2, green, 0))}, acs(domain.RunAffected)},
		{"red first", st{Phase: domain.PhaseVerifyingBatch, Batch: 2, Batches: 3}, Event{Kind: Red},
			st{Phase: domain.PhaseRunningBatch, Batch: 2, Batches: 3, Steps: sps(sp(1, green, 0), sp(2, run, 1), sp(3, pend, 0))}, impl(2, true)},
		{"red second", st{Phase: domain.PhaseVerifyingBatch, Batch: 2, Batches: 3, Attempt: 1}, Event{Kind: Red},
			st{Phase: domain.PhaseBatchFailed, Batch: 2, Batches: 3, Steps: sps(sp(1, green, 0), sp(2, failed, 1), sp(3, pend, 0))}, acs(domain.AskDecision, domain.Notify)},
		{"batch failed retry", st{Phase: domain.PhaseBatchFailed, Batch: 2, Batches: 3}, Event{Kind: Decided, Decision: DecisionRetry},
			st{Phase: domain.PhaseRunningBatch, Batch: 2, Batches: 3, Steps: sps(sp(1, green, 0), sp(2, run, 0), sp(3, pend, 0))}, impl(2, false)},
		{"batch failed continue next", st{Phase: domain.PhaseBatchFailed, Batch: 2, Batches: 3}, Event{Kind: Decided, Decision: DecisionContinue},
			st{Phase: domain.PhaseRunningBatch, Batch: 3, Batches: 3, Steps: sps(sp(1, green, 0), sp(2, accepted, 0), sp(3, run, 0))}, impl(3, false)},
		{"batch failed continue last", st{Phase: domain.PhaseBatchFailed, Batch: 3, Batches: 3}, Event{Kind: Decided, Decision: DecisionContinue},
			st{Phase: domain.PhaseAffected, Batch: 3, Batches: 3, Steps: sps(sp(1, green, 0), sp(2, green, 0), sp(3, accepted, 0))}, acs(domain.RunAffected)},
		{"batch failed stop", st{Phase: domain.PhaseBatchFailed, Batch: 2, Batches: 3}, Event{Kind: Decided, Decision: DecisionStop},
			st{Phase: domain.PhaseDone, Batch: 2, Batches: 3, Outcome: domain.OutcomeStopped}, acs(domain.Close)},
		{"affected green", st{Phase: domain.PhaseAffected, Batches: 2}, Event{Kind: Green},
			st{Phase: domain.PhaseReviewing, Batches: 2}, acs(domain.DispatchReviewer)},
		{"affected red first", st{Phase: domain.PhaseAffected}, Event{Kind: Red},
			st{Phase: domain.PhaseAffectedFix, Attempt: 1}, one(domain.Action{Kind: domain.DispatchAffectedFix, Retry: true})},
		{"affected red second", st{Phase: domain.PhaseAffected, Attempt: 1}, Event{Kind: Red},
			st{Phase: domain.PhaseAffectedFailed}, acs(domain.AskDecision, domain.Notify)},
		{"affected fix returned", st{Phase: domain.PhaseAffectedFix, Attempt: 1}, Event{Kind: FixesReturned},
			st{Phase: domain.PhaseAffected, Attempt: 1}, acs(domain.RunAffected)},
		{"affected failed retry", st{Phase: domain.PhaseAffectedFailed}, Event{Kind: Decided, Decision: DecisionRetry},
			st{Phase: domain.PhaseAffectedFix}, acs(domain.DispatchAffectedFix)},
		{"affected failed continue", st{Phase: domain.PhaseAffectedFailed}, Event{Kind: Decided, Decision: DecisionContinue},
			st{Phase: domain.PhaseReviewing}, acs(domain.DispatchReviewer)},
		{"affected unverified", st{Phase: domain.PhaseAffected}, Event{Kind: Unverified},
			st{Phase: domain.PhaseAffectedUnverified}, acs(domain.AskDecision, domain.Notify)},
		{"affected unverified retry", st{Phase: domain.PhaseAffectedUnverified}, Event{Kind: Decided, Decision: DecisionRetry},
			st{Phase: domain.PhaseAffected}, acs(domain.RunAffected)},
		{"affected unverified continue", st{Phase: domain.PhaseAffectedUnverified}, Event{Kind: Decided, Decision: DecisionContinue},
			st{Phase: domain.PhaseReviewing}, acs(domain.DispatchReviewer)},
		{"affected unverified stop", st{Phase: domain.PhaseAffectedUnverified}, Event{Kind: Decided, Decision: DecisionStop},
			st{Phase: domain.PhaseDone, Outcome: domain.OutcomeStopped}, acs(domain.Close)},
		{"affected failed stop", st{Phase: domain.PhaseAffectedFailed}, Event{Kind: Decided, Decision: DecisionStop},
			st{Phase: domain.PhaseDone, Outcome: domain.OutcomeStopped}, acs(domain.Close)},
		{"review high unproven", st{Phase: domain.PhaseReviewing}, Event{Kind: ReviewDone, HighUnproven: true, Findings: true},
			st{Phase: domain.PhaseRefuting}, acs(domain.DispatchRefuter)},
		{"review findings", st{Phase: domain.PhaseReviewing}, Event{Kind: ReviewDone, Findings: true},
			st{Phase: domain.PhaseChoosingFindings}, acs(domain.AskFindings, domain.Notify)},
		{"review clean", st{Phase: domain.PhaseReviewing}, Event{Kind: ReviewDone},
			st{Phase: domain.PhaseDocsCheck}, acs(domain.CheckDocs)},
		{"refuter findings", st{Phase: domain.PhaseRefuting}, Event{Kind: RefuterDone, Findings: true},
			st{Phase: domain.PhaseChoosingFindings}, acs(domain.AskFindings, domain.Notify)},
		{"refuter clean", st{Phase: domain.PhaseRefuting}, Event{Kind: RefuterDone},
			st{Phase: domain.PhaseDocsCheck}, acs(domain.CheckDocs)},
		{"chosen some", st{Phase: domain.PhaseChoosingFindings}, Event{Kind: Chosen, Findings: true},
			st{Phase: domain.PhaseFixing}, acs(domain.DispatchFixer)},
		{"chosen none", st{Phase: domain.PhaseChoosingFindings}, Event{Kind: Chosen},
			st{Phase: domain.PhaseDocsCheck}, acs(domain.CheckDocs)},
		{"fixes returned", st{Phase: domain.PhaseFixing, Attempt: 1}, Event{Kind: FixesReturned},
			st{Phase: domain.PhaseFixVerify, Attempt: 1}, acs(domain.RunAffected)},
		{"fix verify green", st{Phase: domain.PhaseFixVerify}, Event{Kind: Green},
			st{Phase: domain.PhaseDocsCheck}, acs(domain.CheckDocs)},
		{"fix verify red first", st{Phase: domain.PhaseFixVerify}, Event{Kind: Red},
			st{Phase: domain.PhaseFixing, Attempt: 1}, one(domain.Action{Kind: domain.DispatchFixer, Retry: true})},
		{"fix verify red second", st{Phase: domain.PhaseFixVerify, Attempt: 1}, Event{Kind: Red},
			st{Phase: domain.PhaseFixesFailed}, acs(domain.AskDecision, domain.Notify)},
		{"fix verify unverified", st{Phase: domain.PhaseFixVerify}, Event{Kind: Unverified},
			st{Phase: domain.PhaseFixesFailed}, acs(domain.AskDecision, domain.Notify)},
		{"fixes failed retry", st{Phase: domain.PhaseFixesFailed}, Event{Kind: Decided, Decision: DecisionRetry},
			st{Phase: domain.PhaseFixing}, acs(domain.DispatchFixer)},
		{"fixes failed continue", st{Phase: domain.PhaseFixesFailed}, Event{Kind: Decided, Decision: DecisionContinue},
			st{Phase: domain.PhaseDocsCheck}, acs(domain.CheckDocs)},
		{"docs found", st{Phase: domain.PhaseDocsCheck}, Event{Kind: DocsChecked, Docs: true},
			st{Phase: domain.PhaseDocs}, acs(domain.AskDocs, domain.Notify)},
		{"no docs", st{Phase: domain.PhaseDocsCheck}, Event{Kind: DocsChecked},
			st{Phase: domain.PhaseDone, Outcome: domain.OutcomeSuccess}, acs(domain.Close)},
		{"docs update", st{Phase: domain.PhaseDocs}, Event{Kind: DocsAnswered, Update: true},
			st{Phase: domain.PhaseDocumenting}, acs(domain.DispatchDocumenter)},
		{"docs leave", st{Phase: domain.PhaseDocs}, Event{Kind: DocsAnswered},
			st{Phase: domain.PhaseDone, Outcome: domain.OutcomeSuccess}, acs(domain.Close)},
		{"documenter returned", st{Phase: domain.PhaseDocumenting}, Event{Kind: DocumenterReturned},
			st{Phase: domain.PhaseDone, Outcome: domain.OutcomeSuccess}, acs(domain.Close)},
		{"no docs with candidates", st{Phase: domain.PhaseDocsCheck}, Event{Kind: DocsChecked, Candidates: true},
			st{Phase: domain.PhaseRules}, acs(domain.AskRules, domain.Notify)},
		{"docs leave with candidates", st{Phase: domain.PhaseDocs}, Event{Kind: DocsAnswered, Candidates: true},
			st{Phase: domain.PhaseRules}, acs(domain.AskRules, domain.Notify)},
		{"documenter returned with candidates", st{Phase: domain.PhaseDocumenting}, Event{Kind: DocumenterReturned, Candidates: true},
			st{Phase: domain.PhaseRules}, acs(domain.AskRules, domain.Notify)},
		{"docs update ignores candidates", st{Phase: domain.PhaseDocs}, Event{Kind: DocsAnswered, Update: true, Candidates: true},
			st{Phase: domain.PhaseDocumenting}, acs(domain.DispatchDocumenter)},
		{"rules answered", st{Phase: domain.PhaseRules}, Event{Kind: RulesAnswered},
			st{Phase: domain.PhaseDone, Outcome: domain.OutcomeSuccess}, acs(domain.Close)},
		{"fixes failed stop", st{Phase: domain.PhaseFixesFailed}, Event{Kind: Decided, Decision: DecisionStop},
			st{Phase: domain.PhaseDone, Outcome: domain.OutcomeStopped}, acs(domain.Close)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, acts, err := Next(c.from, c.ev)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !reflect.DeepEqual(got, c.to) {
				t.Errorf("state = %+v, want %+v", got, c.to)
			}
			if !reflect.DeepEqual(acts, c.want) {
				t.Errorf("actions = %+v, want %+v", acts, c.want)
			}
		})
	}
}

// The retry is counted across the whole loop: a second red ends in *_failed.
func TestRetryAttemptCarriesOver(t *testing.T) {
	loops := []struct {
		name   string
		start  st
		events []EventKind
		want   domain.Phase
	}{
		{"batch", st{Phase: domain.PhaseRunningBatch, Batch: 1, Batches: 2},
			[]EventKind{BatchReturned, Red, BatchReturned, Red}, domain.PhaseBatchFailed},
		{"affected", st{Phase: domain.PhaseAffected, Batches: 2},
			[]EventKind{Red, FixesReturned, Red}, domain.PhaseAffectedFailed},
		{"fixes", st{Phase: domain.PhaseFixing},
			[]EventKind{FixesReturned, Red, FixesReturned, Red}, domain.PhaseFixesFailed},
	}
	for _, l := range loops {
		t.Run(l.name, func(t *testing.T) {
			s := l.start
			for i, k := range l.events {
				var err error
				if s, _, err = Next(s, Event{Kind: k}); err != nil {
					t.Fatalf("step %d (%v): %v", i, k, err)
				}
			}
			if s.Phase != l.want {
				t.Errorf("phase = %v, want %v", s.Phase, l.want)
			}
		})
	}
}

func TestDecisionContinueNotAllowedAfterPlannerFailed(t *testing.T) {
	s := st{Phase: domain.PhasePlannerFailed}
	got, _, err := Next(s, Event{Kind: Decided, Decision: DecisionContinue})
	if !errors.Is(err, ErrOutOfOrder) || !reflect.DeepEqual(got, s) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestStopFromEveryLivePhase(t *testing.T) {
	for _, p := range domain.Phases {
		if p == domain.PhaseDone {
			continue
		}
		s := st{Phase: p, Batch: 1, Batches: 2, Attempt: 1}
		got, acts, err := Next(s, Event{Kind: Stop})
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if got.Phase != domain.PhaseDone || got.Outcome != domain.OutcomeStopped {
			t.Errorf("%s: state = %+v", p, got)
		}
		if !reflect.DeepEqual(acts, acs(domain.Close)) {
			t.Errorf("%s: actions = %+v", p, acts)
		}
	}
}

// Every (phase, kind) pair outside the table must be rejected with the state
// untouched. The accepted pairs are the ones the table test above exercises
// plus Stop on live phases.
func TestOutOfOrderIsExhaustive(t *testing.T) {
	ok := map[domain.Phase][]EventKind{
		domain.PhaseNone:               {Requested},
		domain.PhasePlanning:           {PlanSaved, PlannerReturned, GapsFound},
		domain.PhaseDesigning:          {DesignClosed},
		domain.PhaseQuestions:          {Answered, ToEngineer},
		domain.PhaseNoTests:            {NoTestsAnswered},
		domain.PhaseApproval:           {Approved, ChangeRequested, ViewFull},
		domain.PhaseChangeNote:         {ChangeNoted},
		domain.PhasePlannerFailed:      {Decided},
		domain.PhaseRepoClash:          {ClashAnswered, Answered},
		domain.PhaseBranch:             {BranchReady},
		domain.PhaseRunningBatch:       {BatchReturned, Launched, Relaunch, Green, Red},
		domain.PhaseVerifyingBatch:     {Green, Red},
		domain.PhaseBatchFailed:        {Decided},
		domain.PhaseAffected:           {Green, Red, Unverified},
		domain.PhaseAffectedUnverified: {Decided},
		domain.PhaseAffectedFix:        {FixesReturned},
		domain.PhaseAffectedFailed:     {Decided},
		domain.PhaseReviewing:          {ReviewDone},
		domain.PhaseRefuting:           {RefuterDone},
		domain.PhaseChoosingFindings:   {Chosen},
		domain.PhaseFixing:             {FixesReturned},
		domain.PhaseFixVerify:          {Green, Red, Unverified},
		domain.PhaseFixesFailed:        {Decided},
		domain.PhaseDocsCheck:          {DocsChecked},
		domain.PhaseDocs:               {DocsAnswered},
		domain.PhaseDocumenting:        {DocumenterReturned},
		domain.PhaseRules:              {RulesAnswered},
	}
	phases := append([]domain.Phase{domain.PhaseNone}, domain.Phases...)
	for _, p := range phases {
		allowed := map[EventKind]bool{}
		for _, k := range ok[p] {
			allowed[k] = true
		}
		if p != domain.PhaseNone && p != domain.PhaseDone {
			allowed[Stop] = true
		}
		for _, k := range Kinds {
			if allowed[k] {
				continue
			}
			s := st{Phase: p, Batch: 1, Batches: 2}
			got, acts, err := Next(s, Event{Kind: k, Decision: DecisionRetry, Findings: true})
			if !errors.Is(err, ErrOutOfOrder) {
				t.Errorf("%q + %s: err = %v", p, k, err)
			}
			if !reflect.DeepEqual(got, s) || acts != nil {
				t.Errorf("%q + %s: state/actions changed: %+v %+v", p, k, got, acts)
			}
		}
	}
}

func TestDoneAcceptsNothing(t *testing.T) {
	for _, o := range []domain.Outcome{domain.OutcomeSuccess, domain.OutcomeStopped} {
		s := st{Phase: domain.PhaseDone, Outcome: o}
		for _, k := range Kinds {
			if got, _, err := Next(s, Event{Kind: k, Decision: DecisionRetry}); !errors.Is(err, ErrOutOfOrder) || !reflect.DeepEqual(got, s) {
				t.Errorf("done/%s + %s: %+v, %v", o, k, got, err)
			}
		}
	}
}
