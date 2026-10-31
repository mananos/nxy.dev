package app

import (
	"context"
	"strconv"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
	"github.com/mananos/nxy.dev/internal/plan"
)

const toolAskUser = "AskUserQuestion"

// isNxyQuestion is true for the approval question of the feature (any revision) and for the
// questions of its plan.
func isNxyQuestion(f domain.Feature, q string) bool {
	if strings.HasPrefix(q, "Approve nxy plan "+f.ID+" r") {
		return true
	}
	if q == plan.DecisionQuestion(f.ID, f.State.Batch) || q == plan.PlannerFailedQuestion(f.ID) ||
		q == plan.AffectedFailedQuestion(f.ID) || q == plan.UnverifiedQuestion(f.ID) {
		return true
	}
	if f.Plan != nil {
		for _, pq := range f.Plan.Questions {
			if pq.Question == q {
				return true
			}
		}
	}
	return false
}

// decisionAnswer finds the answer to the decision question of the current phase.
func decisionAnswer(f domain.Feature, answers map[string]string) (string, bool) {
	var q string
	switch f.State.Phase {
	case domain.PhaseBatchFailed:
		q = plan.DecisionQuestion(f.ID, f.State.Batch)
	case domain.PhasePlannerFailed:
		q = plan.PlannerFailedQuestion(f.ID)
	case domain.PhaseAffectedFailed:
		q = plan.AffectedFailedQuestion(f.ID)
	case domain.PhaseAffectedUnverified:
		q = plan.UnverifiedQuestion(f.ID)
	default:
		return "", false
	}
	a, ok := answers[q]
	return a, ok
}

func (h Hooks) answerDecision(ctx context.Context, f domain.Feature, answer string) HookResponse {
	var d flow.Decision
	switch answer {
	case plan.Retry:
		d = flow.DecisionRetry
	case plan.Continue:
		switch f.State.Phase {
		case domain.PhaseBatchFailed, domain.PhaseAffectedFailed, domain.PhaseAffectedUnverified:
			d = flow.DecisionContinue
		}
	case plan.Stop:
		d = flow.DecisionStop
	}
	if d == flow.DecisionNone {
		return HookResponse{Next: &Next{Feature: f, Note: "pick one of the options"}}
	}
	nf, _, err := h.advance(ctx, f.ID, flow.Event{Kind: flow.Decided, Decision: d})
	if err != nil {
		return HookResponse{Next: &Next{Feature: f, Note: "nxy could not record your answer; ask again"}}
	}
	return HookResponse{Next: &Next{Feature: nf}}
}

// preToolAskUser refuses nxy questions that arrive with answers the model filled in.
func (h Hooks) preToolAskUser(ctx context.Context, ev HookEvent) HookResponse {
	if ev.AgentID != "" || !ev.InputHasAnswers {
		return HookResponse{}
	}
	f, ok := h.activeFeature(ctx, ev)
	if !ok {
		return HookResponse{}
	}
	for _, q := range ev.Questions {
		if isNxyQuestion(f, q) {
			return HookResponse{Deny: "nxy questions are answered by the user, not pre-filled"}
		}
	}
	return HookResponse{}
}

// postToolAskUser turns the user's answers to nxy's questions into flow events.
func (h Hooks) postToolAskUser(ctx context.Context, ev HookEvent) HookResponse {
	if ev.AgentID != "" || len(ev.Answers) == 0 {
		return HookResponse{}
	}
	f, ok := h.activeFeature(ctx, ev)
	if !ok {
		return HookResponse{}
	}
	if ans, rev, found := approvalAnswer(f, ev.Answers); found {
		return h.answerApproval(ctx, f, ans, rev)
	}
	if ans, found := decisionAnswer(f, ev.Answers); found {
		return h.answerDecision(ctx, f, ans)
	}
	if f.State.Phase == domain.PhaseQuestions && f.Plan != nil {
		return h.answerQuestions(ctx, f, ev.Answers)
	}
	return HookResponse{}
}

// approvalAnswer finds the answer to an approval question of this feature and its revision.
func approvalAnswer(f domain.Feature, answers map[string]string) (string, int, bool) {
	prefix := "Approve nxy plan " + f.ID + " r"
	for q, a := range answers {
		rest, ok := strings.CutPrefix(q, prefix)
		if !ok {
			continue
		}
		rev, err := strconv.Atoi(strings.TrimSuffix(rest, "?"))
		if err != nil {
			continue
		}
		return a, rev, true
	}
	return "", 0, false
}

func (h Hooks) answerApproval(ctx context.Context, f domain.Feature, answer string, rev int) HookResponse {
	if rev != f.PlanRev {
		return HookResponse{Next: &Next{Feature: f,
			Note: "that was plan r" + strconv.Itoa(rev) + "; current is r" + strconv.Itoa(f.PlanRev)}}
	}
	if f.State.Phase != domain.PhaseApproval {
		return HookResponse{Next: &Next{Feature: f}}
	}
	var nf domain.Feature
	var err error
	note := ""
	switch answer {
	case plan.Approve:
		nf, _, err = h.advanceWith(ctx, f.ID, flow.Event{Kind: flow.Approved}, func(x *domain.Feature) error {
			if x.Base == "" && h.Changes != nil {
				x.Base = h.Changes.Head(x.ProjectDir)
			}
			return nil
		})
	case plan.ViewFull:
		if h.Viewer != nil {
			path, serr := h.Viewer.Show(ctx, f)
			if serr != nil {
				note = "could not open the full plan: " + serr.Error()
				if path != "" {
					note += " (" + path + ")"
				}
			}
		}
		nf, _, err = h.advance(ctx, f.ID, flow.Event{Kind: flow.ViewFull})
	case plan.Change:
		nf, _, err = h.advance(ctx, f.ID, flow.Event{Kind: flow.ChangeRequested})
	default:
		nf, _, err = h.advanceWith(ctx, f.ID, flow.Event{Kind: flow.ChangeRequested, HasNote: true},
			func(x *domain.Feature) error {
				x.ChangeNotes = append(append([]string(nil), x.ChangeNotes...), answer)
				return nil
			})
	}
	if err != nil {
		return HookResponse{Next: &Next{Feature: f, Note: "nxy could not record your answer; ask again"}}
	}
	return HookResponse{Next: &Next{Feature: nf, Note: note}}
}

func (h Hooks) answerQuestions(ctx context.Context, f domain.Feature, answers map[string]string) HookResponse {
	merge := func(x *domain.Feature) error {
		if x.Plan == nil || x.State.Phase != domain.PhaseQuestions {
			return errSkipEvent
		}
		out := append([]domain.Answer(nil), x.Answers...)
		for _, pq := range x.Plan.Questions {
			a, ok := answers[pq.Question]
			if !ok {
				continue
			}
			set := false
			for i := range out {
				if out[i].Question == pq.Question {
					out[i].Answer, set = a, true
				}
			}
			if !set {
				out = append(out, domain.Answer{Question: pq.Question, Answer: a})
			}
		}
		x.Answers = out
		for _, pq := range x.Plan.Questions {
			found := false
			for _, a := range x.Answers {
				found = found || a.Question == pq.Question
			}
			if !found {
				return errSkipEvent // incomplete: keep the answers, wait for the rest
			}
		}
		return nil
	}
	// One transition: merge, and apply Answered only when every question has an answer.
	nf, _, err := h.advanceWith(ctx, f.ID, flow.Event{Kind: flow.Answered}, merge)
	if err != nil {
		return HookResponse{Next: &Next{Feature: f, Note: "nxy could not record your answer; ask again"}}
	}
	return HookResponse{Next: &Next{Feature: nf}}
}
