package app

import (
	"math"
	"strings"

	"github.com/mananos/nxy.dev/internal/format"
	"github.com/mananos/nxy.dev/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/internal/jscompat"
	"github.com/mananos/nxy.dev/internal/metrics"
	"github.com/mananos/nxy.dev/internal/report"
)

// Trend is entries/trend.mjs: the range, the session list, the filter and ledger reads.
func (u Usage) Trend(args []string) report.Output {
	a := format.ParseArgs(args)
	cwd := u.Getwd()
	if v, ok := a.Str("cwd"); ok {
		cwd = v
	}
	settings := report.SettingsFrom(u.Config(cwd))
	sinceText := "30d"
	if v, ok := a.Str("since"); ok {
		sinceText = v
	}
	var since *int64
	if ms, ok := format.ParseSince(sinceText, u.Clock().UnixMilli(), u.Loc); ok {
		since = &ms
	}
	by := "day"
	if v, ok := a.Str("by"); ok {
		by = v
	}
	switch by {
	case "day", "week", "session", "model", "agent":
	default:
		return report.Output{Stderr: "nxy: --by must be day | week | session | model | agent\n", Code: 1}
	}

	here := a.Flag("here")
	lookup := SessionLookup{Cwd: cwd, All: !here, ProjectsDir: settings.ProjectsDir}
	if since != nil {
		lookup.Since = *since
	}
	refs := u.Transcripts.ListSessions(lookup)
	d := report.TrendData{Since: since, By: by, Here: here, Cwd: cwd, JSON: a.Flag("json"), Settings: settings, Loc: u.Loc}
	if len(refs) == 0 {
		where := "any project"
		if here {
			where = cwd
		}
		return report.Output{Stdout: "nxy trend: no sessions since " + format.FmtDate(tsOrNaN(since), u.Loc, true) + " for " + where + ". Try a longer --since.\n"}
	}

	// filter.jsonl rows are per project cwd; cache them per cwd.
	filterCache := map[string][]any{}
	filterRowsFor := func(sessionCwd any, sessionID string) []any {
		dir := cwd
		if jscompat.Truthy(sessionCwd) {
			dir = jscompat.String(sessionCwd)
		}
		all, ok := filterCache[dir]
		if !ok {
			all = u.FilterRows(dir)
			filterCache[dir] = all
		}
		var out []any
		for _, r := range all {
			if jscompat.Get(r, "session_id") == any(sessionID) {
				out = append(out, r)
			}
		}
		return out
	}

	// rtk's ledger for the whole range, attributed to sessions by time window + cwd.
	ledger, ledgerOK := u.Ledger.Rows(since, nil)
	d.LedgerOK = ledgerOK
	if !ledgerOK && by == "day" && u.Gain != nil {
		if gain, ok := u.Gain(here, cwd); ok && jscompat.Truthy(gain) {
			d.HasGain = true
			d.GainDaily = report.DailyFromGain(gain) // a failed `rtk gain` must show `?`, not zeros
		}
	}

	threshold := settings.CacheBreakThreshold
	for _, ref := range refs {
		s := u.Transcripts.ParseSession(ref, transcript.SessionOpts{CacheBreakThreshold: &threshold, Timing: by == "agent"})
		if s.Usage.Calls == 0 {
			continue
		}
		if by == "agent" {
			for _, ag := range s.Agents {
				if ag.Timing != nil {
					d.AgentRuns = append(d.AgentRuns, metrics.Run{AgentType: ag.AgentType, Timing: ag.Timing})
				}
			}
			d.AgentSessions++
			continue
		}
		rows := filterRowsFor(s.Cwd, s.SessionID)
		nxy := len(rows) > 0
		for _, k := range s.BySkill.Keys {
			if strings.HasPrefix(k, "nxy:") {
				nxy = true
			}
		}
		rtkCalls := 0
		for _, r := range rows {
			if jscompat.Get(r, "engine") == any("rtk") {
				rtkCalls++
			}
		}
		ts := ref.MtimeMs
		if s.FirstTs != nil {
			ts = float64(*s.FirstTs)
		}
		var saved *float64
		switch {
		case ledgerOK && rtkCalls != 0:
			v := report.SessionSavings(ledger, report.Window{Cwd: report.TruthyString(s.Cwd), FirstTs: s.FirstTs, LastTs: s.LastTs}, u.GOOS).Saved
			saved = &v
		case ledgerOK:
			zero := 0.0
			saved = &zero
		}
		prompt := ""
		if len(s.Prompts) > 0 {
			prompt = s.Prompts[0].Text
		}
		us := s.Usage.Usage
		d.Sessions = append(d.Sessions, report.TrendSession{
			SessionID: s.SessionID, Project: s.Project, Ts: ts, Turns: s.Turns, Calls: s.Usage.Calls,
			MainCalls: s.Main.Calls, MainInput: s.Main.TotalInput, MainEdits: len(s.Tools.FilesEditedMain),
			RtkSaved: saved, Input: us.Input, CacheW: us.CacheWrite5m + us.CacheWrite1h, CacheR: us.CacheRead, Output: us.Output,
			TotalTokens: s.Usage.TotalInput + us.Output, USD: s.Usage.USD, USDPartial: s.Usage.USDPartial,
			UnknownModels: s.Usage.UnknownModels, USDEstimated: s.Usage.USDEstimated, EstimatedModels: s.Usage.EstimatedModels,
			AgentTokens: s.Subagents.TotalInput + s.Subagents.Usage.Output, Agents: len(s.Agents),
			BashLines: s.Tools.Bash.ResultLines, BashCalls: s.Tools.Bash.Calls, RtkCalls: rtkCalls, WallMs: s.WallMs,
			Nxy: nxy, FirstPrompt: prompt, ByModel: s.ByModel, CacheBreaks: s.CacheBreaks, TTLWhatIf: s.TTLWhatIf,
		})
	}
	return report.Trend(d)
}

// tsOrNaN is a nullable epoch ms as the float the formatters take (NaN is JS null).
func tsOrNaN(p *int64) float64 {
	if p == nil {
		return math.NaN()
	}
	return float64(*p)
}
