package app

import (
	"context"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
)

const featureCommand = "/nxy:feature"

// featureRequest returns what follows /nxy:feature, untouched except for the separator, and
// whether the prompt is that command at all.
func featureRequest(prompt string) (string, bool) {
	rest, ok := strings.CutPrefix(prompt, featureCommand)
	if !ok {
		return "", false
	}
	switch {
	case rest == "":
		return "", true
	case strings.HasPrefix(rest, "\r\n"):
		return rest[2:], true
	case rest[0] == ' ' || rest[0] == '\n' || rest[0] == '\t':
		return rest[1:], true
	}
	return "", false
}

// startFeature handles UserPromptSubmit: `/nxy:feature <request>` captures the request verbatim
// and starts a feature; any other prompt is not its business.
func (h Hooks) startFeature(ctx context.Context, ev HookEvent) HookResponse {
	request, ok := featureRequest(ev.Prompt)
	if !ok || h.Store == nil || h.Repos == nil {
		return HookResponse{}
	}
	if strings.TrimSpace(request) == "" {
		return HookResponse{Deny: "usage: /nxy:feature <what you want>"}
	}
	dir := ev.ProjectDir
	if dir == "" {
		dir = ev.Cwd
	}
	repo := h.Repos.Key(dir)
	if f, found, err := h.Store.ActiveFeature(ctx, repo); err != nil {
		return HookResponse{}
	} else if found {
		return HookResponse{Deny: "feature " + f.ID + " in progress (" + string(f.State.Phase.Public()) +
			"); next step " + f.Next.Label() + "; to end it: `nxy feature stop`"}
	}
	s, as, err := flow.Next(domain.State{}, flow.Event{Kind: flow.Requested})
	if err != nil {
		return HookResponse{}
	}
	f := domain.Feature{Repo: repo, ProjectDir: dir, Request: request, State: s, Next: mainThreadAction(as)}
	id, err := h.Store.CreateFeature(ctx, f)
	if err != nil {
		return HookResponse{}
	}
	f.ID = id
	return HookResponse{Next: &Next{Feature: f}}
}
