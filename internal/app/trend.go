package app

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/format"
	"github.com/mananos/nxy.dev/v2/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/v2/internal/jscompat"
	"github.com/mananos/nxy.dev/v2/internal/metrics"
	"github.com/mananos/nxy.dev/v2/internal/report"
	"github.com/mananos/nxy.dev/v2/internal/version"
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

	// rtk's ledger for the whole range, attributed to sessions by time window + cwd.
	ledger, ledgerOK := u.Ledger.Rows(since, nil)
	d.LedgerOK = ledgerOK

	threshold := settings.CacheBreakThreshold
	if by == "agent" {
		for _, ref := range refs {
			s := u.Transcripts.ParseSession(ref, transcript.SessionOpts{CacheBreakThreshold: &threshold, Timing: true})
			if s.Usage.Calls == 0 {
				continue
			}
			for _, ag := range s.Agents {
				if ag.Timing != nil {
					d.AgentRuns = append(d.AgentRuns, metrics.Run{AgentType: ag.AgentType, Timing: ag.Timing})
				}
			}
			d.AgentSessions++
		}
		return len(refs)
	}

	// 1st: the summary of each session, from its stored row when the signature is the same.
	type read struct {
		ref transcript.SessionRef
		us  report.SessionUsage
	}
	var stored map[string]SessionUsageRow
	if u.SessionCache != nil {
		paths := make([]string, len(refs))
		for i, ref := range refs {
			paths[i] = ref.Path
		}
		stored = u.SessionCache.Rows(paths)
	}
	nowMs := time.Now().UnixMilli()
	if u.Clock != nil {
		nowMs = u.Clock().UnixMilli()
	}
	var toSave []SessionUsageRow
	reads := make([]read, 0, len(refs))
	for _, ref := range refs {
		var us report.SessionUsage
		if u.SessionCache == nil {
			us = sessionUsageOf(ref, u.Transcripts.ParseSession(ref, transcript.SessionOpts{CacheBreakThreshold: &threshold}))
		} else {
			stamp, newest := u.Transcripts.Stamp(ref)
			stamp += "|t=" + strconv.FormatFloat(threshold, 'g', -1, 64) + "|v=" + version.Version
			if row, ok := stored[ref.Path]; ok && row.Stamp == stamp {
				us = row.Usage
			} else {
				us = sessionUsageOf(ref, u.Transcripts.ParseSession(ref, transcript.SessionOpts{CacheBreakThreshold: &threshold}))
				if float64(nowMs)-newest >= quietMs {
					toSave = append(toSave, SessionUsageRow{Path: ref.Path, SessionID: ref.SessionID, Stamp: stamp, MtimeMs: int64(newest), Usage: us})
				}
			}
		}
		if us.Calls == 0 {
			continue
		}
		reads = append(reads, read{ref, us})
	}
	if len(toSave) > 0 {
		u.SessionCache.Save(toSave)
	}

	// 2nd: the filter summary of every session in one query.
	dirOf := func(us report.SessionUsage) string {
		if us.Cwd != "" {
			return us.Cwd
		}
		return cwd
	}
	var keys []FilterKey
	seen := map[FilterKey]bool{}
	for _, r := range reads {
		k := FilterKey{dirOf(r.us), r.us.SessionID}
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	var filters map[FilterKey][]report.FilterGroup
	if u.FilterGroupsMany != nil {
		filters = u.FilterGroupsMany(keys)
	} else {
		filters = make(map[FilterKey][]report.FilterGroup, len(keys))
		for _, k := range keys {
			filters[k] = u.FilterGroups(k.Dir, k.SessionID)
		}
	}

	for _, r := range reads {
		us := r.us
		groups := filters[FilterKey{dirOf(us), us.SessionID}]
		nxy := report.FilterGroupsRows(groups) > 0 || us.UsedNxySkill
		rtkCalls := report.FilterGroupsRTK(groups)
		var firstTs, lastTs *int64
		if us.FirstTs != 0 {
			v := int64(us.FirstTs)
			firstTs = &v
		}
		if us.LastTs != 0 {
			v := int64(us.LastTs)
			lastTs = &v
		}
		var saved *float64
		switch {
		case ledgerOK && rtkCalls != 0:
			v := report.SessionSavings(ledger, report.Window{Cwd: us.Cwd, FirstTs: firstTs, LastTs: lastTs}, u.GOOS).Saved
			saved = &v
		case ledgerOK:
			zero := 0.0
			saved = &zero
		}
		d.Sessions = append(d.Sessions, report.TrendSession{
			SessionID: us.SessionID, Project: us.Project, Ts: us.Ts, Turns: us.Turns, Calls: us.Calls,
			MainCalls: us.MainCalls, MainInput: us.MainInput, MainEdits: us.MainEdits,
			RtkSaved: saved, Input: us.Input, CacheW: us.CacheW, CacheR: us.CacheR, Output: us.Output,
			TotalTokens: us.TotalTokens, USD: us.USD, USDPartial: us.USDPartial,
			UnknownModels: us.UnknownModels, USDEstimated: us.USDEstimated, EstimatedModels: us.EstimatedModels,
			AgentTokens: us.AgentTokens, Agents: us.Agents,
			BashLines: us.BashLines, BashCalls: us.BashCalls, RtkCalls: rtkCalls, WallMs: us.WallMs,
			Nxy: nxy, FirstPrompt: us.FirstPrompt, ByModel: us.ByModel, CacheBreaks: us.CacheBreaks, TTLWhatIf: us.TTLWhatIf,
		})
	}
	return len(refs)
}

