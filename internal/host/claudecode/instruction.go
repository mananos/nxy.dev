package claudecode

import (
	"fmt"
	"strings"

	"github.com/mananos/nxy.dev/internal/design"
	"github.com/mananos/nxy.dev/internal/docs"
	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/plan"
	"github.com/mananos/nxy.dev/internal/review"
)

// reviewerInstruction words the DispatchReviewer step: every pending lens in one response,
// or the single quick reviewer, or the wait when everything is already out.
func reviewerInstruction(f domain.Feature) string {
	var rv domain.Review
	if f.Review != nil {
		rv = *f.Review
	}
	if rv.Quick {
		if len(rv.Lenses) > 0 && (rv.Lenses[0].Dispatched || rv.Lenses[0].Saved) {
			return "nxy: feature " + f.ID + " — the reviewer is running; nothing else to do now: stop here, nxy tells you when it is done."
		}
		lens := "base"
		if len(rv.Lenses) > 0 {
			lens = rv.Lenses[0].Name
		}
		return fmt.Sprintf("nxy: feature %s — Next: Agent tool, subagent_type `nxy:reviewer-quick`, prompt `feature %s review %s`; nothing else, nxy writes its prompt; it may run in the background.", f.ID, f.ID, lens)
	}
	var pending []string
	for _, l := range rv.Lenses {
		if !l.Dispatched && !l.Saved {
			pending = append(pending, l.Name)
		}
	}
	if len(pending) == 0 {
		return "nxy: feature " + f.ID + " — the reviewers are running; nothing else to do now: stop here, nxy tells you when they are done."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "nxy: feature %s — Next: %d Agent tool calls in ONE single response (in parallel), all with subagent_type `nxy:reviewer`, one per lens, with these prompts; nothing else, nxy writes each prompt; they may run in the background:", f.ID, len(pending))
	for _, name := range pending {
		fmt.Fprintf(&b, "\n- `feature %s review %s`", f.ID, name)
	}
	return b.String()
}

// findingsAndAsk words the AskFindings step: the review checkpoint, then the multiSelect questions.
func findingsAndAsk(f domain.Feature, stop bool) string {
	var b strings.Builder
	if stop {
		b.WriteString("Next: the user already sees this above: do not repeat it; ask with AskUserQuestion, with exactly this input.\n")
	} else {
		b.WriteString("Next: show this checkpoint to the user verbatim, without summarizing or reformatting.\n")
	}
	b.WriteString("--- checkpoint ---\n" + review.Checkpoint(f) + "\n--- end checkpoint ---\n")
	if stop {
		b.WriteString("Input:\n")
	} else {
		b.WriteString("Then ask with AskUserQuestion, with exactly this input:\n")
	}
	var offered []domain.Finding
	if f.Review != nil {
		offered = review.Offered(*f.Review)
	}
	groups, left := review.Groups(offered)
	qs := make([]askQuestion, 0, len(groups))
	for i, g := range groups {
		q := askQuestion{Question: review.FixQuestion(f.ID, i+1, len(groups)), Header: g.Header, MultiSelect: true}
		for _, o := range g.Options {
			q.Options = append(q.Options, askOption{o.Label, o.Description})
		}
		qs = append(qs, q)
	}
	b.WriteString(askInput(qs))
	if left > 0 {
		fmt.Fprintf(&b, "\nQuedan %d hallazgos fuera de las preguntas (no caben): tell the user so after asking.", left)
	}
	return b.String()
}

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
	return instruction(f, note, "", false)
}

// InstructionFor is Instruction with the path of the nxy binary, for the commands it writes.
func InstructionFor(f domain.Feature, note, exe string) string {
	return instruction(f, note, exe, false)
}

// StopInstruction is Instruction for the Stop hook: its text is the block reason, which the user
// already sees, so the checkpoint or failure goes once and the model is told not to repeat it.
func StopInstruction(f domain.Feature, note, exe string) string {
	return instruction(f, note, exe, true)
}

