package claudecode

import (
	"fmt"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/plan"
)

type askOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type askQuestion struct {
	Question    string      `json:"question"`
	Header      string      `json:"header"`
	MultiSelect bool        `json:"multiSelect"`
	Options     []askOption `json:"options"`
}

// Instruction words the step f.Next as one instruction for the main thread, starting with "nxy:".
func Instruction(f domain.Feature, note string) string {
	return instruction(f, note, false)
}

// StopInstruction is Instruction for the Stop hook: its text is the block reason, which the user
// already sees, so the checkpoint or failure goes once and the model is told not to repeat it.
func StopInstruction(f domain.Feature, note string) string {
	return instruction(f, note, true)
}

func instruction(f domain.Feature, note string, stop bool) string {
	var s string
	switch a := f.Next; a.Kind {
	case domain.DispatchPlanner:
		s = fmt.Sprintf("nxy: feature %s — Next: Agent tool, subagent_type `nxy:planner`, prompt `feature %s`; nothing else, nxy writes its prompt.", f.ID, f.ID)
	case domain.AskQuestions:
		s = "nxy: feature " + f.ID + " — " + checkpointAndAsk(f, questionsInput(f), stop)
	case domain.AskApproval:
		s = "nxy: feature " + f.ID + " — " + checkpointAndAsk(f, approvalInput(f), stop)
	case domain.AskChangeNote:
		s = "nxy: feature " + f.ID + " — Next: tell the user to write what to change in the plan in their next message; do nothing else until they do."
	case domain.DispatchImplementer:
		if a.Retry {
			s = fmt.Sprintf("nxy: feature %s — batch %d failed verification; nxy retries once — dispatch it again the same way: Agent tool, subagent_type `nxy:implementer`, prompt `feature %s batch %d`; nothing else, nxy writes its prompt; it may run in the background.", f.ID, a.Batch, f.ID, a.Batch)
		} else {
			s = fmt.Sprintf("nxy: feature %s — Next: Agent tool, subagent_type `nxy:implementer`, prompt `feature %s batch %d`; nothing else, nxy writes its prompt; it may run in the background.", f.ID, f.ID, a.Batch)
		}
	case domain.AskDecision:
		s = "nxy: feature " + f.ID + " — " + decisionAndAsk(f, stop)
	default:
		if f.State.Phase == domain.PhaseAffected {
			s = "nxy: feature " + f.ID + " — all batches are green; affected tests and review arrive in F2.3: stop here. To end the feature: `nxy feature stop`."
		} else {
			s = "nxy: feature " + f.ID + " — " + a.Label() + ": not available yet in this build; stop here. To end the feature: `nxy feature stop`."
		}
	}
	if note != "" {
		s += "\n" + note
	}
	return s
}

func checkpointAndAsk(f domain.Feature, input string, stop bool) string {
	var b strings.Builder
	if stop {
		b.WriteString("Next: the user already sees this above: do not repeat it; ask with AskUserQuestion, with exactly this input.\n")
	} else {
		b.WriteString("Next: show this checkpoint to the user verbatim, without summarizing or reformatting.\n")
	}
	b.WriteString("--- checkpoint ---\n")
	if f.Plan != nil {
		b.WriteString(plan.Checkpoint(*f.Plan))
	}
	b.WriteString("\n--- end checkpoint ---\n")
	if stop {
		b.WriteString("Input:\n")
	} else {
		b.WriteString("Then ask with AskUserQuestion, with exactly this input:\n")
	}
	b.WriteString(input)
	return b.String()
}

// decisionAndAsk words the AskDecision step: the failure, then the question to ask.
func decisionAndAsk(f domain.Feature, stop bool) string {
	var b strings.Builder
	var input string
	switch f.State.Phase {
	case domain.PhaseBatchFailed:
		if stop {
			b.WriteString("Next: the user already sees this above: do not repeat it; ask with AskUserQuestion, with exactly this input.\n")
		} else {
			b.WriteString("Next: show this failure to the user verbatim, without summarizing or reformatting.\n")
		}
		fmt.Fprintf(&b, "--- Lote %d fallido ---\n", f.State.Batch)
		if f.Proof != nil {
			b.WriteString(f.Proof.Failure)
		}
		b.WriteString("\n--- fin ---\n")
		input = decisionInput(plan.DecisionQuestion(f.ID, f.State.Batch), "Batch", []askOption{
			{plan.Retry, "Run the batch again"},
			{plan.Continue, "Accept it as is and go to the next batch"},
			{plan.Stop, "End the feature"},
		})
	case domain.PhasePlannerFailed:
		b.WriteString("Next: the planner ended without saving a plan; ask with AskUserQuestion, with exactly this input.\n")
		input = decisionInput(plan.PlannerFailedQuestion(f.ID), "Planner", []askOption{
			{plan.Retry, "Plan again"},
			{plan.Stop, "End the feature"},
		})
	default:
		return f.Next.Label() + ": not available yet in this build; stop here. To end the feature: `nxy feature stop`."
	}
	if f.State.Phase == domain.PhaseBatchFailed {
		if stop {
			b.WriteString("Input:\n")
		} else {
			b.WriteString("Then ask with AskUserQuestion, with exactly this input:\n")
		}
	}
	b.WriteString(input)
	return b.String()
}

func decisionInput(question, header string, opts []askOption) string {
	return askInput([]askQuestion{{Question: question, Header: header, Options: opts}})
}

func approvalInput(f domain.Feature) string {
	return askInput([]askQuestion{{
		Question: plan.ApprovalQuestion(f.ID, f.PlanRev),
		Header:   "Plan",
		Options: []askOption{
			{plan.Approve, "Start executing batch 1"},
			{plan.Change, "Write what to change; nxy re-plans"},
			{plan.ViewFull, "Open the full plan in your editor"},
		},
	}})
}

func questionsInput(f domain.Feature) string {
	var qs []askQuestion
	if f.Plan != nil {
		answered := map[string]bool{}
		for _, a := range f.Answers {
			answered[a.Question] = true
		}
		qs = pendingQuestions(f.Plan.Questions, answered)
		if len(qs) == 0 { // nothing left unanswered: ask them all again
			qs = pendingQuestions(f.Plan.Questions, nil)
		}
	}
	return askInput(qs)
}

func pendingQuestions(all []domain.Question, answered map[string]bool) []askQuestion {
	var qs []askQuestion
	for _, q := range all {
		if answered[q.Question] || len(qs) == 4 {
			continue
		}
		aq := askQuestion{Question: q.Question, Header: q.Header}
		for _, o := range q.Options {
			aq.Options = append(aq.Options, askOption{o.Label, o.Description})
		}
		qs = append(qs, aq)
	}
	return qs
}

func askInput(qs []askQuestion) string {
	b, err := marshal(map[string]any{"questions": qs})
	if err != nil {
		return "{}"
	}
	return string(b)
}
