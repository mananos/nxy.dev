package plan

import "fmt"

// Labels of the decision questions' options.
const (
	Retry    = "Reintentar"
	Continue = "Seguir"
	Stop     = "Parar"
)

// DecisionQuestion is the exact text asked when a batch failed twice.
func DecisionQuestion(featureID string, batch int) string {
	return fmt.Sprintf("nxy feature %s: batch %d failed twice — what now?", featureID, batch)
}

// AffectedFailedQuestion is the exact text asked when the affected tests failed twice.
func AffectedFailedQuestion(featureID string) string {
	return fmt.Sprintf("nxy feature %s: the affected tests failed twice — what now?", featureID)
}

// UnverifiedQuestion is the exact text asked when the affected tests could not be verified.
func UnverifiedQuestion(featureID string) string {
	return fmt.Sprintf("nxy feature %s: the affected tests could not be verified — what now?", featureID)
}

// PlannerFailedQuestion is the exact text asked when the planner ended without saving a plan.
func PlannerFailedQuestion(featureID string) string {
	return fmt.Sprintf("nxy feature %s: the planner did not save a plan — what now?", featureID)
}
