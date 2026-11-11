package ask

import (
	"fmt"
	"strings"

	"github.com/mananos/nxy.dev/internal/design"
	"github.com/mananos/nxy.dev/internal/docs"
	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/plan"
	"github.com/mananos/nxy.dev/internal/review"
)

// For is the question f.Next asks the user, or false when the step is not a question (a dispatch,
// a wait, AskNoTests) or its decision phase is not one nxy words yet.
func For(f domain.Feature) (Ask, bool) {
	a := Ask{FeatureID: f.ID}
	switch f.Next.Kind {
	case domain.AskQuestions:
		a.Kind = KindQuestions
		if f.Plan != nil {
			a.Body = plan.CheckpointOf(f)
		}
		a.Questions = questions(f)
	case domain.AskApproval:
		a.Kind = KindApproval
		if f.Plan != nil {
			a.Body = plan.CheckpointOf(f)
		}
		a.Questions = []Question{{
			Question: plan.ApprovalQuestion(f.ID, f.PlanRev),
			Header:   "Plan",
			Options: []Option{
				{Label: plan.Approve, Description: "Start executing batch 1"},
				{Label: plan.Change, Description: "Write what to change; nxy re-plans"},
				{Label: plan.ViewFull, Description: "Open the full plan in your editor"},
			},
		}}
	case domain.AskChangeNote:
		a.Kind = KindChangeNote
	case domain.AskClash:
		a.Kind = KindRepoClash
		for _, c := range f.Clash {
			line := fmt.Sprintf("repo %s: feature %s", c.Repo, c.FeatureID)
			if c.Phase != "" {
				line += fmt.Sprintf(" (%s)", c.Phase)
			}
			a.Context = append(a.Context, line)
		}
		opts := plan.ClashOptions(f)
		ao := make([]Option, len(opts))
		for i, o := range opts {
			ao[i] = fromDomain(o)
		}
		a.Questions = []Question{{Question: plan.ClashQuestion(f.ID), Header: "Repo ocupado", Options: ao}}
	case domain.AskBranch:
		a.Kind = KindBranch
		if f.BranchOffer != nil && f.BranchOffer.Error != "" {
			a.Context = []string{f.BranchOffer.Error}
		}
		asks := plan.BranchQuestions(f)
		qs := make([]Question, 0, len(asks))
		for _, b := range asks {
			q := Question{Question: b.Question, Header: b.Header}
			for _, o := range b.Options {
				q.Options = append(q.Options, fromDomain(o))
			}
			qs = append(qs, q)
		}
		a.Questions = qs
	case domain.AskDecision:
		if !decision(f, &a) {
			return Ask{}, false
		}
	case domain.AskFindings:
		a.Kind = KindFindings
		a.Body = review.Checkpoint(f)
		var offered []domain.Finding
		if f.Review != nil {
			offered = review.Offered(*f.Review)
		}
		groups, left := review.Groups(offered)
		qs := make([]Question, 0, len(groups))
		for i, g := range groups {
			q := Question{Question: review.FixQuestion(f.ID, i+1, len(groups)), Header: g.Header, MultiSelect: true}
			for _, o := range g.Options {
				q.Options = append(q.Options, Option{Label: o.Label, Description: o.Description})
			}
			qs = append(qs, q)
		}
		a.Questions = qs
		a.Left = left
	case domain.AskDocs:
		a.Kind = KindDocs
		var paths []string
		if f.Docs != nil {
			paths = f.Docs.Paths()
		}
		q := docs.Question(f.ID, paths)
		opts := make([]Option, len(q.Options))
		for i, o := range q.Options {
			opts[i] = Option{Label: o.Label, Description: o.Description}
		}
		a.Questions = []Question{{Question: q.Question, Header: q.Header, Options: opts}}
	case domain.AskRules:
		a.Kind = KindRules
		groups := plan.RuleGroups(f.RuleOffer)
		qs := make([]Question, 0, len(groups))
		for i, g := range groups {
			q := Question{Question: plan.RulesQuestion(f.ID, i+1, len(groups)), Header: g.Header, MultiSelect: true}
			for _, o := range g.Options {
				q.Options = append(q.Options, fromDomain(o))
			}
			qs = append(qs, q)
		}
		a.Questions = qs
		a.Left = len(f.RuleOffer) - len(plan.RuleShown(f.RuleOffer))
	default:
		return Ask{}, false
	}
	return a, true
}

func fromDomain(o domain.Option) Option {
	return Option{Label: o.Label, Description: o.Description, Recommended: o.Recommended}
}

