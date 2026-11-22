package claudecode

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/app"
	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/v2/internal/platform"
)

type hookInput struct {
	HookEventName  string          `json:"hook_event_name"`
	ToolName       string          `json:"tool_name"`
	SessionID      string          `json:"session_id"`
	Cwd            string          `json:"cwd"`
	TranscriptPath string          `json:"transcript_path"`
	ToolInput      json.RawMessage `json:"tool_input"`
	AgentID        string          `json:"agent_id"`
	AgentType      string          `json:"agent_type"`
	Prompt         string          `json:"prompt"`
	Source         string          `json:"source"`
	Reason         string          `json:"reason"`
	ToolUseID      string          `json:"tool_use_id"`
	ToolResponse   json.RawMessage `json:"tool_response"`
	PermissionMode string          `json:"permission_mode"`

	StopHookActive       bool   `json:"stop_hook_active"`
	LastAssistantMessage string `json:"last_assistant_message"`
	AgentTranscriptPath  string `json:"agent_transcript_path"`
}

// promptFromAgent reports whether a UserPromptSubmit prompt came from a subagent hand-back or a
// system notification instead of the user.
func promptFromAgent(p string) bool {
	p = strings.TrimSpace(p)
	for _, pre := range []string{"<task-notification", "<system-reminder"} {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	first, _, _ := strings.Cut(p, "\n")
	return strings.Contains(first, "[Subagent hand-back]") ||
		strings.Contains(first, "Another Claude session sent a message")
}

// Decode reads a Claude Code hook payload. Unknown fields are ignored.
func Decode(r io.Reader) (app.HookEvent, error) {
	var in hookInput
	// PowerShell prepends a UTF-8 BOM when it forwards stdin; drop one.
	br := bufio.NewReader(r)
	if head, _ := br.Peek(3); bytes.Equal(head, []byte("\xef\xbb\xbf")) {
		_, _ = br.Discard(3)
	}
	if err := json.NewDecoder(br).Decode(&in); err != nil {
		return app.HookEvent{}, fmt.Errorf("decode hook payload: %w", err)
	}
	ev := app.HookEvent{
		Name:           in.HookEventName,
		Tool:           in.ToolName,
		SessionID:      in.SessionID,
		Cwd:            platform.NativePath(in.Cwd),
		TranscriptPath: in.TranscriptPath,
		ToolInput:      in.ToolInput,
		AgentID:        in.AgentID,
		AgentType:      in.AgentType,
		Prompt:         in.Prompt,
		Source:         in.Source,
		Reason:         in.Reason,
		ToolUseID:      in.ToolUseID,
		PermissionMode: in.PermissionMode,

		StopHookActive: in.StopHookActive,

		PromptFromAgent: promptFromAgent(in.Prompt),
	}
	var ti struct {
		Command      json.RawMessage `json:"command"`
		SubagentType string          `json:"subagent_type"`
		Prompt       string          `json:"prompt"`
		Model        string          `json:"model"`
		Effort       string          `json:"effort"`
		Skill        string          `json:"skill"`
		FilePath     string          `json:"file_path"`
		NotebookPath string          `json:"notebook_path"`
		Questions    []struct {
			Question    string `json:"question"`
			Header      string `json:"header"`
			MultiSelect bool   `json:"multiSelect"`
			Options     []struct {
				Label string `json:"label"`
			} `json:"options"`
		} `json:"questions"`
		Answers map[string]any `json:"answers"`
	}
	if json.Unmarshal(in.ToolInput, &ti) == nil {
		var s string
		if string(ti.Command) != "null" && json.Unmarshal(ti.Command, &s) == nil {
			ev.Command, ev.HasCommand = s, true
		}
		ev.Subagent, ev.AgentPrompt = ti.SubagentType, ti.Prompt
		ev.AgentModel, ev.AgentEffort = ti.Model, ti.Effort
		ev.Skill = ti.Skill
		ev.FilePath = ti.FilePath
		if ev.FilePath == "" {
			ev.FilePath = ti.NotebookPath
		}
		ev.FilePath = platform.NativePath(ev.FilePath)
		for _, q := range ti.Questions {
			ev.Questions = append(ev.Questions, q.Question)
			aq := domain.AskedQuestion{Question: q.Question, Header: q.Header, MultiSelect: q.MultiSelect}
			for _, o := range q.Options {
				aq.Options = append(aq.Options, o.Label)
			}
			ev.Asked = append(ev.Asked, aq)
		}
		ev.InputHasAnswers = len(ti.Answers) > 0
	}
	if ev.Name == "PostToolUse" && ev.Tool == "AskUserQuestion" {
		var tr struct {
			Answers map[string]any `json:"answers"`
		}
		if json.Unmarshal(in.ToolResponse, &tr) == nil {
			ev.Answers = stringAnswers(tr.Answers)
		}
		if len(ev.Answers) == 0 {
			ev.Answers = transcriptAnswers(in.TranscriptPath, in.ToolUseID)
		}
	}
	if ev.Name == "PostToolUse" && (ev.Tool == "Bash" || ev.Tool == "PowerShell") {
		ev.ToolResponse = in.ToolResponse
	}
	if ev.Name == "PostToolUse" && (ev.Tool == "Agent" || ev.Tool == "Task") {
		ev.AgentAsync = asyncLaunch(in.ToolResponse)
		if ev.AgentAsync {
			ev.AsyncAgentID = asyncAgentID(in.ToolResponse)
		}
	}
	if ev.Name == "SubagentStop" {
		ev.AgentResult = in.LastAssistantMessage
		ev.AgentTranscript = in.AgentTranscriptPath
		if ev.AgentResult == "" {
			ev.AgentResult = lastAssistantText(in.AgentTranscriptPath)
		}
		if ev.AgentPrompt == "" {
			// SubagentStop has no tool_input: the first message of the subagent says which batch it is.
			ev.AgentPrompt = firstUserText(in.AgentTranscriptPath)
		}
	}
	dir := os.Getenv("CLAUDE_PROJECT_DIR")
	if dir == "" {
		dir = in.Cwd
	}
	if dir == "" {
		dir, _ = os.Getwd()
	}
	ev.ProjectDir = platform.NativePath(dir)
	return ev, nil
}

// Encode writes the response. The zero response writes nothing.
func Encode(w io.Writer, ev app.HookEvent, resp app.HookResponse) error {
	var out map[string]any
	switch {
	case ev.Name == "PreToolUse" && resp.Deny != "":
		out = map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": resp.Deny,
		}}
	case ev.Name == "PreToolUse" && resp.AllowReason != "":
		out = map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "allow",
			"permissionDecisionReason": resp.AllowReason,
		}}
	case ev.Name == "UserPromptSubmit" && resp.Deny != "":
		out = map[string]any{"decision": "block", "reason": resp.Deny}
	case ev.Name == "PreToolUse" && resp.Answers != nil && len(resp.Answers.ByQuestion) > 0:
		// The original tool_input stays whole (descriptions of the options included); answers is added.
		fields := map[string]json.RawMessage{}
		_ = json.Unmarshal(ev.ToolInput, &fields)
		b, err := marshal(resp.Answers.ByQuestion)
		if err != nil {
			return err
		}
		fields["answers"] = b
		out = map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "allow",
			"permissionDecisionReason": "nxy: contestada en la TUI",
			"updatedInput":             fields,
		}}
	case resp.Rewrite != nil:
		return encodeRewrite(w, ev, resp)
	case ev.Name == "PreToolUse" && (resp.Prompt != "" || resp.Model != "" || resp.Effort != "" || resp.Foreground):
		fields := map[string]json.RawMessage{}
		_ = json.Unmarshal(ev.ToolInput, &fields)
		if resp.Foreground {
			fields["run_in_background"] = json.RawMessage("false")
		}
		for key, val := range map[string]string{"prompt": resp.Prompt, "model": resp.Model, "effort": resp.Effort} {
			if val == "" {
				continue
			}
			b, err := marshal(val)
			if err != nil {
				return err
			}
			fields[key] = b
		}
		reason := "nxy: prompt written by nxy"
		if resp.Prompt == "" && resp.Model == "" && resp.Effort == "" {
			reason = "nxy: foreground"
		} else if resp.Prompt == "" {
			var set []string
			if resp.Model != "" {
				set = append(set, "model")
			}
			if resp.Effort != "" {
				set = append(set, "effort")
			}
			reason = "nxy: roles." + strings.TrimPrefix(ev.Subagent, "nxy:") + "." + strings.Join(set, "/")
		}
		out = map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "allow",
			"permissionDecisionReason": reason,
			"updatedInput":             fields,
		}}
		if resp.Notice != "" {
			out["systemMessage"] = resp.Notice
		}
	case ev.Name == "PreToolUse" && resp.Notice != "" && strings.HasPrefix(ev.Subagent, "nxy:"):
		// A notice alone decides nothing: only the message, no permissionDecision.
		out = map[string]any{"systemMessage": resp.Notice}
	case ev.Name == "Stop" && resp.Block != "":
		out = map[string]any{"decision": "block", "reason": resp.Block}
	case ev.Name == "Stop" && resp.Ask != nil && resp.Next == nil:
		out = map[string]any{"decision": "block", "reason": askInstruction(*resp.Ask)}
	case ev.Name == "Stop" && resp.Next != nil:
		out = map[string]any{"decision": "block", "reason": StopInstruction(resp.Next.Feature, resp.Next.Note, resp.Next.Exe)}
	case ev.Name != "PreToolUse" && (resp.Next != nil || resp.Notice != "" || resp.Context != "" || resp.Ask != nil):
		out = map[string]any{}
		if resp.Next != nil || resp.Context != "" || resp.Ask != nil {
			var parts []string
			if resp.Next != nil {
				parts = append(parts, InstructionFor(resp.Next.Feature, resp.Next.Note, resp.Next.Exe))
			}
			if resp.Ask != nil {
				parts = append(parts, askInstruction(*resp.Ask))
			}
			if resp.Context != "" {
				parts = append(parts, resp.Context)
			}
			out["hookSpecificOutput"] = map[string]any{
				"hookEventName":     ev.Name,
				"additionalContext": strings.Join(parts, "\n\n"),
			}
		}
		if resp.Notice != "" {
			out["systemMessage"] = resp.Notice
		}
	default:
		if resp.Title == app.TitleNone {
			return nil
		}
		out = map[string]any{}
	}
	switch resp.Title {
	case app.TitlePending:
		out["terminalSequence"] = "\x1b]2;● nxy: decisión pendiente\x07"
	case app.TitleRestore:
		out["terminalSequence"] = "\x1b]2;\x07"
	}
	b, err := marshal(out)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

