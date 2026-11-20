package app

import (
	"math"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/format"
	"github.com/mananos/nxy.dev/v2/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/v2/internal/jscompat"
	"github.com/mananos/nxy.dev/v2/internal/metrics"
	"github.com/mananos/nxy.dev/v2/internal/report"
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
		return report.Output{Stderr: "nxy: --by tiene que ser day | week | session | model | agent\n", Code: 1}
	}

	here := a.Flag("here")
	d := report.TrendData{Since: since, By: by, Here: here, Cwd: cwd, JSON: a.Flag("json"), Settings: settings, Loc: u.Loc}
	nrefs := u.readTrend(&d, cwd, since)
	if nrefs == 0 {
		where := "cualquier proyecto"
		if here {
			where = cwd
		}
		return report.Output{Stdout: "nxy trend: no hay sesiones desde " + format.FmtDate(tsOrNaN(since), u.Loc, true) + " para " + where + ". Probá con un --since más largo.\n"}
	}
	if !d.LedgerOK && by == "day" && u.Gain != nil {
		if gain, ok := u.Gain(here, cwd); ok && jscompat.Truthy(gain) {
			d.HasGain = true
			d.GainDaily = report.DailyFromGain(gain) // a failed `rtk gain` must show `?`, not zeros
		}
	}
	return report.Trend(d)
}

// TrendSessions are the sessions of the days since (nil is all), per session as `nxy trend` counts
// them; here limits them to the project of cwd. until (nil is no bound) drops the sessions that
// started after it.
func (u Usage) TrendSessions(since, until *int64, here bool, cwd string) []report.TrendSession {
	d := report.TrendData{Since: since, By: "day", Here: here, Cwd: cwd, Settings: report.SettingsFrom(u.Config(cwd)), Loc: u.Loc}
	u.readTrend(&d, cwd, since)
	if until == nil {
		return d.Sessions
	}
	kept := d.Sessions[:0]
	for _, s := range d.Sessions {
		if s.Ts <= float64(*until) {
			kept = append(kept, s)
		}
	}
	return kept
}

// readTrend lists the sessions in range and fills d (sessions, or agent runs for by agent, and
// whether rtk's ledger was read). It returns how many sessions it found.
func (u Usage) readTrend(d *report.TrendData, cwd string, since *int64) int {
	by, here, settings := d.By, d.Here, d.Settings
	lookup := SessionLookup{Cwd: cwd, All: !here, ProjectsDir: settings.ProjectsDir}
	if since != nil {
		lookup.Since = *since
	}
	refs := u.Transcripts.ListSessions(lookup)
	if len(refs) == 0 {
		return 0
	}

	// The filter summary is per project cwd and session; cache it per pair.
	filterCache := map[[2]string][]report.FilterGroup{}
	filterGroupsFor := func(sessionCwd any, sessionID string) []report.FilterGroup {
		dir := cwd
		if jscompat.Truthy(sessionCwd) {
			dir = jscompat.String(sessionCwd)
		}
		key := [2]string{dir, sessionID}
		gs, ok := filterCache[key]
		if !ok {
			gs = u.FilterGroups(dir, sessionID)
			filterCache[key] = gs
		}
		return gs
	}

	// rtk's ledger for the whole range, attributed to sessions by time window + cwd.
	ledger, ledgerOK := u.Ledger.Rows(since, nil)
	d.LedgerOK = ledgerOK

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
		groups := filterGroupsFor(s.Cwd, s.SessionID)
		nxy := report.FilterGroupsRows(groups) > 0
		for _, k := range s.BySkill.Keys {
			if strings.HasPrefix(k, "nxy:") {
				nxy = true
			}
		}
		rtkCalls := report.FilterGroupsRTK(groups)
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
	return len(refs)
}

// tsOrNaN is a nullable epoch ms as the float the formatters take (NaN is JS null).
func tsOrNaN(p *int64) float64 {
	if p == nil {
		return math.NaN()
	}
	return float64(*p)
}
