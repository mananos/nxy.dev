package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/app"
	"github.com/mananos/nxy.dev/internal/host/claudecode"
	"github.com/mananos/nxy.dev/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/internal/jscompat"
	"github.com/mananos/nxy.dev/internal/platform"
	"github.com/mananos/nxy.dev/internal/report"
	"github.com/mananos/nxy.dev/internal/tools"
)

// claudeTranscripts reads Claude Code's session transcripts.
type claudeTranscripts struct{}

func listOpts(o app.SessionLookup) claudecode.ListOpts {
	return claudecode.ListOpts{Cwd: o.Cwd, All: o.All, ProjectsDir: o.ProjectsDir, Since: o.Since}
}

func (claudeTranscripts) ResolveSession(selector string, o app.SessionLookup) *transcript.SessionRef {
	return claudecode.ResolveSession(selector, listOpts(o))
}

func (claudeTranscripts) ListSessions(o app.SessionLookup) []transcript.SessionRef {
	return claudecode.ListSessions(listOpts(o))
}

func (claudeTranscripts) ParseSession(ref transcript.SessionRef, o transcript.SessionOpts) transcript.Session {
	return claudecode.ParseSession(ref, o)
}

func (claudeTranscripts) LastCacheTTL(path string) (string, bool) {
	return claudecode.LastCacheTTL(path, 0)
}

// rtkLedger is rtk's history database, read only.
type rtkLedger struct{ path string }

func (l rtkLedger) Rows(since, until *int64) ([]report.LedgerRow, bool) {
	return tools.ReadLedger(l.path, since, until)
}
func (l rtkLedger) Unavailable() string { return tools.LedgerUnavailable(l.path) }
func (l rtkLedger) DBPath() string      { return l.path }

// newUsage wires the usage reports. It never opens nxy.db.
func newUsage(getenv func(string) string, getwd func() string, clock func() time.Time, loc *time.Location) app.Usage {
	return app.Usage{
		Transcripts: claudeTranscripts{},
		Ledger:      rtkLedger{path: tools.RtkHistoryDBPath(runtime.GOOS, getenv)},
		FilterRows: func(dir string) []any {
			return platform.ReadJSONL(filepath.Join(platform.RuntimeDir(dir), "metrics", "filter.jsonl"))
		},
		Config:        func(dir string) map[string]any { return platform.LoadConfig(dir, getenv) },
		BroadSearches: claudecode.BroadSearchesInTranscript,
		Gain: func(here bool, cwd string) (any, bool) {
			info := filterEngine{tools.Engine{Getenv: getenv, Now: clock}}.Info(platform.LoadFilterSettings(cwd, getenv), cwd)
			if info.RtkPath == "" {
				return nil, false
			}
			args := []string{"--all"}
			if here {
				args = append(args, "--project")
			}
			return tools.Gain(info.RtkPath, args, cwd)
		},
		Getenv: getenv,
		Getwd:  getwd,
		Clock:  clock,
		Loc:    loc,
		GOOS:   runtime.GOOS,
	}
}

func osGetwd() string {
	d, err := os.Getwd()
	if err != nil {
		return "."
	}
	return d
}

func realUsage() app.Usage { return newUsage(os.Getenv, osGetwd, time.Now, time.Local) }

func printOutput(o report.Output, stdout, stderr io.Writer) int {
	io.WriteString(stdout, o.Stdout)
	io.WriteString(stderr, o.Stderr)
	return o.Code
}

// statusState keeps the statusline cache under <UserDir>/cache: one file per session in
// statusline-v2/ and the last payload in statusline-v2-input.json. The 1.x files are never touched.
type statusState struct {
	dir   string // <UserDir>/cache
	clock func() time.Time
}

// staleAfter is how old a session state file must be to be removed.
const staleAfter = 7 * 24 * time.Hour

// stateFileName is the session id with every UTF-16 unit outside [A-Za-z0-9_-] as "_" (as the 1.x).
func stateFileName(session string) string {
	var b strings.Builder
	for _, r := range session {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		case r > 0xFFFF:
			b.WriteString("__")
		default:
			b.WriteByte('_')
		}
	}
	return b.String() + ".json"
}

func (s statusState) sessionDir() string { return filepath.Join(s.dir, "statusline-v2") }

func (s statusState) Load(session string) transcript.SessionCache {
	if s.dir == "" {
		return transcript.NewSessionCache()
	}
	data, err := os.ReadFile(filepath.Join(s.sessionDir(), stateFileName(session)))
	if err != nil {
		return transcript.NewSessionCache()
	}
	return transcript.ParseSessionCache(data)
}

func (s statusState) Save(session string, c transcript.SessionCache) error {
	if s.dir == "" {
		return nil
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	text, err := jscompat.Restringify(raw, "")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.sessionDir(), 0o755); err != nil {
		return err
	}
	path := filepath.Join(s.sessionDir(), stateFileName(session))
	_, statErr := os.Stat(path)
	if err := platform.WriteAtomic(path, text); err != nil {
		return err
	}
	if statErr != nil {
		s.cleanup(path) // a new session: drop the states nobody has touched for a week
	}
	return nil
}

// cleanup removes the files of the state directory older than staleAfter, except keep.
func (s statusState) cleanup(keep string) {
	entries, err := os.ReadDir(s.sessionDir())
	if err != nil {
		return
	}
	cutoff := s.clock().Add(-staleAfter)
	for _, e := range entries {
		p := filepath.Join(s.sessionDir(), e.Name())
		if p == keep || e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(p)
		}
	}
}

func (s statusState) SaveInput(data []byte) error {
	if s.dir == "" {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	return platform.WriteAtomic(filepath.Join(s.dir, "statusline-v2-input.json"), data)
}

// newStatusline wires the statusline. It never spawns a process, opens nxy.db or calls tools.Engine.
func newStatusline(getenv func(string) string, getwd func() string, clock func() time.Time) app.Statusline {
	stateDir := ""
	if userDir, err := platform.UserDirEnv(getenv); err == nil {
		stateDir = filepath.Join(userDir, "cache")
	} // no home: render without state
	return app.Statusline{
		Config:     func(dir string) map[string]any { return platform.LoadConfig(dir, getenv) },
		Getwd:      getwd,
		State:      statusState{dir: stateDir, clock: clock},
		Fold:       claudecode.FoldStatusline,
		GitBranch:  platform.GitBranch,
		NativePath: platform.NativePath,
		FileExists: func(p string) bool { _, err := os.Stat(p); return err == nil },
		Clock:      clock,
		Getenv:     getenv,
	}
}

func runStatusline(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "setup" {
		return runStatuslineSetup(args[1:], stdout, stderr)
	}
	data, _ := io.ReadAll(stdin)
	return printOutput(newStatusline(os.Getenv, osGetwd, time.Now).Run(data), stdout, stderr)
}

func runStats(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	return printOutput(realUsage().Stats(args), stdout, stderr)
}

func runTrend(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	return printOutput(realUsage().Trend(args), stdout, stderr)
}