var asyncText = regexp.MustCompile(`(?i)^\s*Async agent launched`)

// asyncLaunch reports whether an Agent tool_response says the agent was launched in the background
// (a string, an object or content blocks). It ports the 1.x isAsyncLaunch.
func asyncLaunch(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return false
	}
	return asyncValue(v)
}

var agentIDText = regexp.MustCompile(`(?i)agent_?id["']?\s*[:=]\s*["']?([A-Za-z0-9_-]+)`)

// asyncAgentID is the agentId an async launch response carries (a field, or "agentId: x" in its text).
func asyncAgentID(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return agentIDValue(v)
}

func agentIDValue(v any) string {
	switch r := v.(type) {
	case string:
		if m := agentIDText.FindStringSubmatch(r); m != nil {
			return m[1]
		}
	case []any:
		for _, b := range r {
			if id := agentIDValue(b); id != "" {
				return id
			}
		}
	case map[string]any:
		for _, k := range []string{"agentId", "agent_id"} {
			if s, _ := r[k].(string); s != "" {
				return s
			}
		}
		for _, k := range []string{"text", "content"} {
			if id := agentIDValue(r[k]); id != "" {
				return id
			}
		}
	}
	return ""
}

func asyncValue(v any) bool {
	switch r := v.(type) {
	case string:
		return asyncText.MatchString(r)
	case []any:
		for _, b := range r {
			if asyncValue(b) {
				return true
			}
		}
	case map[string]any:
		if b, _ := r["isAsync"].(bool); b {
			return true
		}
		if s, _ := r["status"].(string); s == "async_launched" {
			return true
		}
		if s, ok := r["text"].(string); ok && asyncText.MatchString(s) {
			return true
		}
		switch c := r["content"].(type) {
		case []any, string:
			return asyncValue(c)
		}
	}
	return false
}

