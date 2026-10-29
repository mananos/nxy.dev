package app

import (
	"context"
	"encoding/json"

	"github.com/mananos/nxy.dev/internal/domain"
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
	// AgentID and AgentType are set only when the hook runs inside a subagent.
	AgentID   string
	AgentType string
	// Prompt is the user's prompt (UserPromptSubmit); Source is SessionStart's source.
	Prompt    string
	Source    string
	ToolUseID string
	// Subagent and AgentPrompt are tool_input.subagent_type / prompt of Agent/Task.
	Subagent    string
	AgentPrompt string
	// FilePath is tool_input.file_path (or notebook_path).
	FilePath string
	// Questions are the AskUserQuestion question texts; InputHasAnswers is true when
	// the tool_input already carries answers put there by the model.
	Questions       []string
	InputHasAnswers bool
	// Answers maps question to the user's answer (PostToolUse AskUserQuestion).
	Answers map[string]string
}

// Next is the step the main thread has to take; the host adapter words it.
type Next struct {
	Feature domain.Feature
	Note    string
}

// CommandRewrite asks the host to run Command instead of the original; Allow also pre-approves it.
type CommandRewrite struct {
	Command string
	Allow   bool
}

// HookResponse is what a hook answers. The zero value says nothing.
type HookResponse struct {
	Rewrite *CommandRewrite
	// Deny is the reason to refuse the call or prompt; empty means no.
	Deny string
	// Prompt replaces tool_input.prompt (PreToolUse Agent), keeping the rest of the input.
	Prompt string
	// Next is the instruction for the main thread.
	Next *Next
	// Notice is shown to the user.
	Notice string
}

// Hooks answers hook events with the ports it was given.
type Hooks struct {
	Config      FilterConfig
	Engine      FilterEngine
	Permissions PermissionRules
	// Store, Repos, Legacy and Viewer serve the feature flow; a nil Store answers nothing there.
	Store  Store
	Repos  Repos
	Legacy Legacy
	Viewer Viewer
	// Exe is the path of the nxy binary, with forward slashes.
	Exe string
}

// Handle answers a hook event, one use case per event/tool.
func (h Hooks) Handle(ctx context.Context, ev HookEvent) HookResponse {
	switch {
	case ev.Name == "PreToolUse" && (ev.Tool == "Bash" || ev.Tool == "PowerShell"):
		return h.preToolBash(ev)
	case ev.Name == "PreToolUse" && isAgentTool(ev.Tool):
		return h.preToolAgent(ctx, ev)
	case ev.Name == "PostToolUse" && isAgentTool(ev.Tool):
		return h.postToolAgent(ctx, ev)
	case ev.Name == "UserPromptSubmit":
		if r, ok := h.changeNote(ctx, ev); ok {
			return r
		}
		return h.startFeature(ctx, ev)
	case ev.Name == "PreToolUse" && ev.Tool == toolAskUser:
		return h.preToolAskUser(ctx, ev)
	case ev.Name == "PostToolUse" && ev.Tool == toolAskUser:
		return h.postToolAskUser(ctx, ev)
	case ev.Name == "PreToolUse" && isEditTool(ev.Tool):
		return h.preToolEdit(ctx, ev)
	case ev.Name == "SessionStart":
		return h.sessionStart(ctx, ev)
	}
	return HookResponse{}
}
