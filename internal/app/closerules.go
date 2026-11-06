package app

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
	"github.com/mananos/nxy.dev/internal/memory"
	"github.com/mananos/nxy.dev/internal/plan"
)

// maxRuleCandidates is the most rule candidates one feature offers when it closes.
const maxRuleCandidates = 16

// ruleCandidates are the conventions a feature may leave as rules of the repo: the repo decisions
// of its plan (of its design in engineer mode) plus the candidates raised while it ran. A candidate
// without a question is left out (a rule needs it); equal texts count once.
func ruleCandidates(f domain.Feature) []domain.RuleCandidate {
	var all []domain.RuleCandidate
	if f.Mode == domain.ModeEngineer && f.Design != nil {
		for _, d := range f.Design.Decisions {
			if d.Scope != domain.ScopeRepo {
				continue
			}
			q := strings.TrimSpace(d.Why)
			for _, fk := range f.Design.Agenda {
				if fk.ID == d.Fork && strings.TrimSpace(fk.Title) != "" {
					q = strings.TrimSpace(fk.Title)
				}
			}
			all = append(all, domain.RuleCandidate{Text: d.Text, Question: q, Origin: memory.OriginEngineer})
		}
		if f.Plan != nil {
			for _, d := range f.Plan.Decisions {
				if d.Scope == domain.ScopeRepo && d.By == domain.ByPlanner {
					all = append(all, domain.RuleCandidate{Text: d.Text, Question: d.Question, Origin: memory.OriginPlan, Repo: d.Repo})
				}
			}
		}
	} else if f.Plan != nil {
		for _, d := range f.Plan.Decisions {
			if d.Scope == domain.ScopeRepo {
				all = append(all, domain.RuleCandidate{Text: d.Text, Question: d.Question, Origin: memory.OriginPlan, Repo: d.Repo})
			}
		}
	}
	all = append(all, f.Candidates...)
	var out []domain.RuleCandidate
	seen := map[string]bool{}
	for _, c := range all {
		c.Text, c.Question = strings.TrimSpace(c.Text), strings.TrimSpace(c.Question)
		key := memory.NormalizeText(c.Text)
		if key == "" || c.Question == "" || seen[key] {
			continue
		}
		seen[key] = true
		c.ID = ""
		out = append(out, c)
		if len(out) == maxRuleCandidates {
			break
		}
	}
	return out
}

// candidateKey is the memory key of the repo a rule candidate belongs to: the one it names, else the
// first repo of the feature.
func (h Hooks) candidateKey(f domain.Feature, c domain.RuleCandidate) string {
	if h.Repos == nil {
		return ""
	}
	mrs := h.memRepos(f)
	if i := memRepoIndex(mrs, c.Repo); i >= 0 {
		return mrs[i].Key
	}
	return mrs[0].Key
}

// hasCandidates is whether the closing question has something to ask; without memory it never has.
func (h Hooks) hasCandidates(f domain.Feature) bool {
	return h.Memory != nil && h.Repos != nil && f.ProjectDir != "" && len(ruleCandidates(f)) > 0
}

// withCandidates marks the DocumenterReturned events with whether there are rule candidates.
func (h Hooks) withCandidates(f *domain.Feature, evs []flow.Event) []flow.Event {
	for i := range evs {
		if evs[i].Kind == flow.DocumenterReturned {
			evs[i].Candidates = h.hasCandidates(*f)
		}
	}
	return evs
}

// ensureRules builds the offer of a feature in the rules phase that has none (the process died
// between the transition and its preparation); any hook that reads the feature calls it.
func (h Hooks) ensureRules(ctx context.Context, f domain.Feature) domain.Feature {
	if f.State.Phase != domain.PhaseRules || f.RuleOffer != nil {
		return f
	}
	offer := ruleCandidates(f)
	for i := range offer {
		offer[i].ID = "S" + strconv.Itoa(i+1)
		repo := h.candidateKey(f, offer[i])
		if h.Memory == nil || repo == "" {
			continue
		}
		if sim, err := h.Memory.SimilarRules(ctx, repo, offer[i].Text, 1); err == nil && len(sim) > 0 {
			offer[i].Supersedes, offer[i].SupersedesText = sim[0].Short, sim[0].Text
		}
	}
	if nf, ok, err := h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
		if g.State.Phase != domain.PhaseRules || g.RuleOffer != nil {
			return false
		}
		g.RuleOffer = offer
		return true
	}); err == nil && ok {
		f = nf
	}
	return f
}

