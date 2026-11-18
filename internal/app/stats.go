package app

import (
	"strings"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/format"
	"github.com/mananos/nxy.dev/v2/internal/gate"
	"github.com/mananos/nxy.dev/v2/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/v2/internal/jscompat"
	"github.com/mananos/nxy.dev/v2/internal/report"
)

// Usage runs the read-only usage reports (`nxy stats`, `nxy trend`). Nothing here writes.
type Usage struct {
	Transcripts Transcripts
	Ledger      Ledger
	// FilterGroups is the filter summary of a session of the project at dir (empty when none).
	FilterGroups func(dir, sessionID string) []report.FilterGroup
	// Config is the merged config for a directory.
	Config func(dir string) map[string]any
	// BroadSearches are the main-thread broad searches of a transcript at or above threshold.
	BroadSearches func(path string, threshold float64) []report.BroadRow
	// Gain is `rtk gain --all` (plus `--project` when here) run in cwd; false when rtk is missing or fails.
	Gain func(here bool, cwd string) (any, bool)
	// Gate serves the gate lines of stats; nil means none.
	Gate    Gate
	Context ContextMeter
	Getenv  func(string) string
	Getwd   func() string
	Clock   func() time.Time
	Loc     *time.Location
	// GOOS is the operating system name (runtime.GOOS), for ledger path matching.
	GOOS string
}

// Stats is entries/stats.mjs: the session selector, the early exits, the filter and ledger reads.
func (u Usage) Stats(args []string) report.Output {
	d, _, dir, early := u.statsData(args)
	if early != nil {
		return *early
	}
	out := report.Stats(d)
	if !d.JSON && out.Code == 0 && u.Gate != nil {
		if rows := u.Gate.Rows(dir); len(rows) > 0 {
			if !strings.HasSuffix(out.Stdout, "\n") {
				out.Stdout += "\n"
			}
			out.Stdout += "\n" + strings.Join(gate.Summary(rows), "\n") + "\n"
		}
	}
	return out
}

// StatsData is the data of `nxy stats` for the session the args select: the numbers, the session
// and, when there is nothing to show (no session, no calls), the output that says so instead.
func (u Usage) StatsData(args []string) (report.StatsData, *transcript.SessionRef, *report.Output) {
	d, ref, _, early := u.statsData(args)
	return d, ref, early
}

func (u Usage) statsData(args []string) (report.StatsData, *transcript.SessionRef, string, *report.Output) {
	exit := func(o report.Output) (report.StatsData, *transcript.SessionRef, string, *report.Output) {
		return report.StatsData{}, nil, "", &o
	}
	a := format.ParseArgs(args)
	cwd := u.Getwd()
	if v, ok := a.Str("cwd"); ok {
		cwd = v
	}
	settings := report.SettingsFrom(u.Config(cwd))
	currentID := u.Getenv("CLAUDE_CODE_SESSION_ID")
	selector, ok := a.Str("session")
	if !ok {
		switch {
		case len(a.Positional) > 0 && a.Positional[0] != "":
			selector = a.Positional[0]
		case currentID != "":
			selector = "current"
		default:
			selector = "last"
		}
	}
	wantsCurrent := selector == "current"
	if wantsCurrent {
		selector = currentID
		if selector == "" {
			selector = "last"
		}
	}

	lookup := SessionLookup{Cwd: cwd, All: a.Flag("all"), ProjectsDir: settings.ProjectsDir}
	ref := u.Transcripts.ResolveSession(selector, lookup)
	if ref == nil {
		if wantsCurrent || (currentID != "" && selector == currentID) {
			// Brand-new session: Claude Code creates the transcript with the first API response.
			return exit(report.Output{Stdout: "nxy stats: esta sesión todavía no tiene llamadas a la API — la primera es la respuesta a este comando. Corré /nxy:stats de nuevo en el próximo turno, o `/nxy:stats last` para la sesión anterior.\n"})
		}
		return exit(report.Output{Stderr: "nxy: no encontré la sesión (" + selector + ") para " + cwd + ". Probá con --all o --session <id>.\n", Code: 1})
	}
	include := !a.Flag("no-agents")
	threshold := settings.CacheBreakThreshold
	s := u.Transcripts.ParseSession(*ref, transcript.SessionOpts{IncludeSubagents: &include, CacheBreakThreshold: &threshold, Timing: true})
	asJSON := a.Flag("json")
	if s.Usage.Calls == 0 && !asJSON {
		return exit(report.StatsNoCalls(s))
	}

	dir := cwd
	if jscompat.Truthy(s.Cwd) {
		dir = jscompat.String(s.Cwd)
	}
	d := report.StatsData{Session: s, Settings: settings, JSON: asJSON, Loc: u.Loc, Filter: report.SummarizeFilterGroups(u.FilterGroups(dir, s.SessionID))}

	// rtk's ledger: what the raw commands printed vs what the model read. Only rtk knows the raw side.
	if d.Filter.ByEngine.Get("rtk") != 0 {
		var until *int64
		if s.LastTs != nil {
			t := *s.LastTs + 60_000
			until = &t
		}
		if ledger, ok := u.Ledger.Rows(s.FirstTs, until); ok {
			sv := report.SessionSavings(ledger, report.Window{Cwd: report.TruthyString(s.Cwd), FirstTs: s.FirstTs, LastTs: s.LastTs}, u.GOOS)
			d.RtkSavings = &sv
		} else {
			d.LedgerWhy = u.Ledger.Unavailable()
			d.LedgerPath = u.Ledger.DBPath()
		}
	}

	// Main-thread broad searches past the gate threshold, read from this session's own transcript.
	d.Broad = report.SummarizeBroadSearches(u.BroadSearches(ref.Path, settings.GateContextTokens), settings.GateContextTokens)
	if asJSON {
		if ttl, ok := u.Transcripts.LastCacheTTL(ref.Path); ok {
			d.CacheTTL = &ttl
		}
	}
	return d, ref, dir, nil
}
