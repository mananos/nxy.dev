package claudecode

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mananos/nxy.dev/internal/app"
	"github.com/mananos/nxy.dev/internal/ask"
	"github.com/mananos/nxy.dev/internal/design"
	"github.com/mananos/nxy.dev/internal/domain"
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
	a := askOf(f)
	b.WriteString("--- checkpoint ---\n" + a.Body + "\n--- end checkpoint ---\n")
	if stop {
		b.WriteString("Input:\n")
	} else {
		b.WriteString("Then ask with AskUserQuestion, with exactly this input:\n")
	}
	b.WriteString(askInput(askInputOf(a.Questions)))
	if a.Left > 0 {
		fmt.Fprintf(&b, "\nQuedan %d hallazgos fuera de las preguntas (no caben): tell the user so after asking.", a.Left)
	}
	return b.String()
}

// askOf is the ask.Ask of a step that is always a question.
func askOf(f domain.Feature) ask.Ask {
	a, _ := ask.For(f)
	return a
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
		s = "nxy: feature " + f.ID + " — " + checkpointAndAsk(askOf(f), stop)
	case domain.AskClash:
		s = "nxy: feature " + f.ID + " — " + clashAsk(f, stop)
	case domain.AskBranch:
		s = "nxy: feature " + f.ID + " — " + branchAsk(f, stop)
	case domain.AskApproval:
		s = "nxy: feature " + f.ID + " — " + checkpointAndAsk(askOf(f), stop)
	case domain.AskChangeNote:
		s = "nxy: feature " + f.ID + " — Next: tell the user to write what to change in the plan in their next message; do nothing else until they do."
	case domain.DispatchImplementer:
		others := ""
		if running := f.State.Running(); len(running) > 0 {
			var up []string
			for _, n := range running {
				if !slices.Contains(f.State.Unlaunched(), n) {
					up = append(up, strconv.Itoa(n))
				}
			}
			if len(up) > 0 {
				others = " Batch " + strings.Join(up, ", ") + " is already running: do not dispatch it again."
			}
		}
		if len(a.Batches) > 1 {
			var prompts []string
			for _, n := range a.Batches {
				prompts = append(prompts, fmt.Sprintf("`feature %s batch %d`", f.ID, n))
			}
			s = fmt.Sprintf("nxy: feature %s — Next: in ONE message, %d Agent tool calls, one per batch, so they run in parallel (each works in its own repo): subagent_type `nxy:implementer`, prompts %s; nothing else, nxy writes their prompts; they may run in the background.%s", f.ID, len(a.Batches), strings.Join(prompts, ", "), others)
		} else if a.Retry {
			s = fmt.Sprintf("nxy: feature %s — batch %d failed verification; nxy retries once — dispatch it again the same way: Agent tool, subagent_type `nxy:implementer`, prompt `feature %s batch %d`; nothing else, nxy writes its prompt; it may run in the background.%s", f.ID, a.Batch, f.ID, a.Batch, others)
		} else {
			s = fmt.Sprintf("nxy: feature %s — Next: Agent tool, subagent_type `nxy:implementer`, prompt `feature %s batch %d`; nothing else, nxy writes its prompt; it may run in the background.%s", f.ID, f.ID, a.Batch, others)
		}
	case domain.AwaitBatches:
		ns := a.Batches
		if len(ns) == 0 {
			ns = []int{a.Batch}
		}
		var names []string
		for _, n := range ns {
			names = append(names, strconv.Itoa(n))
		}
		s = fmt.Sprintf("nxy: feature %s — batch %s running; wait for it, do nothing else and do not dispatch it again: nxy brings each result when it ends.", f.ID, strings.Join(names, ", "))
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

// clashAsk words the AskClash step: which repos are held by which feature, then the question with
// its options copied as they are. The plan is kept; the answer moves it on without another planner.
func clashAsk(f domain.Feature, stop bool) string {
	var b strings.Builder
	a := askOf(f)
	b.WriteString("Next: the plan is ready but a repo it needs is held by another active feature:")
	for _, c := range a.Context {
		b.WriteString("\n- " + c)
	}
	if stop {
		b.WriteString("\nThe user already sees this above: do not repeat it; ask with AskUserQuestion, copying the question and its options as they are, with exactly this input.\nInput:\n")
	} else {
		b.WriteString("\nAsk the user with AskUserQuestion, copying the question and its options as they are, with exactly this input.\nThen nothing else: nxy continues with the answer. Input:\n")
	}
	b.WriteString(askInput(askInputOf(a.Questions)))
	return b.String()
}

// branchAsk words the AskBranch step: the plan is approved; the questions open are the branch and the
// base of each repo, copied as they are. Free text in Other is a branch (or base) name.
func branchAsk(f domain.Feature, stop bool) string {
	a := askOf(f)
	var b strings.Builder
	b.WriteString("Next: the plan is approved; before the first batch ask which branch to work in")
	if len(a.Context) > 0 {
		b.WriteString(". The last try failed: " + a.Context[0] + ". Tell the user that in one line and ask again")
	}
	if stop {
		b.WriteString(". The user already sees this above: do not repeat it; ask with AskUserQuestion, copying the questions and their options as they are, with exactly this input.\nInput:\n")
	} else {
		b.WriteString(". Ask with AskUserQuestion, copying the questions and their options as they are, with exactly this input.\nThen nothing else: nxy creates the branch and continues with the answer. Input:\n")
	}
	b.WriteString(askInput(askInputOf(a.Questions)))
	return b.String()
}

// docsAsk words the AskDocs step: the question about the docs that name the change, copied as is.
func docsAsk(f domain.Feature, stop bool) string {
	a := askOf(f)
	var b strings.Builder
	b.WriteString("Next: the review is done and some docs of the repo name what changed; ask the user with AskUserQuestion, copying the question and its options as they are, with exactly this input.\n")
	if stop {
		b.WriteString("Input:\n")
	} else {
		b.WriteString("Then nothing else: nxy continues with the answer. Input:\n")
	}
	b.WriteString(askInput(askInputOf(a.Questions)))
	return b.String()
}

// rulesAsk words the AskRules step: the multiSelect questions with the rule candidates, copied as
// they are. Stopping the feature instead saves no rule.
func rulesAsk(f domain.Feature, stop bool) string {
	a := askOf(f)
	var b strings.Builder
	b.WriteString("Next: the feature is done and some of its decisions may become rules of the repo; ask the user with AskUserQuestion, copying the questions and their options as they are, with exactly this input.\n")
	if stop {
		b.WriteString("Input:\n")
	} else {
		b.WriteString("Then nothing else: nxy saves the rules the user picks and closes the feature. Input:\n")
	}
	b.WriteString(askInput(askInputOf(a.Questions)))
	if a.Left > 0 {
		fmt.Fprintf(&b, "\nQuedan %d candidatas fuera de las preguntas (no caben): tell the user so after asking.", a.Left)
	}
	return b.String()
}

// checkpointAndAsk words the AskQuestions and AskApproval steps: the plan checkpoint, then the questions.
func checkpointAndAsk(a ask.Ask, stop bool) string {
	var b strings.Builder
	if stop {
		b.WriteString("Next: the user already sees this above: do not repeat it; ask with AskUserQuestion, with exactly this input.\n")
	} else {
		b.WriteString("Next: show this checkpoint to the user verbatim, without summarizing or reformatting.\n")
	}
	b.WriteString("--- checkpoint ---\n")
	b.WriteString(a.Body)
	b.WriteString("\n--- end checkpoint ---\n")
	if stop {
		b.WriteString("Input:\n")
	} else {
		b.WriteString("Then ask with AskUserQuestion, with exactly this input:\n")
	}
	b.WriteString(askInput(askInputOf(a.Questions)))
	return b.String()
}

// decisionAndAsk words the AskDecision step: the failure, then the question to ask.
func decisionAndAsk(f domain.Feature, stop bool) string {
	a, ok := ask.For(f)
	if !ok {
		return f.Next.Label() + ": not available yet in this build; stop here. To end the feature: `nxy feature stop`."
	}
	var b strings.Builder
	if a.Kind == ask.KindPlannerFailed {
		b.WriteString("Next: the planner ended without saving a plan; ask with AskUserQuestion, with exactly this input.\n")
		b.WriteString(askInput(askInputOf(a.Questions)))
		return b.String()
	}
	if stop {
		b.WriteString("Next: the user already sees this above: do not repeat it; ask with AskUserQuestion, with exactly this input.\n")
	} else if a.Kind == ask.KindBatchFailed {
		b.WriteString("Next: show this failure to the user verbatim, without summarizing or reformatting.\n")
	} else {
		b.WriteString("Next: show this to the user verbatim, without summarizing or reformatting.\n")
	}
	b.WriteString("--- " + a.Title + " ---\n" + a.Body + "\n--- fin ---\n")
	if stop {
		b.WriteString("Input:\n")
	} else {
		b.WriteString("Then ask with AskUserQuestion, with exactly this input:\n")
	}
	b.WriteString(askInput(askInputOf(a.Questions)))
	return b.String()
}

// askInputOf maps the questions of an ask.Ask to the JSON structs of AskUserQuestion; Recommended
// and Argv are not part of it. A nil list stays nil so the JSON keeps null where it had it.
func askInputOf(qs []ask.Question) []askQuestion {
	if qs == nil {
		return nil
	}
	out := make([]askQuestion, len(qs))
	for i, q := range qs {
		out[i] = askQuestion{Question: q.Question, Header: q.Header, MultiSelect: q.MultiSelect}
		if q.Options != nil {
			out[i].Options = make([]askOption, len(q.Options))
			for j, o := range q.Options {
				out[i].Options[j] = askOption{o.Label, o.Description}
			}
		}
	}
	return out
}

// askInstruction words an app.AskSpec as an instruction to ask it with AskUserQuestion.
func askInstruction(a app.AskSpec) string {
	var qs []askQuestion
	for _, s := range append([]app.AskSpec{a}, a.More...) {
		q := askQuestion{Question: s.Question, Header: s.Header, MultiSelect: s.MultiSelect}
		for _, o := range s.Options {
			q.Options = append(q.Options, askOption{o.Label, o.Description})
		}
		qs = append(qs, q)
	}
	after := a.After
	if after == "" {
		after = "and then carry on with their message using the answer"
	}
	return "nxy: before anything else, ask the user with AskUserQuestion, with exactly this input, " +
		after + ":\n" + askInput(qs)
}

func askInput(qs []askQuestion) string {
	b, err := marshal(map[string]any{"questions": qs})
	if err != nil {
		return "{}"
	}
	return string(b)
}
