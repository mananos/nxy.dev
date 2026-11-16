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
	home := h.Repos.Checkout(dir)
	if f, ok := h.linkedFeature(ctx, ev); ok {
		return inProgress(f)
	}
	if f, found, err := h.Store.ActiveFeature(ctx, home); err != nil {
		return HookResponse{}
	} else if found && !h.heldByOther(ctx, ev, f) {
		return inProgress(f)
	}
	return h.beginFeature(ctx, ev, dir, request, engineer)
}

// beginFeature starts the feature of a request: the once-per-repo rules offer when in a repo, else
// the feature itself.
func (h Hooks) beginFeature(ctx context.Context, ev HookEvent, dir, request string, engineer bool) HookResponse {
	if _, isRepo := h.Repos.Root(dir); isRepo {
		if r, ok := h.offerBootstrap(ctx, h.Repos.Key(dir), request, engineer); ok {
			return r
		}
	}
	return h.createFeature(ctx, ev, dir, request, engineer)
}

// inProgress is the refusal to start a feature while another one is active.
func inProgress(f domain.Feature) HookResponse {
	return HookResponse{Deny: "feature " + f.ID + " in progress (" + string(f.State.Phase.Public()) +
		"); next step " + f.Next.Label() + "; to end it: `nxy feature stop`"}
}

// heldByOther is true when a different live (not ended) session is linked to the feature f that
// holds the folder: this session may then start its own feature. A holder nobody else works on
// (a reopened session, a feature of before the links) is this session's own.
func (h Hooks) heldByOther(ctx context.Context, ev HookEvent, f domain.Feature) bool {
	if h.Sessions == nil || ev.SessionID == "" {
		return false
	}
	live, err := h.Sessions.OtherLive(ctx, f.ID, ev.SessionID)
	return err == nil && live
}

// linkedFeature is the feature the event's session is linked to, if it is neither done nor paused.
func (h Hooks) linkedFeature(ctx context.Context, ev HookEvent) (domain.Feature, bool) {
	if h.Sessions == nil || h.Store == nil || ev.SessionID == "" {
		return domain.Feature{}, false
	}
	s, ok, err := h.Sessions.Session(ctx, ev.SessionID)
	if err != nil || !ok || s.FeatureID == "" {
		return domain.Feature{}, false
	}
	f, err := h.Store.Feature(ctx, s.FeatureID)
	if err != nil || f.State.Phase == domain.PhaseDone || f.Paused {
		return domain.Feature{}, false
	}
	return f, true
}

// createFeature creates the feature of a request and returns its first step. It holds the folder's
// checkout unless a feature of another session holds it: then it goes without, and the clash is
// solved when its plan is saved.
func (h Hooks) createFeature(ctx context.Context, ev HookEvent, dir, request string, engineer bool) HookResponse {
	repo := h.Repos.Key(dir)
	home := h.Repos.Checkout(dir)
	s, as, err := flow.Next(domain.State{}, flow.Event{Kind: flow.Requested, Engineer: engineer})
	if err != nil {
		return HookResponse{}
	}
	f := domain.Feature{Repo: repo, ProjectDir: dir, Home: home, Holds: []string{home}, Request: request, State: s, Next: mainThreadAction(as)}
	if engineer {
		f.Mode = domain.ModeEngineer
		f.Design = &domain.Design{}
	}
	id, err := h.Store.CreateFeature(ctx, f)
	if errors.Is(err, ErrActiveFeature) {
		// another feature holds the folder: a prompt that raced, or one of another session
		if cur, found, err := h.Store.ActiveFeature(ctx, home); err != nil || !found || !h.heldByOther(ctx, ev, cur) {
			if err == nil && found {
				return inProgress(cur)
			}
			return HookResponse{}
		}
		f.Holds = nil
		id, err = h.Store.CreateFeature(ctx, f)
	}
	if err != nil {
		return HookResponse{}
	}
	f.ID = id
	if h.Sessions != nil && ev.SessionID != "" {
		_ = h.Sessions.LinkFeature(ctx, ev.SessionID, id)
	}
	n := &Next{Feature: f}
	if engineer {
		n.Note = h.engineerMemoryNote(ctx, f)
	}
	return HookResponse{Next: n}
}
