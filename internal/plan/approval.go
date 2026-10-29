package plan

import "fmt"

// Labels of the approval question's options.
const (
	Approve  = "Approve"
	Change   = "Change"
	ViewFull = "Ver completo"
)

// ApprovalQuestion is the exact text of the approval question. It carries the
// feature id and plan revision so an old answer cannot approve a newer plan.
func ApprovalQuestion(featureID string, rev int) string {
	return fmt.Sprintf("Approve nxy plan %s r%d?", featureID, rev)
}