func instruction(f domain.Feature, note, exe string, stop bool) string {
	if exe == "" {
		exe = "nxy"
	}
	var s string
	a := f.Next
	if f.State.Phase == domain.PhaseDone && f.State.Outcome == domain.OutcomeSuccess {
		a.Kind = domain.Close // Close is not a main-thread action, so Next never keeps it
	}
	switch a.Kind {
	case domain.DesignDebate:
		s = "nxy: feature " + f.ID + " — " + design.Guide(f, exe)
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
	case domain.RunAffected:
		if f.State.Phase == domain.PhaseFixVerify {
			s = fmt.Sprintf("nxy: feature %s — the fixes came back; Next: run exactly `\"%s\" affected --feature %s` as its own Bash call with timeout 600000, not in the background; nxy runs the affected tests again and prints the next step.", f.ID, exe, f.ID)
		} else {
			s = fmt.Sprintf("nxy: feature %s — all batches are green; Next: run exactly `\"%s\" affected --feature %s` as its own Bash call with timeout 600000, not in the background; nxy picks and runs the affected tests and prints the next step.", f.ID, exe, f.ID)
		}
	case domain.DispatchAffectedFix:
		lead := "Next"
		if a.Retry {
			lead = "the affected fix did not pass the check; nxy retries once — dispatch it again the same way"
		}
		s = fmt.Sprintf("nxy: feature %s — %s: Agent tool, subagent_type `nxy:implementer`, prompt `feature %s affected fix`; nothing else, nxy writes its prompt; it may run in the background.", f.ID, lead, f.ID)
	case domain.DispatchReviewer:
		s = reviewerInstruction(f)
	case domain.DispatchRefuter:
		s = fmt.Sprintf("nxy: feature %s — the review found high-severity findings; Next: Agent tool, subagent_type `nxy:refuter`, prompt `feature %s refute`; nothing else, nxy writes its prompt; it may run in the background.", f.ID, f.ID)
	case domain.AskFindings:
		s = "nxy: feature " + f.ID + " — " + findingsAndAsk(f, stop)
	case domain.DispatchFixer:
		lead := "Next"
		if a.Retry {
			lead = "the fixes did not pass the check; nxy retries once — dispatch it again the same way"
		}
		s = fmt.Sprintf("nxy: feature %s — %s: ONE Agent tool call, subagent_type `nxy:implementer`, prompt `feature %s review fix`; nothing else, nxy writes its prompt; it may run in the background.", f.ID, lead, f.ID)
	case domain.AskDocs:
		s = "nxy: feature " + f.ID + " — " + docsAsk(f, stop)
	case domain.AskRules:
		s = "nxy: feature " + f.ID + " — " + rulesAsk(f, stop)
	case domain.DispatchDocumenter:
		s = fmt.Sprintf("nxy: feature %s — Next: Agent tool, subagent_type `nxy:documenter`, prompt `feature %s docs`; nothing else, nxy writes its prompt; it may run in the background.", f.ID, f.ID)
	case domain.Close:
		if f.State.Outcome == domain.OutcomeSuccess {
			s = fmt.Sprintf("nxy: feature %s — Listo. Next: tell the user this line verbatim, without summarizing or reformatting, and stop here:\n%s", f.ID, review.Summary(f))
		} else {
			s = "nxy: feature " + f.ID + " — " + f.State.Outcome.Label() + ": nothing else to do; stop here."
		}
	default:
		s = "nxy: feature " + f.ID + " — " + a.Label() + ": nothing for the main thread; stop here. To end the feature: `nxy feature stop`."
	}
	if note != "" {
		s += "\n" + note
	}
	return s
}

// docsAsk words the AskDocs step: the question about the docs that name the change, copied as is.
func docsAsk(f domain.Feature, stop bool) string {
	var paths []string
	if f.Docs != nil {
		paths = f.Docs.Paths()
	}
	q := docs.Question(f.ID, paths)
	opts := make([]askOption, len(q.Options))
	for i, o := range q.Options {
		opts[i] = askOption{o.Label, o.Description}
	}
	var b strings.Builder
	b.WriteString("Next: the review is done and some docs of the repo name what changed; ask the user with AskUserQuestion, copying the question and its options as they are, with exactly this input.\n")
	if stop {
		b.WriteString("Input:\n")
	} else {
		b.WriteString("Then nothing else: nxy continues with the answer. Input:\n")
	}
	b.WriteString(askInput([]askQuestion{{Question: q.Question, Header: q.Header, Options: opts}}))
	return b.String()
}

