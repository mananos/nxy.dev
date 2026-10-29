package flow

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
	Decided         EventKind = "decided"
	ReviewDone      EventKind = "review_done"
	RefuterDone     EventKind = "refuter_done"
	Chosen          EventKind = "chosen"
	FixesReturned   EventKind = "fixes_returned"
	Stop            EventKind = "stop"
)

// Kinds lists every event kind.
var Kinds = []EventKind{
	Requested, PlanSaved, PlannerReturned, Answered, NoTestsAnswered, Approved,
	ChangeRequested, ChangeNoted, ViewFull, BatchReturned, Green, Red, Decided,
	ReviewDone, RefuterDone, Chosen, FixesReturned, Stop,
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
	Questions    bool     // PlanSaved: the plan has questions for the user
	NoTests      bool     // PlanSaved: the plan has rules without tests to confirm
	Batches      int      // PlanSaved: number of batches in the plan
	HasNote      bool     // ChangeRequested: the change note came with the answer
	Decision     Decision // Decided
	Findings     bool     // ReviewDone, RefuterDone, Chosen: there are findings left
	HighUnproven bool     // ReviewDone: high findings still unproven
}
