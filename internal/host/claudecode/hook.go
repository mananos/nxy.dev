package claudecode

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/matyy-a/nxy.dev/internal/app"
	"github.com/matyy-a/nxy.dev/internal/platform"
)

type hookInput struct {
	HookEventName  string `json:"hook_event_name"`
	ToolName       string `json:"tool_name"`
	SessionID      string `json:"session_id"`
	Cwd            string `json:"cwd"`
	TranscriptPath string `json:"transcript_path"`
}

// Decode reads a Claude Code hook payload. Unknown fields are ignored.
func Decode(r io.Reader) (app.HookEvent, error) {
	var in hookInput
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return app.HookEvent{}, fmt.Errorf("decode hook payload: %w", err)
	}
	return app.HookEvent{
		Name:           in.HookEventName,
		Tool:           in.ToolName,
		SessionID:      in.SessionID,
		Cwd:            platform.NativePath(in.Cwd),
		TranscriptPath: in.TranscriptPath,
	}, nil
}

// Encode writes the response. The zero response writes nothing.
func Encode(w io.Writer, resp app.HookResponse) error {
	return nil
}
