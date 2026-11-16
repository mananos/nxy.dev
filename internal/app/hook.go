package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mananos/nxy.dev/internal/ask"
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
	// AgentResult is the last message of the subagent that stopped (SubagentStop).
	AgentResult string
	// AgentTranscript is the subagent's own transcript file (SubagentStop).
	AgentTranscript string
	// Prompt is the user's prompt (UserPromptSubmit); Source is SessionStart's source.
	Prompt    string
	Source    string
	ToolUseID string
	// Subagent and AgentPrompt are tool_input.subagent_type / prompt of Agent/Task.
	Subagent    string
	AgentPrompt string
	// AgentModel and AgentEffort are tool_input.model / effort of the dispatch, when it carries them.
	AgentModel  string
	AgentEffort string
	// FilePath is tool_input.file_path (or notebook_path).
	FilePath string
	// Questions are the AskUserQuestion question texts; InputHasAnswers is true when
	// the tool_input already carries answers put there by the model.
	Questions       []string
	InputHasAnswers bool
	// Asked are the questions of the AskUserQuestion call with their options' labels (PreToolUse).
	Asked []domain.AskedQuestion
	// Answers maps question to the user's answer (PostToolUse AskUserQuestion).
	Answers map[string]string
	// AgentAsync is true on PostToolUse Agent/Task when the agent was launched in the background.
	AgentAsync bool
	// AsyncAgentID is the agentId of the background agent in that tool_response, when it carries one.
	AsyncAgentID string
	// Reason is SessionEnd's reason (clear, logout, other...).
	Reason string
	// ToolResponse is the raw tool_response of PostToolUse Bash/PowerShell: {stdout, stderr} or an old string.
	ToolResponse json.RawMessage
}

// AskOption is one option of a question nxy asks the user.
type AskOption struct {
	Label       string
	Description string
}

// AskSpec is a question the main thread has to put to the user (AskUserQuestion).
type AskSpec struct {
	Question    string
	Header      string
	MultiSelect bool
	Options     []AskOption
	// More are further questions asked in the same AskUserQuestion call.
	More []AskSpec
	// After replaces the default tail of the instruction ("carry on with their message").
	After string
}

// Next is the step the main thread has to take; the host adapter words it.
type Next struct {
	Feature domain.Feature
	Note    string
	// Exe is the path of the nxy binary, for the commands the adapter writes.
	Exe string
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
	// AllowReason pre-approves the call (PreToolUse) with this reason; empty means no decision.
	AllowReason string
	// Prompt replaces tool_input.prompt (PreToolUse Agent), keeping the rest of the input.
	Prompt string
	// Model and Effort set tool_input.model / effort (PreToolUse Agent) from the config's role table.
	Model  string
	Effort string
	// Next is the instruction for the main thread.
	Next *Next
	// Notice is shown to the user.
	Notice string
	// Context is a plain line for the main thread, with no step attached.
	Context string
	// Ask is a question for the main thread to ask the user before anything else.
	Ask *AskSpec
	// Block keeps the main thread from stopping (Stop), with this instruction as the reason.
	Block string
	// Answers answers an AskUserQuestion call from the TUI (PreToolUse); the call then runs with them
	// and shows no dialog. A pointer so a HookResponse stays comparable.
	Answers *AskAnswers
	// Title sets the terminal title: a decision of nxy is pending, or the title goes back to the host's.
	Title Title
}

// Title is a change of the terminal title the host is asked to make.
type Title int

const (
	TitleNone Title = iota
	// TitlePending says a question of nxy is waiting for the user.
	TitlePending
	// TitleRestore gives the title back to the host.
	TitleRestore
)

// AskAnswers maps each question of an AskUserQuestion call to the chosen label (a multiSelect one
// joins its labels with ", ").
type AskAnswers struct {
	ByQuestion map[string]string
}

// AskWait sets how the hook waits for the TUI to answer a question; zero values mean 300 ms and 25 min.
type AskWait struct {
	Every time.Duration
	Max   time.Duration
}

func (w AskWait) every() time.Duration {
	if w.Every <= 0 {
		return 300 * time.Millisecond
	}
	return w.Every
}

func (w AskWait) max() time.Duration {
	if w.Max <= 0 {
		return 25 * time.Minute
	}
	return w.Max
}

