package app

import "github.com/matyy-a/nxy.dev/internal/filter"

// preToolBash mirrors hosts/claude-code/hooks/pretooluse-bash.mjs (filter part): rewrite the
// command through the engine, keeping the approval the original would have had.
func (h Hooks) preToolBash(ev HookEvent) HookResponse {
	if !ev.HasCommand {
		return HookResponse{}
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