// quietMs is how long a session has to be untouched before its summary is stored.
const quietMs = 10 * 60_000

// sessionUsageOf is what is kept of a parsed session.
func sessionUsageOf(ref transcript.SessionRef, s transcript.Session) report.SessionUsage {
	ts := ref.MtimeMs
	var firstTs, lastTs float64
	if s.FirstTs != nil {
		ts = float64(*s.FirstTs)
		firstTs = ts
	}
	if s.LastTs != nil {
		lastTs = float64(*s.LastTs)
	}
	prompt := ""
	if len(s.Prompts) > 0 {
		prompt = s.Prompts[0].Text
	}
	nxySkill := false
	for _, k := range s.BySkill.Keys {
		if strings.HasPrefix(k, "nxy:") {
			nxySkill = true
		}
	}
	us := s.Usage.Usage
	return report.SessionUsage{
		SessionID: s.SessionID, Project: s.Project, Ts: ts, Turns: s.Turns, Calls: s.Usage.Calls,
		MainCalls: s.Main.Calls, MainInput: s.Main.TotalInput, MainEdits: len(s.Tools.FilesEditedMain),
		Input: us.Input, CacheW: us.CacheWrite5m + us.CacheWrite1h, CacheR: us.CacheRead, Output: us.Output,
		TotalTokens: s.Usage.TotalInput + us.Output, USD: s.Usage.USD, USDPartial: s.Usage.USDPartial,
		UnknownModels: s.Usage.UnknownModels, USDEstimated: s.Usage.USDEstimated, EstimatedModels: s.Usage.EstimatedModels,
		AgentTokens: s.Subagents.TotalInput + s.Subagents.Usage.Output, Agents: len(s.Agents),
		BashLines: s.Tools.Bash.ResultLines, BashCalls: s.Tools.Bash.Calls, WallMs: s.WallMs,
		FirstPrompt: prompt, ByModel: s.ByModel, CacheBreaks: s.CacheBreaks, TTLWhatIf: s.TTLWhatIf,
		Cwd: report.TruthyString(s.Cwd), FirstTs: firstTs, LastTs: lastTs, UsedNxySkill: nxySkill,
	}
}

// tsOrNaN is a nullable epoch ms as the float the formatters take (NaN is JS null).
func tsOrNaN(p *int64) float64 {
	if p == nil {
		return math.NaN()
	}
	return float64(*p)
}