func encodeRewrite(w io.Writer, ev app.HookEvent, resp app.HookResponse) error {
	fields := map[string]json.RawMessage{}
	_ = json.Unmarshal(ev.ToolInput, &fields)
	cmd, err := marshal(resp.Rewrite.Command)
	if err != nil {
		return err
	}
	fields["command"] = cmd
	hso := map[string]any{"hookEventName": "PreToolUse", "updatedInput": fields}
	if resp.Rewrite.Allow {
		hso["permissionDecision"] = "allow"
		hso["permissionDecisionReason"] = "nxy: original command matched an allow rule"
	}
	b, err := marshal(map[string]any{"hookSpecificOutput": hso, "suppressOutput": true})
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

func stringAnswers(m map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		switch x := v.(type) {
		case string:
			out[k] = x
		case []any:
			var parts []string
			for _, e := range x {
				s, ok := e.(string)
				if !ok {
					parts = nil
					break
				}
				parts = append(parts, s)
			}
			if len(parts) > 0 {
				out[k] = strings.Join(parts, ", ")
			}
		}
	}
	return out
}

// transcriptAnswers finds the user's answers to the AskUserQuestion call toolUseID in the
// transcript (main thread only: sidechain entries are skipped). Errors give nil.
func transcriptAnswers(path, toolUseID string) map[string]string {
	if path == "" || toolUseID == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	var found map[string]string
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && bytes.Contains(line, []byte(toolUseID)) {
			var e struct {
				Type        string `json:"type"`
				IsSidechain bool   `json:"isSidechain"`
				Message     struct {
					Content json.RawMessage `json:"content"`
				} `json:"message"`
				ToolUseResult struct {
					Answers map[string]any `json:"answers"`
				} `json:"toolUseResult"`
			}
			if json.Unmarshal(line, &e) == nil && e.Type == "user" && !e.IsSidechain {
				var blocks []struct {
					Type      string `json:"type"`
					ToolUseID string `json:"tool_use_id"`
					IsError   bool   `json:"is_error"`
				}
				if json.Unmarshal(e.Message.Content, &blocks) == nil {
					for _, b := range blocks {
						if b.Type == "tool_result" && b.ToolUseID == toolUseID && !b.IsError {
							if a := stringAnswers(e.ToolUseResult.Answers); len(a) > 0 {
								found = a
							}
						}
					}
				}
			}
		}
		if err != nil {
			return found
		}
	}
}

