package app

import (
	"context"
	"strconv"
	"strings"

	"github.com/mananos/nxy.dev/internal/design"
	"github.com/mananos/nxy.dev/internal/docs"
	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
	"github.com/mananos/nxy.dev/internal/plan"
	"github.com/mananos/nxy.dev/internal/repos"
	"github.com/mananos/nxy.dev/internal/review"
)

const toolAskUser = "AskUserQuestion"

// isNxyQuestion is true for the approval question of the feature (any revision) and for the
// questions of its plan.
func isNxyQuestion(f domain.Feature, q string) bool {
	if _, ok := plan.ApprovalRev(f.ID, q); ok {
		return true
	}
	if q == plan.DecisionQuestion(f.ID, f.State.Batch) || q == plan.PlannerFailedQuestion(f.ID) ||
		q == plan.AffectedFailedQuestion(f.ID) || q == plan.UnverifiedQuestion(f.ID) ||
		q == plan.FixesFailedQuestion(f.ID) || q == plan.ClashQuestion(f.ID) || strings.HasPrefix(q, review.FixQuestionPrefix(f.ID)) ||
		q == plan.BranchQuestion(f.ID) || strings.HasPrefix(q, plan.BasePrefix(f.ID)) {
		return true
	}
	if q == docs.QuestionText(f.ID) || strings.HasPrefix(q, plan.RulesQuestionPrefix(f.ID)) {
		return true
	}
	if q == design.CloseQuestion(f.ID) || q == design.OfferQuestion(f.ID) ||
		strings.HasPrefix(q, "nxy feature "+f.ID+": ¿guardar la decisión D") {
		return true
	}
	if f.Design != nil && f.Design.Current != "" && q == design.ForkQuestion(f.ID, f.Design.Current) {
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
	case domain.PhaseFixesFailed:
		q = plan.FixesFailedQuestion(f.ID)
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
		case domain.PhaseBatchFailed, domain.PhaseAffectedFailed, domain.PhaseAffectedUnverified, domain.PhaseFixesFailed:
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

// preToolAskUser refuses nxy questions that arrive with answers the model filled in, and holds the
// ones a live TUI can answer until the user answers there.
func (h Hooks) preToolAskUser(ctx context.Context, ev HookEvent) HookResponse {
	if ev.AgentID != "" {
		return HookResponse{}
	}
	if !ev.InputHasAnswers && (h.Asks == nil || h.TUI == nil) {
		return HookResponse{}
	}
	f, ok := h.activeFeature(ctx, ev)
	if !ok {
		return HookResponse{}
	}
	if ev.InputHasAnswers {
		for _, q := range ev.Questions {
			if isNxyQuestion(f, q) {
				return HookResponse{Deny: "nxy questions are answered by the user, not pre-filled"}
			}
		}
		return HookResponse{}
	}
	return h.waitInTUI(ctx, ev, f)
}

// postToolAskUser turns the user's answers to nxy's questions into flow events.
func (h Hooks) postToolAskUser(ctx context.Context, ev HookEvent) HookResponse {
	if ev.AgentID != "" || len(ev.Answers) == 0 {
		return HookResponse{}
	}
	if r, ok := h.answerLegacyPlan(ctx, ev); ok {
		return r
	}
	if r, ok := h.answerNotes(ctx, ev); ok {
		return r
	}
	if r, ok := h.answerBootstrap(ctx, ev); ok {
		return r
	}
	f, ok := h.activeFeature(ctx, ev)
	if !ok {
		return HookResponse{}
	}
	if ans, rev, found := approvalAnswer(f, ev.Answers); found {
		return h.answerApproval(ctx, f, ans, rev)
	}
	if ans, found := ev.Answers[plan.ClashQuestion(f.ID)]; found && f.State.Phase == domain.PhaseRepoClash {
		return h.answerClash(ctx, f, ans)
	}
	if f.State.Phase == domain.PhaseBranch && hasBranchAnswer(f, ev.Answers) {
		return h.answerBranch(ctx, f, ev.Answers)
	}
	if ans, found := decisionAnswer(f, ev.Answers); found {
		return h.answerDecision(ctx, f, ans)
	}
	if ans, found := ev.Answers[docs.QuestionText(f.ID)]; found && f.State.Phase == domain.PhaseDocs {
		return h.answerDocs(ctx, f, ans)
	}
	if f.State.Phase == domain.PhaseDesigning {
		return h.answerDesign(ctx, f, ev.Answers)
	}
	if f.State.Phase == domain.PhaseQuestions && f.Plan != nil {
		return h.answerQuestions(ctx, f, ev.Answers)
	}
	if f.State.Phase == domain.PhaseChoosingFindings && f.Review != nil {
		return h.answerFindings(ctx, f, ev.Answers)
	}
	if f.State.Phase == domain.PhaseRules {
		return h.answerRules(ctx, f, ev.Answers)
	}
	return HookResponse{}
}

// answerDocs turns the answer to the docs question into DocsAnswered; anything that is not one of
// the two options asks again.
func (h Hooks) answerDocs(ctx context.Context, f domain.Feature, answer string) HookResponse {
	var update bool
	switch strings.TrimSpace(answer) {
	case docs.LabelUpdate:
		update = true
	case docs.LabelLeave:
	default:
		return HookResponse{Next: &Next{Feature: f, Note: "pick one of the options"}}
	}
	nf, _, err := h.advanceSteps(ctx, f.ID, func(g *domain.Feature) ([]flow.Event, error) {
		// Candidates are read off the stored feature, which may have changed since the hook read it.
		return []flow.Event{{Kind: flow.DocsAnswered, Update: update, Candidates: h.hasCandidates(*g)}}, nil
	})
	if err != nil {
		return HookResponse{Next: &Next{Feature: f, Note: "nxy could not record your answer; ask again"}}
	}
	return HookResponse{Next: &Next{Feature: nf}}
}

// answerFindings reads which offered findings the user picked (the ids H<n> in every answer of
// the findings questions), moves the flow and records the picks. An answer with free text and no
// id asks again.
func (h Hooks) answerFindings(ctx context.Context, f domain.Feature, answers map[string]string) HookResponse {
	prefix := review.FixQuestionPrefix(f.ID)
	seen, free := false, false
	picked := map[string]bool{}
	for q, a := range answers {
		if !strings.HasPrefix(q, prefix) {
			continue
		}
		seen = true
		ids := review.ChosenIDs(a)
		for _, id := range ids {
			picked[id] = true
		}
		if len(ids) == 0 && strings.TrimSpace(a) != "" && !strings.Contains(strings.ToLower(a), strings.ToLower(review.NoneLabel)) {
			free = true
		}
	}
	if !seen {
		return HookResponse{}
	}
	if free {
		return HookResponse{Next: &Next{Feature: f, Note: "pick findings by their H number (or Ninguno); free text is not an answer"}}
	}
	// Only the findings that appeared in the question count as picked or ignored.
	offered, _ := review.Shown(review.Offered(*f.Review))
	var chosen []string
	var picks []domain.Pick
	for _, fd := range offered {
		picks = append(picks, domain.Pick{Finding: fd.ID, Lens: fd.Lens, Severity: fd.Severity, Chosen: picked[fd.ID]})
		if picked[fd.ID] {
			chosen = append(chosen, fd.ID)
		}
	}
	nf, _, err := h.advanceWith(ctx, f.ID, flow.Event{Kind: flow.Chosen, Findings: len(chosen) > 0}, func(x *domain.Feature) error {
		if x.State.Phase != domain.PhaseChoosingFindings || x.Review == nil {
			return errNoChange
		}
		x.Review.Chosen = chosen
		return nil
	})
	if err != nil {
		return HookResponse{Next: &Next{Feature: f, Note: "nxy could not record your answer; ask again"}}
	}
	if h.ReviewLog != nil && len(picks) > 0 {
		repoKey := f.Repo
		if len(f.Repos) > 0 && f.Repos[0].Key != "" {
			repoKey = f.Repos[0].Key
		}
		// Each pick goes with the repo of its finding's file; the first repo takes the rest.
		byRepo := map[string][]domain.Pick{}
		var order []string
		fileOf := map[string]string{}
		for _, fd := range offered {
			fileOf[fd.ID] = fd.File
		}
		for _, p := range picks {
			key := repoKey
			if len(f.Repos) > 1 {
				if name, _, ok := repos.Split(fileOf[p.Finding], featureRepoNames(f)); ok {
					if r, ok := f.RepoByName(name); ok && r.Key != "" {
						key = r.Key
					}
				}
			}
			if _, seen := byRepo[key]; !seen {
				order = append(order, key)
			}
			byRepo[key] = append(byRepo[key], p)
		}
		for _, key := range order {
			_ = h.ReviewLog.RecordPicks(ctx, f.ID, key, byRepo[key])
		}
	}
	return HookResponse{Next: &Next{Feature: nf}}
}

// approvalAnswer finds the answer to an approval question of this feature and its revision.
func approvalAnswer(f domain.Feature, answers map[string]string) (string, int, bool) {
	for q, a := range answers {
		rev, ok := plan.ApprovalRev(f.ID, q)
		if !ok {
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
		// The branch question is built from git before the transition; with nothing to ask the first
		// batch starts right away, from the HEAD of the folder.
		offer := h.branchOffer(ctx, f)
		nf, _, err = h.advanceSteps(ctx, f.ID, func(x *domain.Feature) ([]flow.Event, error) {
			x.BranchOffer = offer
			if offer != nil {
				return []flow.Event{{Kind: flow.Approved}}, nil
			}
			if x.Base == "" && h.Changes != nil {
				x.Base = h.Changes.Head(x.ProjectDir)
			}
			return []flow.Event{{Kind: flow.Approved}, {Kind: flow.BranchReady}}, nil
		})
	case plan.ViewFull:
		if h.Viewer != nil {
			path, serr := h.Viewer.Show(ctx, f)
			if serr != nil {
				note = "could not open the full plan: " + serr.Error()
				if path != "" {
					note += " (" + path + ")"
				}
			} else if path != "" {
				note = "the full plan was opened in the user's editor: " + path + ". Tell the user that in one line, then ask again."
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
	offer, offered := answers[design.OfferQuestion(f.ID)]
	if offered && strings.Contains(offer, design.LabelToEngineer) {
		nf, _, err := h.advanceWith(ctx, f.ID, flow.Event{Kind: flow.ToEngineer}, func(x *domain.Feature) error {
			if x.Plan == nil || x.State.Phase != domain.PhaseQuestions {
				return errNoChange
			}
			x.Mode = domain.ModeEngineer
			x.OfferAnswered = true
			if x.Design == nil {
				x.Design = &domain.Design{}
			}
			start := len(x.Design.Agenda)
			titles := make([]string, 0, len(x.Plan.Questions))
			for _, pq := range x.Plan.Questions {
				titles = append(titles, pq.Question)
			}
			design.AddForks(x.Design, titles, domain.SourceOffer)
			for i, pq := range x.Plan.Questions {
				for _, o := range pq.Options {
					x.Design.Agenda[start+i].Options = append(x.Design.Agenda[start+i].Options, domain.ForkOption{Label: o.Label})
				}
			}
			return nil
		})
		if err != nil {
			return HookResponse{Next: &Next{Feature: f, Note: "nxy could not record your answer; ask again"}}
		}
		return HookResponse{Next: &Next{Feature: nf, Note: h.engineerMemoryNote(ctx, nf)}}
	}
	merge := func(x *domain.Feature) error {
		if x.Plan == nil || x.State.Phase != domain.PhaseQuestions {
			return errSkipEvent
		}
		if offered {
			x.OfferAnswered = true
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
