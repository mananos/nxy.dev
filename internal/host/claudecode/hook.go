package claudecode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/matyy-a/nxy.dev/internal/app"
	"github.com/matyy-a/nxy.dev/internal/platform"
)

type hookInput struct {
	HookEventName  string          `json:"hook_event_name"`
	ToolName       string          `json:"tool_name"`
	SessionID      string          `json:"session_id"`
	Cwd            string          `json:"cwd"`
	TranscriptPath string          `json:"transcript_path"`
	ToolInput      json.RawMessage `json:"tool_input"`
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
	}
	var ti struct {
		Command json.RawMessage `json:"command"`
	}
	if json.Unmarshal(in.ToolInput, &ti) == nil {
		var s string
		if string(ti.Command) != "null" && json.Unmarshal(ti.Command, &s) == nil {
			ev.Command, ev.HasCommand = s, true
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
	if resp.Rewrite == nil {
		return nil
	}
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

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
