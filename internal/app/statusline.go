package app

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/v2/internal/jscompat"
	"github.com/mananos/nxy.dev/v2/internal/report"
)

// StatusState keeps what the statusline caches between renders. It is the only thing the
// statusline writes.
type StatusState interface {
	// Load is the saved cache of a session (a fresh one when there is none or it is unreadable).
	Load(session string) transcript.SessionCache
	Save(session string, c transcript.SessionCache) error
	// SaveInput keeps the last payload Claude Code sent, already formatted.
	SaveInput(data []byte) error
}

// Statusline is hosts/claude-code/statusline.mjs main(): the hot path. Its ports never spawn a
// process, open the db or call the filter engine.
type Statusline struct {
	// Config is the merged config for a directory.
	Config func(dir string) map[string]any
	Getwd  func() string
	State  StatusState
	// Fold adds the transcript bytes written since the last render and returns the subagent count.
	Fold func(transcriptPath string, c *transcript.SessionCache) int
	// GitBranch is the branch of the checkout of dir; false when unknown.
	GitBranch func(dir string) (string, bool)
	// NativePath turns a Git Bash path into the native one.
	NativePath func(p string) string
	// FileExists is false when the path is missing.
	FileExists func(p string) bool
	Clock      func() time.Time
	Getenv     func(string) string
}

// Run renders the statusline for the JSON Claude Code sent on stdin. Anything it cannot read
// renders as `?`; a failure prints "nxy ?" (the cause on stderr when NXY_DEBUG is set).
func (s Statusline) Run(stdin []byte) (out report.Output) {
	defer func() {
		if r := recover(); r != nil {
			out = report.Output{Stdout: "nxy ?"}
			if s.Getenv("NXY_DEBUG") != "" {
				out.Stderr = fmt.Sprintf("[nxy] %v\n", r)
			}
		}
	}()
	return report.Output{Stdout: s.render(stdin)}
}

func (s Statusline) render(stdin []byte) string {
	var input any = map[string]any{}
	pretty := []byte("{}")
	var parsed any
	if err := json.Unmarshal(stdin, &parsed); err == nil {
		if parsed == nil {
			panic("statusline input is null") // JS: null.cwd throws
		}
		input = parsed
		if p, err := jscompat.Restringify(stdin, "  "); err == nil {
			pretty = p
		}
	}
	cwd := s.Getwd()
	if c, ok := jscompat.Get(input, "cwd").(string); ok && c != "" {
		cwd = s.NativePath(c)
	}
	settings := report.SettingsFrom(s.Config(cwd))
	session := "unknown"
	if id, ok := jscompat.Get(input, "session_id").(string); ok {
		session = id
	}
	tpath, hasTranscript := jscompat.Get(input, "transcript_path").(string)
	if hasTranscript {
		tpath = s.NativePath(tpath)
	}

	cache := s.State.Load(session)
	_ = s.State.SaveInput(pretty) // optional: support aid
	agentCount := 0
	if hasTranscript && tpath != "" && s.FileExists(tpath) {
		agentCount = s.Fold(tpath, &cache)
		_ = s.State.Save(session, cache) // the cache is optional
	}
	branch, hasBranch := s.GitBranch(cwd)
	return report.RenderStatusline(input, cache, report.StatusCtx{
		AgentCount: agentCount, Branch: branch, HasBranch: hasBranch, Now: s.Clock().UnixMilli(),
	}, settings)
}