// rulesAsk words the AskRules step: the multiSelect questions with the rule candidates, copied as
// they are. Stopping the feature instead saves no rule.
func rulesAsk(f domain.Feature, stop bool) string {
	groups := plan.RuleGroups(f.RuleOffer)
	qs := make([]askQuestion, 0, len(groups))
	for i, g := range groups {
		q := askQuestion{Question: plan.RulesQuestion(f.ID, i+1, len(groups)), Header: g.Header, MultiSelect: true}
		for _, o := range g.Options {
			q.Options = append(q.Options, askOption{o.Label, o.Description})
		}
		qs = append(qs, q)
	}
	var b strings.Builder
	b.WriteString("Next: the feature is done and some of its decisions may become rules of the repo; ask the user with AskUserQuestion, copying the questions and their options as they are, with exactly this input.\n")
	if stop {
		b.WriteString("Input:\n")
	} else {
		b.WriteString("Then nothing else: nxy saves the rules the user picks and closes the feature. Input:\n")
	}
	b.WriteString(askInput(qs))
	if n := len(f.RuleOffer) - len(plan.RuleShown(f.RuleOffer)); n > 0 {
		fmt.Fprintf(&b, "\nQuedan %d candidatas fuera de las preguntas (no caben): tell the user so after asking.", n)
	}
	return b.String()
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
		b.WriteString(plan.CheckpointOf(f))
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
	case domain.PhaseAffectedFailed, domain.PhaseAffectedUnverified:
		if stop {
			b.WriteString("Next: the user already sees this above: do not repeat it; ask with AskUserQuestion, with exactly this input.\n")
		} else {
			b.WriteString("Next: show this to the user verbatim, without summarizing or reformatting.\n")
		}
		failed := f.State.Phase == domain.PhaseAffectedFailed
		body := ""
		if f.Affected != nil {
			body = strings.TrimSpace(f.Affected.Reason + "\n" + f.Affected.Note)
			if failed {
				body = f.Affected.Failure
			}
		}
		if failed {
			b.WriteString("--- Afectados fallidos ---\n" + body + "\n--- fin ---\n")
		} else {
			b.WriteString("--- Afectados sin verificar ---\n" + body + "\n--- fin ---\n")
		}
		if stop {
			b.WriteString("Input:\n")
		} else {
			b.WriteString("Then ask with AskUserQuestion, with exactly this input:\n")
		}
		if failed {
			input = decisionInput(plan.AffectedFailedQuestion(f.ID), "Afectados", []askOption{
				{plan.Retry, "Dispatch the implementer to fix them again"},
				{plan.Continue, "Accept them failing and go on to review"},
				{plan.Stop, "End the feature"},
			})
		} else {
			input = decisionInput(plan.UnverifiedQuestion(f.ID), "Afectados", []askOption{
				{plan.Retry, "Run nxy affected again (e.g. after adding .nxy/runners.json or suite)"},
				{plan.Continue, "Go on without verifying"},
				{plan.Stop, "End the feature"},
			})
		}
	case domain.PhaseFixesFailed:
		if stop {
			b.WriteString("Next: the user already sees this above: do not repeat it; ask with AskUserQuestion, with exactly this input.\n")
		} else {
			b.WriteString("Next: show this to the user verbatim, without summarizing or reformatting.\n")
		}
		body := ""
		if f.Affected != nil {
			body = strings.TrimSpace(f.Affected.Failure + "\n" + f.Affected.Reason + "\n" + f.Affected.Note)
		}
		b.WriteString("--- Arreglos fallidos ---\n" + body + "\n--- fin ---\n")
		if stop {
			b.WriteString("Input:\n")
		} else {
			b.WriteString("Then ask with AskUserQuestion, with exactly this input:\n")
		}
		input = decisionInput(plan.FixesFailedQuestion(f.ID), "Arreglos", []askOption{
			{plan.Retry, "Dispatch the implementer to fix them again"},
			{plan.Continue, "Accept it as is and finish"},
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
		offer := f.Mode != domain.ModeEngineer && !f.OfferAnswered && len(f.Plan.Questions) > 0
		if offer {
			qs = append(qs, askQuestion{
				Question: design.OfferQuestion(f.ID),
				Header:   "Engineer",
				Options: []askOption{
					{design.LabelToEngineer, "Decide these with Claude, fork by fork, before planning"},
					{design.LabelAnswerHere, "Answer the questions here"},
				},
			})
		}
		qs = append(qs, pendingQuestions(f.Plan.Questions, answered, 4-len(qs))...)
		if len(qs) == 0 || (offer && len(qs) == 1) { // nothing left unanswered: ask them all again
			qs = append(qs, pendingQuestions(f.Plan.Questions, nil, 4-len(qs))...)
		}
	}
	return askInput(qs)
}

func pendingQuestions(all []domain.Question, answered map[string]bool, max int) []askQuestion {
	var qs []askQuestion
	for _, q := range all {
		if answered[q.Question] || len(qs) >= max {
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
