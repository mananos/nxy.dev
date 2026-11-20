// Package ask builds, from the state of a feature, every question the main thread (and the TUI)
// puts to the user: its verbatim block, its short context lines and its AskUserQuestion questions.
// It is pure: the host words the instruction around it, views read it as data.
package ask

import "strings"

// Kind says which question an Ask is.
type Kind string

const (
	KindApproval           Kind = "approval"
	KindQuestions          Kind = "questions"
	KindChangeNote         Kind = "change_note"
	KindRepoClash          Kind = "repo_clash"
	KindBranch             Kind = "branch"
	KindBatchFailed        Kind = "batch_failed"
	KindAffectedFailed     Kind = "affected_failed"
	KindAffectedUnverified Kind = "affected_unverified"
	KindFixesFailed        Kind = "fixes_failed"
	KindPlannerFailed      Kind = "planner_failed"
	KindFindings           Kind = "findings"
	KindDocs               Kind = "docs"
	KindRules              Kind = "rules"
	KindDesignProposal     Kind = "design_proposal"
	KindDesignClose        Kind = "design_close"
)

// Option is one answer of a Question.
type Option struct {
	Label       string
	Description string
	Recommended bool
	// Argv is the binary command that answers this option; nil until an answer by feature id exists.
	Argv []string
}

// Question is one question of an AskUserQuestion call.
type Question struct {
	Question    string
	Header      string
	MultiSelect bool
	Options     []Option
}

// Ask is one question to the user about a feature.
type Ask struct {
	Kind      Kind
	FeatureID string
	// Title and Body are the block shown verbatim before the question (Title may be empty).
	Title string
	Body  string
	// Context are short lines: who holds a repo, why the last branch try failed.
	Context []string
	// Questions are the AskUserQuestion questions; nil when the step asks none.
	Questions []Question
	// Left counts the findings or rule candidates that did not fit in the questions.
	Left int
}

// Lines splits Body for a view.
func (a Ask) Lines() []string {
	if a.Body == "" {
		return nil
	}
	return strings.Split(a.Body, "\n")
}
