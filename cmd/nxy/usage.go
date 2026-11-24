package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/app"
	"github.com/mananos/nxy.dev/v2/internal/host/claudecode"
	"github.com/mananos/nxy.dev/v2/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/v2/internal/jscompat"
	"github.com/mananos/nxy.dev/v2/internal/platform"
	"github.com/mananos/nxy.dev/v2/internal/report"
	"github.com/mananos/nxy.dev/v2/internal/store/sqlite"
	"github.com/mananos/nxy.dev/v2/internal/tools"
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

func (claudeTranscripts) Stamp(ref transcript.SessionRef) (string, float64) {
	return claudecode.SessionStamp(ref)
}

// rtkLedger is rtk's history database, read only.
type rtkLedger struct{ path string }

func (l rtkLedger) Rows(since, until *int64) ([]report.LedgerRow, bool) {
	return tools.ReadLedger(l.path, since, until)
}
func (l rtkLedger) Unavailable() string { return tools.LedgerUnavailable(l.path) }
func (l rtkLedger) DBPath() string      { return l.path }

func toAppRows(m map[string]sqlite.SessionUsageRow) map[string]app.SessionUsageRow {
	out := make(map[string]app.SessionUsageRow, len(m))
	for p, r := range m {
		out[p] = app.SessionUsageRow{Path: r.Path, SessionID: r.SessionID, Stamp: r.Stamp, MtimeMs: r.MtimeMs, Usage: r.Usage}
	}
	return out
}

// readSessionCache reads session_usage from nxy.db read only; Save does nothing.
type readSessionCache struct {
	rows func(paths []string) map[string]sqlite.SessionUsageRow
}

func (c readSessionCache) Rows(paths []string) map[string]app.SessionUsageRow {
	return toAppRows(c.rows(paths))
}
func (readSessionCache) Save([]app.SessionUsageRow) {}

// storeSessionCache reads and writes session_usage through the open store.
type storeSessionCache struct{ store *sqlite.Store }

func (c storeSessionCache) Rows(paths []string) map[string]app.SessionUsageRow {
	m, err := c.store.SessionUsageRows(context.Background(), paths)
	if err != nil {
		return nil
	}
	return toAppRows(m)
}

func (c storeSessionCache) Save(rows []app.SessionUsageRow) {
	out := make([]sqlite.SessionUsageRow, len(rows))
	for i, r := range rows {
		out[i] = sqlite.SessionUsageRow{Path: r.Path, SessionID: r.SessionID, Stamp: r.Stamp, MtimeMs: r.MtimeMs, Usage: r.Usage}
	}
	_ = c.store.SaveSessionUsage(context.Background(), out)
}

// newUsage wires the usage reports. It opens nxy.db read-only (lazily, never creating it); the
// returned func closes it.
func newUsage(getenv func(string) string, getwd func() string, clock func() time.Time, loc *time.Location) (app.Usage, func()) {
	var reader *sqlite.Reader
	var once sync.Once
	open := func() *sqlite.Reader {
		once.Do(func() {
			if userDir, err := platform.UserDirEnv(getenv); err == nil {
				if r, ok, err := sqlite.OpenReadOnly(context.Background(), filepath.Join(userDir, "nxy.db")); err == nil && ok {
					reader = r
				}
			}
		})
		return reader
	}
	closeDB := func() {
		if reader != nil {
			reader.Close()
			reader = nil
		}
	}
	filterGroups := func(dir, session string) []report.FilterGroup {
		r := open()
		if r == nil {
			return nil
		}
		gs, err := r.FilterGroups(context.Background(), platform.FilterCheckout(dir), session)
		if err != nil {
			return nil
		}
		// the db keeps 1 and "1" apart; the report folds them by their JS string, as the 1.x did
		return report.MergeFilterGroups(nil, gs)
	}
	filterGroupsMany := func(keys []app.FilterKey) map[app.FilterKey][]report.FilterGroup {
		out := make(map[app.FilterKey][]report.FilterGroup, len(keys))
		r := open()
		if r == nil || len(keys) == 0 {
			return out
		}
		sessions := make([]sqlite.FilterSessionKey, 0, len(keys))
		seen := map[sqlite.FilterSessionKey]bool{}
		for _, k := range keys {
			sk := sqlite.FilterSessionKey{Checkout: platform.FilterCheckout(k.Dir), Session: k.SessionID}
			if !seen[sk] {
				seen[sk] = true
				sessions = append(sessions, sk)
			}
		}
		all, err := r.FilterGroupsMany(context.Background(), sessions)
		if err != nil {
			return out
		}
		for _, k := range keys {
			if gs := all[sqlite.FilterSessionKey{Checkout: platform.FilterCheckout(k.Dir), Session: k.SessionID}]; len(gs) > 0 {
				out[k] = report.MergeFilterGroups(nil, gs)
			}
		}
		return out
	}
	return app.Usage{
		Transcripts:      claudeTranscripts{},
		Ledger:           rtkLedger{path: tools.RtkHistoryDBPath(runtime.GOOS, getenv)},
		FilterGroups:     filterGroups,
		FilterGroupsMany: filterGroupsMany,
		SessionCache: readSessionCache{rows: func(paths []string) map[string]sqlite.SessionUsageRow {
			r := open()
			if r == nil {
				return nil
			}
			m, err := r.SessionUsageRows(context.Background(), paths)
			if err != nil {
				return nil
			}
			return m
		}},
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
		Gate:    gateFiles{getenv: getenv},
		Context: claudeContext{},
		Getenv:  getenv,
		Getwd:   getwd,
		Clock:   clock,
		Loc:     loc,
		GOOS:    runtime.GOOS,
	}, closeDB
}

func osGetwd() string {
	d, err := os.Getwd()
	if err != nil {
		return "."
	}
	return d
}

func realUsage() (app.Usage, func()) { return newUsage(os.Getenv, osGetwd, time.Now, time.Local) }

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
	u, done := realUsage()
	defer done()
	return printOutput(u.Stats(args), stdout, stderr)
}

func runTrend(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	u, done := realUsage()
	defer done()
	return printOutput(u.Trend(args), stdout, stderr)
}
