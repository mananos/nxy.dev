package claudecode

import (
	"os"

	"github.com/mananos/nxy.dev/v2/internal/host/claudecode/transcript"
)

// AgentFile reads a subagent's transcript file for app.AgentFiles.
type AgentFile struct{}

// Read parses the file at path as the transcript of agentID; ok is false when it cannot be read.
func (AgentFile) Read(path, agentID string) (transcript.FileResult, bool) {
	var data []byte
	ok := false
	if path != "" {
		var err error
		data, err = os.ReadFile(path)
		ok = err == nil
	}
	return transcript.ParseFile(path, data, ok, transcript.FileOpts{AgentID: &agentID}), ok
}
