package app

import (
	"context"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
)

// changeNote handles UserPromptSubmit while the feature waits for the change note: the prompt,
// verbatim, is the note. It reports whether the prompt was consumed.
func (h Hooks) changeNote(ctx context.Context, ev HookEvent) (HookResponse, bool) {
	if _, isCmd := featureRequest(ev.Prompt); isCmd || strings.HasPrefix(ev.Prompt, "/") || h.Store == nil || h.Repos == nil {
		return HookResponse{}, false
	}
	f, ok := h.activeFeature(ctx, ev)
	if !ok || f.State.Phase != domain.PhaseChangeNote {
		return HookResponse{}, false
	}
	nf, _, err := h.advanceWith(ctx, f.ID, flow.Event{Kind: flow.ChangeNoted}, func(x *domain.Feature) error {
		x.ChangeNotes = append(append([]string(nil), x.ChangeNotes...), ev.Prompt)
		return nil
	})
	if err != nil {
		return HookResponse{}, true
	}
	return HookResponse{Next: &Next{Feature: nf}}, true
}
