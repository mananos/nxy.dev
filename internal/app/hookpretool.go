package app

import (
	"context"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/filter"
	"github.com/mananos/nxy.dev/internal/searchscope"
)

// shellWrites returns the files the command writes inside the repo (outside .git and .nxy), in the
// form normPath gives. Pure string work: commands without such targets never open the store.
func (h Hooks) shellWrites(ev HookEvent) []string {
	root := ev.ProjectDir
	if root == "" {
		return nil
	}
	cwd := ev.Cwd
	if cwd == "" {
		cwd = root
	}
	o := filter.WriteOptions{Tool: ev.Tool}
	if h.Files != nil {
		o.Native, o.IsDir = h.Files.NativePath, h.Files.IsDir
	}
	targets := filter.WriteTargets(ev.Command, cwd, o)
	if len(targets) == 0 {
		return nil
	}
	r := normPath(root, "")
	var out []string
	for _, t := range targets {
		p := normPath(t, "")
		if within(r, p) && !within(r+"/.git", p) && !within(r+"/.nxy", p) {
			out = append(out, p)
		}
	}
	return out
}

// shellCodeGate is the write gate for a shell write of the main thread without a feature: a write
// to a code file is stopped like an Edit (same one-shot escape).
func (h Hooks) shellCodeGate(ctx context.Context, ev HookEvent, targets []string) HookResponse {
	if h.Gate == nil || len(targets) == 0 {
		return HookResponse{}
	}
	// One escape lets the whole command through: it is spent once, by the first code target.
	var first HookResponse
	spent := false
	for _, t := range targets {
		if r := h.writeGateWith(ctx, ev, ev.Tool, t, &spent); r.Deny != "" && first.Deny == "" {
			first = r
		}
	}
	return first
}

// broadShellSearch is true for a broad grep/find/rg of the main thread, which goes to the read gate.
func (h Hooks) broadShellSearch(ev HookEvent) bool {
	return ev.AgentID == "" && h.Gate != nil && searchscope.IsBroadMainSearch(ev.Tool, map[string]any{"command": ev.Command})
}

// preToolBash mirrors hosts/claude-code/hooks/pretooluse-bash.mjs: while a feature is active the
// main thread does not write the repo through the shell either (an implementer's writes are
// counted), then the command goes through the engine, keeping the approval the original would have had.
func (h Hooks) preToolBash(ctx context.Context, ev HookEvent) HookResponse {
	if !ev.HasCommand {
		return HookResponse{}
	}
	if r, ok := h.ownStep(ctx, ev); ok {
		return r
	}
	if h.broadShellSearch(ev) {
		if r, denied := h.readGate(ctx, ev, ev.Tool, map[string]any{"command": ev.Command}); denied {
			return r
		}
	}
	writes := h.shellWrites(ev)
	if ev.AgentID != "" && ev.AgentType == roleDocumenter && len(writes) > 0 {
		if f, ok := h.activeFeature(ctx, ev); ok && f.ProjectDir != "" {
			return HookResponse{Deny: "nxy: feature " + f.ID + " — the documenter edits docs with Edit/Write, not through the shell"}
		}
	}
	if (ev.AgentID == "" || ev.AgentType == roleImplementer) && len(writes) > 0 {
		if f, ok := h.activeFeature(ctx, ev); ok && f.ProjectDir != "" {
			if ev.AgentID == "" {
				return HookResponse{Deny: mainThreadWriteDeny(f)}
			}
			if f.State.Phase == domain.PhaseRunningBatch {
				n, _ := h.editBatch(f, ev, writes[0])
				h.noteWrite(ctx, f.ID, n)
			}
		} else if ev.AgentID == "" {
			if r := h.shellCodeGate(ctx, ev, writes); r.Deny != "" {
				return r
			}
		}
		h.markEdit(ctx, ev)
	}
	s := h.Config.Filter(ev.ProjectDir)
	if !s.Enabled {
		return HookResponse{}
	}
	info := h.Engine.Info(s, ev.ProjectDir)
	fctx := filter.Context{
		Tool:            ev.Tool,
		Engine:          info.Engine,
		RtkHookDetected: info.RtkHookDetected,
		ExcludeCommands: s.ExcludeCommands,
		OnlyCommands:    s.OnlyCommands,
	}
	if info.RtkPath != "" {
		fctx.Rewrite = func(c string) (filter.RewriteResult, error) { return h.Engine.Rewrite(info, c) }
	}
	d := filter.Decide(ev.Command, fctx)
	if d.Action != "rewrite" || d.Command == "" {
		return HookResponse{}
	}
	// A denied original must stay denied.
	verdict := filter.OriginalVerdict(ev.Command, h.Permissions.Rules(ev.ProjectDir))
	if verdict == "deny" {
		return HookResponse{}
	}
	// rtk's own verdict only counts for a single command; for a chain ours checks every part.
	rtkAllows := d.Allow && len(filter.CommandSegments(ev.Command)) == 1 && !filter.HasSubstitution(ev.Command)
	return HookResponse{Rewrite: &CommandRewrite{
		Command: filter.PinRtkPath(d.Command, info.RtkPath),
		Allow:   s.AutoAllow && (rtkAllows || verdict == "allow"),
	}}
}
