package claudecode

import (
	"strings"

	"github.com/mananos/nxy.dev/internal/ask"
	"github.com/mananos/nxy.dev/internal/domain"
)

// ProposalInstruction shows the pending decision and asks the user to save it.
func ProposalInstruction(f domain.Feature) string {
	a := ask.DesignProposal(f)
	return verbatimAndAsk("nxy: feature "+f.ID+" — ", a.Title, a.Body, askInput(askInputOf(a.Questions)))
}

// CloseInstruction shows the design summary and asks the user to close it.
func CloseInstruction(f domain.Feature) string {
	a := ask.DesignClose(f)
	return verbatimAndAsk("nxy: feature "+f.ID+" — ", a.Title, a.Body, askInput(askInputOf(a.Questions)))
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
