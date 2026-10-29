package claudecode

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/mananos/nxy.dev/internal/app"
	"github.com/mananos/nxy.dev/internal/platform"
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
	ToolUseID      string          `json:"tool_use_id"`
	ToolResponse   json.RawMessage `json:"tool_response"`
}

// Decode reads a Claude Code hook payload. Unknown fields are ignored.
func Decode(r io.Reader) (app.HookEvent, error) {
	var in hookInput
	if err := json.NewDecoder(r).Decode(&in); err != nil {
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
		ToolUseID:      in.ToolUseID,
	}
	var ti struct {
		Command      json.RawMessage `json:"command"`
		SubagentType string          `json:"subagent_type"`
		Prompt       string          `json:"prompt"`
		FilePath     string          `json:"file_path"`
		NotebookPath string          `json:"notebook_path"`
		Questions    []struct {
			Question string `json:"question"`
		} `json:"questions"`
		Answers map[string]any `json:"answers"`
	}
	if json.Unmarshal(in.ToolInput, &ti) == nil {
		var s string
		if string(ti.Command) != "null" && json.Unmarshal(ti.Command, &s) == nil {
			ev.Command, ev.HasCommand = s, true
		}
		ev.Subagent, ev.AgentPrompt = ti.SubagentType, ti.Prompt
		ev.FilePath = ti.FilePath
		if ev.FilePath == "" {
			ev.FilePath = ti.NotebookPath
		}
		ev.FilePath = platform.NativePath(ev.FilePath)
		for _, q := range ti.Questions {
			ev.Questions = append(ev.Questions, q.Question)
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
	case ev.Name == "UserPromptSubmit" && resp.Deny != "":
		out = map[string]any{"decision": "block", "reason": resp.Deny}
	case resp.Rewrite != nil:
		return encodeRewrite(w, ev, resp)
	case ev.Name == "PreToolUse" && resp.Prompt != "":
		fields := map[string]json.RawMessage{}
		_ = json.Unmarshal(ev.ToolInput, &fields)
		p, err := marshal(resp.Prompt)
		if err != nil {
			return err
		}
		fields["prompt"] = p
		out = map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "allow",
			"permissionDecisionReason": "nxy: prompt written by nxy",
			"updatedInput":             fields,
		}}
	case ev.Name != "PreToolUse" && (resp.Next != nil || resp.Notice != ""):
		out = map[string]any{}
		if resp.Next != nil {
			out["hookSpecificOutput"] = map[string]any{
				"hookEventName":     ev.Name,
				"additionalContext": Instruction(resp.Next.Feature, resp.Next.Note),
			}
		}
		if resp.Notice != "" {
			out["systemMessage"] = resp.Notice
		}
	default:
		return nil
	}
	b, err := marshal(out)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
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
		if s, ok := v.(string); ok {
			out[k] = s
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

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