// transcriptTailBytes is how much of the end of a subagent transcript is read for its last message.
const transcriptTailBytes = 256 << 10

// lastAssistantText is the text of the last assistant message of a transcript, read from its tail.
// Errors give "".
func lastAssistantText(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	size, truncated := st.Size(), false
	if size > transcriptTailBytes {
		if _, err := f.Seek(size-transcriptTailBytes, io.SeekStart); err != nil {
			return ""
		}
		truncated = true
	}
	tail, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	if truncated {
		nl := bytes.IndexByte(tail, '\n')
		if nl == -1 {
			return ""
		}
		tail = tail[nl+1:]
	}
	lines := bytes.Split(tail, []byte{'\n'})
	for i := len(lines) - 1; i >= 0; i-- {
		if len(lines[i]) == 0 {
			continue
		}
		var e struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(lines[i], &e) != nil || e.Type != "assistant" {
			continue
		}
		var s string
		if json.Unmarshal(e.Message.Content, &s) == nil && s != "" {
			return s
		}
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(e.Message.Content, &blocks) == nil {
			var parts []string
			for _, b := range blocks {
				if b.Type == "text" && b.Text != "" {
					parts = append(parts, b.Text)
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, "\n")
			}
		}
	}
	return ""
}

// transcriptHeadBytes is how much of the start of a subagent transcript is read for its first message.
const transcriptHeadBytes = 64 << 10

// firstUserText is the text of the first user message of a transcript (the prompt a subagent was
// given), read from its head. Errors give "".
func firstUserText(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	head, err := io.ReadAll(io.LimitReader(f, transcriptHeadBytes))
	if err != nil {
		return ""
	}
	return transcript.FirstUserText(head)
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
