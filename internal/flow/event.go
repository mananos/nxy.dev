package flow

import "github.com/mananos/nxy.dev/internal/domain"

// EventKind is what happened to a feature.
type EventKind string

const (
	Requested       EventKind = "requested"
	PlanSaved       EventKind = "plan_saved"
	PlannerReturned EventKind = "planner_returned"
	Answered        EventKind = "answered"
	NoTestsAnswered EventKind = "no_tests_answered"
	Approved        EventKind = "approved"
	ChangeRequested EventKind = "change_requested"
	ChangeNoted     EventKind = "change_noted"
	ViewFull        EventKind = "view_full"
	BatchReturned   EventKind = "batch_returned"
	Green           EventKind = "green"
	Red             EventKind = "red"
	Unverified      EventKind = "unverified"
	Decided         EventKind = "decided"
	ReviewDone      EventKind = "review_done"
	RefuterDone     EventKind = "refuter_done"
	Chosen          EventKind = "chosen"
	FixesReturned   EventKind = "fixes_returned"
	Stop            EventKind = "stop"
	DesignClosed    EventKind = "design_closed"
	GapsFound       EventKind = "gaps_found"
	ToEngineer      EventKind = "to_engineer"
	ClashAnswered   EventKind = "clash_answered"
	BranchReady     EventKind = "branch_ready"
	Launched        EventKind = "launched" // the main thread launched the implementer of a batch
	Relaunch        EventKind = "relaunch" // a new process: the launched implementers were lost

	DocsChecked        EventKind = "docs_checked"
	DocsAnswered       EventKind = "docs_answered"
	DocumenterReturned EventKind = "documenter_returned"
	RulesAnswered      EventKind = "rules_answered"
)

// Kinds lists every event kind.
var Kinds = []EventKind{
	Requested, PlanSaved, PlannerReturned, Answered, NoTestsAnswered, Approved,
	ChangeRequested, ChangeNoted, ViewFull, BatchReturned, Green, Red, Unverified, Decided,
	ReviewDone, RefuterDone, Chosen, FixesReturned, Stop,
	DesignClosed, GapsFound, ToEngineer, DocsChecked, DocsAnswered, DocumenterReturned,
	RulesAnswered, ClashAnswered, BranchReady,
	Launched, Relaunch,
}

// Decision is what the user picks after a step failed.
type Decision string

const (
	DecisionNone     Decision = ""
	DecisionRetry    Decision = "retry"
	DecisionContinue Decision = "continue"
	DecisionStop     Decision = "stop"
)

// Event is a fact plus the few details the flow needs to branch on.
type Event struct {
	Kind         EventKind
	Engineer     bool          // Requested: the request came through /nxy:engineer
	Questions    bool          // PlanSaved, ClashAnswered: the plan has questions for the user
	NoTests      bool          // PlanSaved, ClashAnswered: the plan has rules without tests to confirm
	Batches      int           // PlanSaved, ClashAnswered: number of batches in the plan
	Batch        int           // BatchReturned, Green, Red, Launched, Decided in batch phases: the batch (0 = the one in focus)
	Steps        []domain.Step // PlanSaved, ClashAnswered: the schedule of the plan (empty = one batch after the other)
	Blocked      bool          // PlannerReturned: the plan was not saved because a repo is held by another feature
	HasNote      bool          // ChangeRequested: the change note came with the answer
	Decision     Decision      // Decided
	Findings     bool          // ReviewDone, RefuterDone, Chosen: there are findings left
	HighUnproven bool          // ReviewDone: high findings still unproven
	Docs         bool          // DocsChecked: there are docs that name the change
	Update       bool          // DocsAnswered: the user wants the docs updated
	Candidates   bool          // DocsChecked, DocsAnswered, DocumenterReturned: there are rule candidates to ask about
}
