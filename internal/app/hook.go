package app

import (
	"context"
	"encoding/json"
)

// HookEvent is a host hook call, already decoded by an adapter.
type HookEvent struct {
	Name           string
	Tool           string
	SessionID      string
	Cwd            string
	TranscriptPath string
	// Command is tool_input.command; HasCommand is false when it is missing or not a string.
	Command    string
	HasCommand bool
	ProjectDir string
	// ToolInput is the raw tool_input, opaque to the use cases: the adapter uses it to build updatedInput.
	ToolInput json.RawMessage
}

// CommandRewrite asks the host to run Command instead of the original; Allow also pre-approves it.
type CommandRewrite struct {
	Command string
	Allow   bool
}

// HookResponse is what a hook answers. The zero value says nothing.
type HookResponse struct {
	Rewrite *CommandRewrite
}

// Hooks answers hook events with the ports it was given.
type Hooks struct {
	Config      FilterConfig
	Engine      FilterEngine
	Permissions PermissionRules
}

// Handle answers a hook event. Only PreToolUse on Bash/PowerShell says something so far.
func (h Hooks) Handle(ctx context.Context, ev HookEvent) HookResponse {
	if ev.Name == "PreToolUse" && (ev.Tool == "Bash" || ev.Tool == "PowerShell") {
		return h.preToolBash(ev)
	}
	return HookResponse{}
}
