package app

import (
	"context"
	"errors"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
)

const (
	featureCommand  = "/nxy:feature"
	engineerCommand = "/nxy:engineer"
)

// featureRequest returns what follows /nxy:feature, untouched except for the separator, and
// whether the prompt is that command at all.
func featureRequest(prompt string) (string, bool) {
	return commandRequest(prompt, featureCommand)
}

// commandRequest is featureRequest for any of the commands that start a feature.
func commandRequest(prompt, cmd string) (string, bool) {
	rest, ok := strings.CutPrefix(prompt, cmd)
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
	cmd := featureCommand
	request, ok := featureRequest(ev.Prompt)
	if !ok {
		cmd = engineerCommand
		request, ok = commandRequest(ev.Prompt, cmd)
	}
	if !ok || h.Store == nil || h.Repos == nil {
		return HookResponse{}
	}
	if strings.TrimSpace(request) == "" {
		return HookResponse{Deny: "usage: " + cmd + " <what you want>"}
	}
	engineer := cmd == engineerCommand
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
	s, as, err := flow.Next(domain.State{}, flow.Event{Kind: flow.Requested, Engineer: engineer})
	if err != nil {
		return HookResponse{}
	}
	f := domain.Feature{Repo: repo, ProjectDir: dir, Request: request, State: s, Next: mainThreadAction(as)}
	if engineer {
		f.Mode = domain.ModeEngineer
		f.Design = &domain.Design{}
	}
	id, err := h.Store.CreateFeature(ctx, f)
	if errors.Is(err, ErrActiveFeature) {
		// two prompts raced: the other one created the feature first
		if cur, found, err := h.Store.ActiveFeature(ctx, repo); err == nil && found {
			return HookResponse{Deny: "feature " + cur.ID + " in progress (" + string(cur.State.Phase.Public()) +
				"); next step " + cur.Next.Label() + "; to end it: `nxy feature stop`"}
		}
		return HookResponse{}
	}
	if err != nil {
		return HookResponse{}
	}
	f.ID = id
	return HookResponse{Next: &Next{Feature: f}}
}