// decision fills a for the failure the flow is stopped at; false for a phase it does not word.
func decision(f domain.Feature, a *Ask) bool {
	switch f.State.Phase {
	case domain.PhaseBatchFailed:
		a.Kind = KindBatchFailed
		a.Title = fmt.Sprintf("Lote %d fallido", f.State.Batch)
		a.Body = f.ProofOf(f.State.Batch).Failure
		a.Questions = one(plan.DecisionQuestion(f.ID, f.State.Batch), "Batch",
			Option{Label: plan.Retry, Description: "Run the batch again"},
			Option{Label: plan.Continue, Description: "Accept it as is and go to the next batch"},
			Option{Label: plan.Stop, Description: "End the feature"})
	case domain.PhaseAffectedFailed:
		a.Kind = KindAffectedFailed
		a.Title = "Afectados fallidos"
		if f.Affected != nil {
			a.Body = f.Affected.Failure
		}
		a.Questions = one(plan.AffectedFailedQuestion(f.ID), "Afectados",
			Option{Label: plan.Retry, Description: "Dispatch the implementer to fix them again"},
			Option{Label: plan.Continue, Description: "Accept them failing and go on to review"},
			Option{Label: plan.Stop, Description: "End the feature"})
	case domain.PhaseAffectedUnverified:
		a.Kind = KindAffectedUnverified
		a.Title = "Afectados sin verificar"
		if f.Affected != nil {
			a.Body = strings.TrimSpace(f.Affected.Reason + "\n" + f.Affected.Note)
		}
		a.Questions = one(plan.UnverifiedQuestion(f.ID), "Afectados",
			Option{Label: plan.Retry, Description: "Run nxy affected again (e.g. after adding .nxy/runners.json or suite)"},
			Option{Label: plan.Continue, Description: "Go on without verifying"},
			Option{Label: plan.Stop, Description: "End the feature"})
	case domain.PhaseFixesFailed:
		a.Kind = KindFixesFailed
		a.Title = "Arreglos fallidos"
		if f.Affected != nil {
			a.Body = strings.TrimSpace(f.Affected.Failure + "\n" + f.Affected.Reason + "\n" + f.Affected.Note)
		}
		a.Questions = one(plan.FixesFailedQuestion(f.ID), "Arreglos",
			Option{Label: plan.Retry, Description: "Dispatch the implementer to fix them again"},
			Option{Label: plan.Continue, Description: "Accept it as is and finish"},
			Option{Label: plan.Stop, Description: "End the feature"})
	case domain.PhasePlannerFailed:
		a.Kind = KindPlannerFailed
		a.Questions = one(plan.PlannerFailedQuestion(f.ID), "Planner",
			Option{Label: plan.Retry, Description: "Plan again"},
			Option{Label: plan.Stop, Description: "End the feature"})
	default:
		return false
	}
	return true
}

func one(question, header string, opts ...Option) []Question {
	return []Question{{Question: question, Header: header, Options: opts}}
}

// questions are the questions of the plan still open: the Engineer offer first, then the
// unanswered ones, four at most; when none is left they are all asked again.
func questions(f domain.Feature) []Question {
	var qs []Question
	if f.Plan == nil {
		return qs
	}
	answered := map[string]bool{}
	for _, a := range f.Answers {
		answered[a.Question] = true
	}
	offer := f.Mode != domain.ModeEngineer && !f.OfferAnswered && len(f.Plan.Questions) > 0
	if offer {
		qs = append(qs, Question{
			Question: design.OfferQuestion(f.ID),
			Header:   "Engineer",
			Options: []Option{
				{Label: design.LabelToEngineer, Description: "Decide these with Claude, fork by fork, before planning"},
				{Label: design.LabelAnswerHere, Description: "Answer the questions here"},
			},
		})
	}
	qs = append(qs, pending(f.Plan.Questions, answered, 4-len(qs))...)
	if len(qs) == 0 || (offer && len(qs) == 1) { // nothing left unanswered: ask them all again
		qs = append(qs, pending(f.Plan.Questions, nil, 4-len(qs))...)
	}
	return qs
}

func pending(all []domain.Question, answered map[string]bool, max int) []Question {
	var qs []Question
	for _, q := range all {
		if answered[q.Question] || len(qs) >= max {
			continue
		}
		aq := Question{Question: q.Question, Header: q.Header}
		for _, o := range q.Options {
			aq.Options = append(aq.Options, fromDomain(o))
		}
		qs = append(qs, aq)
	}
	return qs
}

// DesignProposal is the question to save the pending decision of the design.
func DesignProposal(f domain.Feature) Ask {
	n := 1
	if f.Design != nil {
		n = len(f.Design.Decisions) + 1
	}
	return Ask{
		Kind:      KindDesignProposal,
		FeatureID: f.ID,
		Title:     "decision",
		Body:      design.Proposal(f.Design),
		Questions: one(design.ProposeQuestion(f.ID, n), "Diseño",
			Option{Label: design.LabelSave, Description: "Save the decision in the design"},
			Option{Label: design.LabelKeepDebating, Description: "Do not save it; keep debating"}),
	}
}

// DesignClose is the question to close the design.
func DesignClose(f domain.Feature) Ask {
	return Ask{
		Kind:      KindDesignClose,
		FeatureID: f.ID,
		Title:     "design summary",
		Body:      design.CloseSummary(f),
		Questions: one(design.CloseQuestion(f.ID), "Diseño",
			Option{Label: design.LabelClose, Description: "Approve the rules and plan the batches"},
			Option{Label: design.LabelKeepDesigning, Description: "Do not close; keep designing"}),
	}
}
