package app

import (
	"context"
	"regexp"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/filter"
	"github.com/mananos/nxy.dev/v2/internal/searchscope"
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

// decideCommand matches a command that runs `nxy … action decide`, in Bash or PowerShell (the binary
// may carry a path, an extension or quotes). Strict on purpose: the answer is the user's, not the model's.
var decideCommand = regexp.MustCompile(`(?i)\bnxy(?:\.exe|\.cmd)?['"]?(?:\s+\S+)*?\s+action\s+(?:decide|pick)\b`)

// preToolBash mirrors hosts/claude-code/hooks/pretooluse-bash.mjs: while a feature is active the
// main thread does not write the repo through the shell either (an implementer's writes are
// counted), then the command goes through the engine, keeping the approval the original would have had.
func (h Hooks) preToolBash(ctx context.Context, ev HookEvent) HookResponse {
	if !ev.HasCommand {
		return HookResponse{}
	}
	if decideCommand.MatchString(ev.Command) {
		return HookResponse{Deny: "nxy action decide and pick are the user's answer from the TUI; ask with AskUserQuestion"}
	}
	if r, ok := h.ownStep(ctx, ev); ok {
		return r
	}
	rtkResp := h.rtkStep(ev)
	// A command stopped so it runs through rtk spends nothing: the run that follows does.
	if rtkResp.Deny != "" {
		return rtkResp
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
				h.noteWrite(ctx, f.ID, n, writes[0])
			}
		} else if ev.AgentID == "" {
			if r := h.shellCodeGate(ctx, ev, writes); r.Deny != "" {
				return r
			}
		}
		h.markEdit(ctx, ev)
	}
	return rtkResp
}

// rtkStep is the rtk rewrite of a command: a rewrite, the auto mode deny, or nothing.
func (h Hooks) rtkStep(ev HookEvent) HookResponse {
	s, info, fctx, _ := h.rtkEngine(ev.ProjectDir, ev.Tool)
	if !s.Enabled {
		return HookResponse{}
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
	pinned := filter.PinRtkPath(d.Command, info.RtkPath)
	allow := s.AutoAllow && (rtkAllows || verdict == "allow")
	// Auto mode refuses a changed input that carries no decision: ask for the rewrite instead.
	if ev.PermissionMode == permissionModeAuto && !allow {
		return HookResponse{Deny: autoModeRtkDeny(pinned, ev.Tool)}
	}
	return HookResponse{Rewrite: &CommandRewrite{Command: pinned, Allow: allow}}
}

// autoModeRtkDeny names the command to run instead; PowerShell needs `&` to run a quoted path.
func autoModeRtkDeny(cmd, tool string) string {
	if tool == "PowerShell" && strings.HasPrefix(cmd, `"`) {
		cmd = "& " + cmd
	}
	return "nxy: auto mode — nxy filters this command through rtk but cannot rewrite it for you here; run exactly this instead: " + cmd
}

// rtkActive resolves the filter settings and engine; the bool is true when rtk is the active engine.
func (h Hooks) rtkActive(dir string) (filter.Settings, filter.EngineInfo, bool) {
	if h.Config == nil || h.Engine == nil {
		return filter.Settings{}, filter.EngineInfo{}, false
	}
	s := h.Config.Filter(dir)
	if !s.Enabled {
		return s, filter.EngineInfo{}, false
	}
	info := h.Engine.Info(s, dir)
	return s, info, info.Engine == "rtk" && info.RtkPath != "" && !info.RtkHookDetected
}

// rtkEngine resolves the filter settings, engine and decision context; the bool is true when rtk is the active engine.
func (h Hooks) rtkEngine(dir, tool string) (filter.Settings, filter.EngineInfo, filter.Context, bool) {
	s, info, active := h.rtkActive(dir)
	if !s.Enabled {
		return s, filter.EngineInfo{}, filter.Context{}, false
	}
	fctx := filter.Context{
		Tool:            tool,
		Engine:          info.Engine,
		RtkHookDetected: info.RtkHookDetected,
		ExcludeCommands: s.ExcludeCommands,
		OnlyCommands:    s.OnlyCommands,
	}
	if info.RtkPath != "" {
		fctx.Rewrite = func(c string) (filter.RewriteResult, error) { return h.Engine.Rewrite(info, c) }
	}
	return s, info, fctx, active
}