// Hooks answers hook events with the ports it was given.
type Hooks struct {
	Config      FilterConfig
	Engine      FilterEngine
	Permissions PermissionRules
	// Roles gives the model and effort the user chose per role; nil leaves every role as its frontmatter.
	Roles Roles
	// Store, Repos, Legacy and Viewer serve the feature flow; a nil Store answers nothing there.
	Store  Store
	Repos  Repos
	Legacy Legacy
	Viewer Viewer
	// Shell runs the Accept command of a batch.
	Shell Shell
	// Files serves the Bash write parser; nil reads paths as they are.
	Files Files
	// Exe is the path of the nxy binary, with forward slashes.
	Exe string
	// Changes, Graph, Runners and Trees serve `nxy affected`; nil ones degrade to plan paths,
	// no graph, the embedded table and no files.
	Changes Changes
	Graph   TestGraph
	Runners Runners
	Trees   FileTrees
	// Lenses, Diffs, ReviewLog and ReviewFiles serve the review; nil ones degrade to the embedded
	// lenses only, no diff, no record and no review document.
	Lenses      Lenses
	Diffs       Diffs
	ReviewLog   ReviewLog
	ReviewFiles ReviewFiles
	// Docs finds the docs a finished feature changes; nil means there are none to look for.
	Docs Docs
	// Branches runs the git of the branch step; nil means no branch is made (the feature works where it is).
	Branches Branches
	// Memory serves the repo memory inside the flow; nil means no memory: everything that uses it is skipped.
	Memory FlowMemory
	// Sessions keeps the per-session hook state; nil means no notes question, no chaining.
	Sessions Sessions
	// Bootstrap keeps the first-rules bootstrap per repo; nil means it is never offered.
	Bootstrap Bootstrap
	// Gate and Context serve the write and read gates; nil means the gate does not act.
	Gate    Gate
	Context ContextMeter
	// Clock gives the time for notes and sessions; nil means the system clock.
	Clock Clock
	// LegacyData and LegacyStore import what the 1.x knows of the repo at SessionStart; either nil
	// means no import.
	LegacyData  LegacyData
	LegacyStore LegacyStore
	// AgentFiles and Runs record what each subagent cost at SubagentStop; either nil means no record.
	AgentFiles AgentFiles
	Runs       AgentRuns
	// Asks and TUI let a live TUI answer nxy's questions; either nil means the hook never waits.
	Asks    PendingAsks
	TUI     TUILiveness
	AskWait AskWait
}

// Handle answers a hook event, one use case per event/tool.
func (h Hooks) Handle(ctx context.Context, ev HookEvent) HookResponse {
	answeredNxy := ev.Name == "PostToolUse" && ev.Tool == toolAskUser && h.askedByNxy(ctx, ev)
	resp := h.handle(ctx, ev)
	if resp.Next != nil {
		resp.Next.Exe = h.Exe
	}
	if ev.Name != "PreToolUse" && resp.Next != nil {
		if a, ok := ask.For(resp.Next.Feature); ok && len(a.Questions) > 0 {
			resp.Title = TitlePending
		}
	}
	if answeredNxy && resp.Title == TitleNone {
		resp.Title = TitleRestore
	}
	return resp
}

// askedByNxy is true when some question of the AskUserQuestion call is one of the active feature's.
func (h Hooks) askedByNxy(ctx context.Context, ev HookEvent) bool {
	if ev.AgentID != "" || h.Store == nil {
		return false
	}
	f, ok := h.activeFeature(ctx, ev)
	if !ok {
		return false
	}
	for _, q := range ev.Questions {
		if isNxyQuestion(f, q) {
			return true
		}
	}
	return false
}

func (h Hooks) handle(ctx context.Context, ev HookEvent) HookResponse {
	switch {
	case ev.Name == "PreToolUse" && (ev.Tool == "Bash" || ev.Tool == "PowerShell"):
		return h.preToolBash(ctx, ev)
	case ev.Name == "PostToolUse" && (ev.Tool == "Bash" || ev.Tool == "PowerShell"):
		h.postToolBash(ctx, ev)
		return HookResponse{}
	case ev.Name == "PreToolUse" && (ev.Tool == "Grep" || ev.Tool == "Glob"):
		return h.preToolSearch(ctx, ev)
	case ev.Name == "PreToolUse" && isAgentTool(ev.Tool):
		return h.preToolAgent(ctx, ev)
	case ev.Name == "PostToolUse" && isAgentTool(ev.Tool):
		if r, ok := h.bootstrapScoutDone(ctx, ev); ok {
			return h.deliverPending(ctx, ev, r)
		}
		return h.deliverPending(ctx, ev, h.postToolAgent(ctx, ev))
	case ev.Name == "UserPromptSubmit":
		if r, ok := h.firstPromptNotes(ctx, ev); ok {
			return r
		}
		if r, ok := h.changeNote(ctx, ev); ok {
			return h.withRuleHint(ctx, ev, h.deliverPending(ctx, ev, r))
		}
		h.designRef(ctx, ev)
		return h.withRuleHint(ctx, ev, h.deliverPending(ctx, ev, h.startFeature(ctx, ev)))
	case ev.Name == "PreToolUse" && ev.Tool == toolAskUser:
		return h.preToolAskUser(ctx, ev)
	case ev.Name == "PostToolUse" && ev.Tool == toolAskUser:
		return h.deliverPending(ctx, ev, h.postToolAskUser(ctx, ev))
	case ev.Name == "PreToolUse" && isEditTool(ev.Tool):
		return h.preToolEdit(ctx, ev)
	case ev.Name == "SessionStart":
		return h.deliverPending(ctx, ev, h.sessionStart(ctx, ev))
	case ev.Name == "SessionEnd":
		return h.sessionEnd(ctx, ev)
	case ev.Name == "SubagentStop":
		return h.subagentStop(ctx, ev)
	case ev.Name == "Stop":
		r := h.deliverPending(ctx, ev, HookResponse{})
		if r.Next == nil {
			if n, ok := h.stopNote(ctx, ev); ok {
				return n
			}
		}
		return r
	}
	return HookResponse{}
}
