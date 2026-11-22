package app

import (
	"context"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/memory"
)

// engineerMemoryNote is what the main thread reads when the design starts in engineer mode: every
// repo rule (up to the dispatch budget) and the order to look for related memory. Empty without
// memory or without rules. It counts the delivery of the rules it carries.
func (h Hooks) engineerMemoryNote(ctx context.Context, f domain.Feature) string {
	pkt := h.memoryFor(ctx, f, roleMain, nil)
	if len(pkt.Rules) == 0 {
		return ""
	}
	return "## Repo rules (nxy memory)\n" + pkt.Text +
		"\n\nRun `nxy mem related <files>` when the scout says which files this touches; refute the design against these rules."
}

// withRuleHint adds to the prompt's response a line asking the user whether a "from now on" style
// prompt is a repo rule. It only suggests: nothing is saved here.
func (h Hooks) withRuleHint(ctx context.Context, ev HookEvent, resp HookResponse) HookResponse {
	if h.Store == nil || h.Repos == nil || ev.PromptFromAgent || !memory.RuleHint(ev.Prompt) {
		return resp
	}
	if _, ok := h.activeFeature(ctx, ev); !ok {
		return resp
	}
	hint := "nxy: that message sounds like a repo rule. Ask the user whether to save it; " +
		"if yes, `nxy mem rule \"<text>\" --question \"<what it solves>\"`, or " +
		"`nxy feature rule \"<text>\" --question \"<what it solves>\"` for the closing question. " +
		"Do not save it unless asked."
	if resp.Context != "" {
		resp.Context += "\n" + hint
	} else {
		resp.Context = hint
	}
	return resp
}