// answerRules turns the answers to the rules question into the saved rules and RulesAnswered. An
// answer with free text and no id asks again.
func (h Hooks) answerRules(ctx context.Context, f domain.Feature, answers map[string]string) HookResponse {
	prefix := plan.RulesQuestionPrefix(f.ID)
	shown := plan.RuleShown(f.RuleOffer)
	seen, free := false, false
	picked := map[string]bool{}
	for q, a := range answers {
		if !strings.HasPrefix(q, prefix) {
			continue
		}
		seen = true
		ids := plan.ChosenRuleIDs(a, shown)
		for _, id := range ids {
			picked[id] = true
		}
		if len(ids) == 0 && strings.TrimSpace(a) != "" && !plan.IsNoneRules(a) {
			free = true
		}
	}
	if !seen {
		return HookResponse{}
	}
	if free {
		return HookResponse{Next: &Next{Feature: f, Note: "pick rules by their S number (or Ninguna); free text is not an answer"}}
	}
	if f.State.Phase != domain.PhaseRules {
		return HookResponse{Next: &Next{Feature: f}}
	}
	// Advance first: the rules are saved only by the answer that really moved the phase.
	moved := false
	nf, _, err := h.advanceWith(ctx, f.ID, flow.Event{Kind: flow.RulesAnswered}, func(x *domain.Feature) error {
		if x.State.Phase != domain.PhaseRules {
			return errNoChange
		}
		moved = true
		return nil
	})
	if err != nil {
		return HookResponse{Next: &Next{Feature: f, Note: "nxy could not record your answer; ask again"}}
	}
	if !moved {
		return HookResponse{Next: &Next{Feature: f}}
	}
	if h.Memory != nil && h.Repos != nil {
		liveByRepo := map[string]map[string]bool{}
		for _, c := range shown {
			repo := h.candidateKey(f, c)
			key := memory.NormalizeText(c.Text)
			live := liveByRepo[repo]
			if live == nil {
				live = map[string]bool{}
				liveByRepo[repo] = live
				if rules, err := h.Memory.Rules(ctx, repo); err == nil {
					for _, r := range rules {
						live[memory.NormalizeText(r.Text)] = true
					}
				}
			}
			if !picked[c.ID] || live[key] {
				continue
			}
			origin := c.Origin
			if origin == "" {
				origin = memory.OriginChat
			}
			r := memory.Rule{
				Repo: repo, Text: c.Text, Question: c.Question, Origin: origin,
				FeatureID: f.ID, Supersedes: c.Supersedes,
			}
			if _, err := h.Memory.AddRule(ctx, r); err != nil && r.Supersedes != "" {
				// The rule it replaced is gone: keep the new one without replacing anything.
				r.Supersedes = ""
				_, _ = h.Memory.AddRule(ctx, r)
			}
			live[key] = true
		}
	}
	return HookResponse{Next: &Next{Feature: nf}}
}

// AddRuleCandidate raises a rule candidate on the active feature of dir: it is offered in the
// question of the closing, never saved before. It returns ErrNoActiveFeature when there is none.
func (h Hooks) AddRuleCandidate(ctx context.Context, dir, text, question, origin string) (domain.Feature, error) {
	return h.addRuleCandidate(ctx, dir, text, question, origin, "")
}

// errRulesClosed: the feature is already asking (or past) the closing question, so a new candidate
// can no longer be offered.
var errRulesClosed = errors.New("the closing question is already open: use nxy mem rule")

func (h Hooks) addRuleCandidate(ctx context.Context, dir, text, question, origin, lens string) (domain.Feature, error) {
	f, ok, err := h.activeIn(ctx, dir)
	if err != nil {
		return domain.Feature{}, err
	}
	if !ok {
		return domain.Feature{}, ErrNoActiveFeature
	}
	if origin == "" {
		origin = memory.OriginChat
	}
	if err := memory.ValidateRule(memory.Rule{Text: text, Question: question, Origin: origin}); err != nil {
		return domain.Feature{}, err
	}
	key := memory.NormalizeText(text)
	nf, _, err := h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
		if g.State.Phase == domain.PhaseRules || g.State.Phase == domain.PhaseDone {
			return false
		}
		for _, c := range g.Candidates {
			if memory.NormalizeText(c.Text) == key {
				return false
			}
		}
		g.Candidates = append(append([]domain.RuleCandidate(nil), g.Candidates...),
			domain.RuleCandidate{Text: strings.TrimSpace(text), Question: strings.TrimSpace(question), Origin: origin, Lens: lens})
		return true
	})
	if err != nil {
		return domain.Feature{}, err
	}
	if nf.State.Phase == domain.PhaseRules || nf.State.Phase == domain.PhaseDone {
		return nf, errRulesClosed
	}
	return nf, nil
}

// deleteFeatureNotes drops the temporary notes of a feature that ended (done or stopped); failing
// never stops the close.
func (h Hooks) deleteFeatureNotes(ctx context.Context, featureID string) {
	if h.Memory == nil || featureID == "" {
		return
	}
	_, _ = h.Memory.DeleteNotesOfFeature(ctx, featureID)
}

// saveClosedFeature keeps what is left of a feature that closed well: its title, the decisions of
// the task, the files it changed and the findings the user chose to fix. Failing to keep it never
// stops the close.
func (h Hooks) saveClosedFeature(ctx context.Context, f domain.Feature) {
	if h.Memory == nil || h.Repos == nil || f.Plan == nil || f.ProjectDir == "" {
		return
	}
	mrs := h.memRepos(f)
	cf := memory.ClosedFeature{Meta: memory.Meta{ID: f.ID}, Title: f.Plan.Title, Repos: h.memKeys(f)}
	for _, d := range f.Plan.Decisions {
		if d.Scope == domain.ScopeTask && strings.TrimSpace(d.Text) != "" {
			cf.Decisions = append(cf.Decisions, d.Text)
		}
	}
	for _, p := range h.changedPaths(ctx, f) {
		i, rel := h.splitLabel(f, mrs, p)
		if i < 0 {
			i = 0
		}
		cf.Files = append(cf.Files, memory.FileRef{Repo: mrs[i].Key, Path: rel})
	}
	if f.Review != nil {
		chosen := map[string]bool{}
		for _, id := range f.Review.Chosen {
			chosen[id] = true
		}
		for _, fd := range f.Review.Findings {
			if chosen[fd.ID] {
				cf.Findings = append(cf.Findings, fd.ID+" "+fd.Title)
			}
		}
	}
	_, _ = h.Memory.SaveClosedFeature(ctx, cf)
}
