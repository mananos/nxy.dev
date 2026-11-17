package app

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/mananos/nxy.dev/internal/design"
	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
	"github.com/mananos/nxy.dev/internal/plan"
)

// DesignResult is the outcome of the `nxy design` commands: Problems when the input was
// refused (nothing stored), otherwise the stored Feature.
type DesignResult struct {
	Problems []plan.Problem
	Feature  domain.Feature
}

func notDesigning(f domain.Feature) error {
	return fmt.Errorf("feature %s is not designing (state %s); next step %s", f.ID, f.State.Phase, f.Next.Label())
}

// cloneDesign copies the design so a mutation never touches slices shared with the stored feature.
func cloneDesign(src *domain.Design) domain.Design {
	d := *src
	d.Agenda = append([]domain.Fork(nil), d.Agenda...)
	d.Decisions = append([]domain.Decision(nil), d.Decisions...)
	d.Refs = append([]string(nil), d.Refs...)
	return d
}

// mutateDesign applies fn to a copy of the design of a designing feature under the store's
// transaction; the copy is stored only when fn returns true. It fails with notDesigning when the
// feature is no longer designing inside the transaction.
func (h Hooks) mutateDesign(ctx context.Context, id string, fn func(d *domain.Design) bool) (domain.Feature, error) {
	var gone bool
	nf, _, err := h.mutate(ctx, id, func(g *domain.Feature) bool {
		if g.State.Phase != domain.PhaseDesigning || g.Design == nil {
			gone = true
			return false
		}
		d := cloneDesign(g.Design)
		if !fn(&d) {
			return false
		}
		g.Design = &d
		return true
	})
	if err == nil && gone {
		err = notDesigning(nf)
	}
	return nf, err
}

// designChange validates and applies one change of the design under the store's transaction.
func (h Hooks) designChange(ctx context.Context, id string, apply func(d *domain.Design) []plan.Problem) (DesignResult, error) {
	if h.Store == nil {
		return DesignResult{}, fmt.Errorf("no store")
	}
	var probs []plan.Problem
	nf, err := h.mutateDesign(ctx, id, func(d *domain.Design) bool {
		probs = apply(d)
		return len(probs) == 0
	})
	if err != nil {
		return DesignResult{}, err
	}
	if len(probs) > 0 {
		return DesignResult{Problems: probs}, nil
	}
	return DesignResult{Feature: nf}, nil
}

// SaveAgenda sets the open forks of the design from {"forks": [...]}.
func (h Hooks) SaveAgenda(ctx context.Context, id string, r io.Reader) (DesignResult, error) {
	titles, err := design.DecodeAgenda(r)
	if err != nil {
		return DesignResult{}, err
	}
	return h.designChange(ctx, id, func(d *domain.Design) []plan.Problem {
		return design.SetAgenda(d, titles)
	})
}

// SaveForkOptions stores the options and the opinion of one open fork.
func (h Hooks) SaveForkOptions(ctx context.Context, id string, r io.Reader) (DesignResult, error) {
	in, err := design.DecodeForkOptions(r)
	if err != nil {
		return DesignResult{}, err
	}
	return h.designChange(ctx, id, func(d *domain.Design) []plan.Problem {
		return design.SetForkOptions(d, in)
	})
}

// SaveToday stores the d2 of how the code is today from {"d2": "..."}.
func (h Hooks) SaveToday(ctx context.Context, id string, r io.Reader) (DesignResult, error) {
	src, err := design.DecodeToday(r)
	if err != nil {
		return DesignResult{}, err
	}
	return h.designChange(ctx, id, func(d *domain.Design) []plan.Problem {
		return design.SetToday(d, src)
	})
}

// ProposeDecision leaves a decision waiting for the user's OK.
func (h Hooks) ProposeDecision(ctx context.Context, id string, r io.Reader) (DesignResult, error) {
	p, err := design.DecodeProposal(r)
	if err != nil {
		return DesignResult{}, err
	}
	return h.designChange(ctx, id, func(d *domain.Design) []plan.Problem {
		if probs := design.ValidateProposal(d, p); len(probs) > 0 {
			return probs
		}
		design.Propose(d, p)
		return nil
	})
}

// ProposeClose leaves the close of the design waiting for the user's OK.
func (h Hooks) ProposeClose(ctx context.Context, id string, r io.Reader) (DesignResult, error) {
	c, err := design.DecodeClose(r)
	if err != nil {
		return DesignResult{}, err
	}
	return h.designChange(ctx, id, func(d *domain.Design) []plan.Problem {
		if probs := design.ValidateClose(d, c); len(probs) > 0 {
			return probs
		}
		d.Close = &c
		return nil
	})
}

