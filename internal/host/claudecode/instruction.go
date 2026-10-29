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
	var s string
	switch a := f.Next; a.Kind {
	case domain.DispatchPlanner:
		s = fmt.Sprintf("nxy: feature %s — Next: Agent tool, subagent_type `nxy:planner`, prompt `feature %s`; nothing else, nxy writes its prompt.", f.ID, f.ID)
	case domain.AskQuestions:
		s = "nxy: feature " + f.ID + " — " + checkpointAndAsk(f, questionsInput(f))
	case domain.AskApproval:
		s = "nxy: feature " + f.ID + " — " + checkpointAndAsk(f, approvalInput(f))
	case domain.AskChangeNote:
		s = "nxy: feature " + f.ID + " — Next: tell the user to write what to change in the plan in their next message; do nothing else until they do."
	case domain.DispatchImplementer:
		s = fmt.Sprintf("nxy: feature %s — Lote %d aprobado; la ejecución llega en F2.2: stop here. To end the feature: `nxy feature stop`.", f.ID, a.Batch)
	default:
		s = "nxy: feature " + f.ID + " — " + a.Label() + ": not available yet in this build; stop here. To end the feature: `nxy feature stop`."
	}
	if note != "" {
		s += "\n" + note
	}
	return s
}

func checkpointAndAsk(f domain.Feature, input string) string {
	var b strings.Builder
	b.WriteString("Next: show this checkpoint to the user verbatim, without summarizing or reformatting.\n")
	b.WriteString("--- checkpoint ---\n")
	if f.Plan != nil {
		b.WriteString(plan.Checkpoint(*f.Plan))
	}
	b.WriteString("\n--- end checkpoint ---\n")
	b.WriteString("Then ask with AskUserQuestion, with exactly this input:\n")
	b.WriteString(input)
	return b.String()
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
