package claudecode

import (
	"strings"

	"github.com/mananos/nxy.dev/internal/design"
	"github.com/mananos/nxy.dev/internal/domain"
)

// ProposalInstruction shows the pending decision and asks the user to save it.
func ProposalInstruction(f domain.Feature) string {
	n := 1
	if f.Design != nil {
		n = len(f.Design.Decisions) + 1
	}
	body := design.Proposal(f.Design)
	input := askInput([]askQuestion{{
		Question: design.ProposeQuestion(f.ID, n),
		Header:   "Diseño",
		Options: []askOption{
			{design.LabelSave, "Save the decision in the design"},
			{design.LabelKeepDebating, "Do not save it; keep debating"},
		},
	}})
	return verbatimAndAsk("nxy: feature "+f.ID+" — ", "decision", body, input)
}

// CloseInstruction shows the design summary and asks the user to close it.
func CloseInstruction(f domain.Feature) string {
	input := askInput([]askQuestion{{
		Question: design.CloseQuestion(f.ID),
		Header:   "Diseño",
		Options: []askOption{
			{design.LabelClose, "Approve the rules and plan the batches"},
			{design.LabelKeepDesigning, "Do not close; keep designing"},
		},
	}})
	return verbatimAndAsk("nxy: feature "+f.ID+" — ", "design summary", design.CloseSummary(f), input)
}

func verbatimAndAsk(lead, what, body, input string) string {
	var b strings.Builder
	b.WriteString(lead)
	b.WriteString("Next: show this " + what + " to the user verbatim, without summarizing or reformatting.\n")
	b.WriteString("--- " + what + " ---\n")
	b.WriteString(body)
	b.WriteString("\n--- end " + what + " ---\n")
	b.WriteString("Then ask with AskUserQuestion, with exactly this input:\n")
	b.WriteString(input)
	return b.String()
}