// designRef keeps every free prompt of the user while designing, verbatim, as a reference for the
// planner. It does not consume the prompt.
func (h Hooks) designRef(ctx context.Context, ev HookEvent) {
	if strings.TrimSpace(ev.Prompt) == "" || strings.HasPrefix(ev.Prompt, "/") || h.Store == nil || h.Repos == nil {
		return
	}
	f, ok := h.activeFeature(ctx, ev)
	if !ok || f.State.Phase != domain.PhaseDesigning {
		return
	}
	_, _ = h.mutateDesign(ctx, f.ID, func(d *domain.Design) bool {
		d.Refs = append(d.Refs, ev.Prompt)
		return true
	})
}

// answerDesign turns the user's answer to the save/close questions of the design into the db.
func (h Hooks) answerDesign(ctx context.Context, f domain.Feature, answers map[string]string) HookResponse {
	if f.Design == nil {
		return HookResponse{}
	}
	if f.Design.Pending != nil {
		if a, ok := answers[design.ProposeQuestion(f.ID, len(f.Design.Decisions)+1)]; ok {
			return h.answerProposal(ctx, f, a)
		}
	}
	if f.Design.Close != nil {
		if a, ok := answers[design.CloseQuestion(f.ID)]; ok {
			return h.answerClose(ctx, f, a)
		}
	}
	if fork := f.Design.Current; fork != "" {
		if a, ok := answers[design.ForkQuestion(f.ID, fork)]; ok {
			return answerFork(f.Design, fork, a)
		}
	}
	return HookResponse{}
}

// answerFork tells the main thread which option of the fork the user picked, or what they said.
func answerFork(d *domain.Design, fork, answer string) HookResponse {
	a := strings.TrimSpace(answer)
	for _, f := range d.Agenda {
		if f.ID != fork {
			continue
		}
		for _, o := range f.Options {
			if o.Label == a {
				return HookResponse{Context: fmt.Sprintf("The user chose %s for %s: propose the decision with design propose.", a, fork)}
			}
		}
	}
	return HookResponse{Context: "The user said: " + a + "; keep debating."}
}

// freeText is the user's own words in an answer, or "" when it is one of the options.
func freeText(answer string, labels ...string) string {
	a := strings.TrimSpace(answer)
	for _, l := range labels {
		if a == l {
			return ""
		}
	}
	return a
}

func (h Hooks) answerProposal(ctx context.Context, f domain.Feature, answer string) HookResponse {
	save := strings.TrimSpace(answer) == design.LabelSave
	var saved bool
	nf, err := h.mutateDesign(ctx, f.ID, func(d *domain.Design) bool {
		if d.Pending == nil {
			return false
		}
		if save {
			_, saved = design.Approve(d)
		} else {
			design.Discard(d)
		}
		return true
	})
	if err != nil {
		return HookResponse{Next: &Next{Feature: f, Note: "nxy could not record your answer; ask again"}}
	}
	if save && saved {
		return HookResponse{Context: "The decision is saved. " + design.Status(nf)}
	}
	ctxLine := "The proposal was not saved: keep debating."
	if t := freeText(answer, design.LabelSave, design.LabelKeepDebating); t != "" {
		ctxLine += " The user said: " + t
	}
	return HookResponse{Context: ctxLine + " " + design.Status(nf)}
}

func (h Hooks) answerClose(ctx context.Context, f domain.Feature, answer string) HookResponse {
	if strings.TrimSpace(answer) != design.LabelClose {
		nf, err := h.mutateDesign(ctx, f.ID, func(d *domain.Design) bool {
			if d.Close == nil {
				return false
			}
			d.Close = nil
			return true
		})
		if err != nil {
			return HookResponse{Next: &Next{Feature: f, Note: "nxy could not record your answer; ask again"}}
		}
		ctxLine := "The design stays open: keep designing."
		if t := freeText(answer, design.LabelKeepDesigning); t != "" {
			ctxLine += " The user said: " + t
		}
		return HookResponse{Context: ctxLine + " " + design.Status(nf)}
	}
	nf, _, err := h.advanceWith(ctx, f.ID, flow.Event{Kind: flow.DesignClosed}, func(g *domain.Feature) error {
		if g.State.Phase != domain.PhaseDesigning || g.Design == nil || g.Design.Close == nil {
			return errNoChange
		}
		d := cloneDesign(g.Design)
		design.ApplyClose(&d, *d.Close)
		g.Design = &d
		return nil
	})
	if err != nil {
		return HookResponse{Next: &Next{Feature: f, Note: "nxy could not record your answer; ask again"}}
	}
	return HookResponse{Next: &Next{Feature: nf, Note: "the design is in the db: /clear is safe"}}
}
