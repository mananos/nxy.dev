package views

import (
	"sort"
	"strconv"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/metrics"
	"github.com/mananos/nxy.dev/v2/internal/report"
)

// TrendIn is the data of Tendencia. Loc is the zone the days are cut in (nil is UTC).
type TrendIn struct {
	Now      time.Time
	Loc      *time.Location
	Scope    string
	Sessions []report.TrendSession
	Runs     []domain.AgentRun
}

var (
	weekdaysEs = [7]string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"}
	monthsEs   = [12]string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"}
)

type period struct {
	row PeriodRow
	cw  float64
	cr  float64
	inp float64
}

// BuildTrend groups the sessions by local day (the last 7, every day has a row) and by week
// (the last 8, Monday to Sunday), sums the models and groups the agent runs by role.
func BuildTrend(in TrendIn) Trend {
	loc := in.Loc
	if loc == nil {
		loc = time.UTC
	}
	now := in.Now.In(loc)
	noon := func(d time.Time) float64 {
		return float64(time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, loc).UnixMilli())
	}

	days := make([]*period, 7)
	dayIdx := map[string]int{}
	for i := range days {
		d := now.AddDate(0, 0, i-6)
		days[i] = &period{row: PeriodRow{Label: weekdaysEs[d.Weekday()] + " " + two(d.Day())}}
		dayIdx[report.PeriodKey(noon(d), "day", loc)] = i
	}
	monday := now.AddDate(0, 0, -((int(now.Weekday()) + 6) % 7))
	weeks := make([]*period, 8)
	weekIdx := map[string]int{}
	for i := range weeks {
		d := monday.AddDate(0, 0, 7*(i-7))
		weeks[i] = &period{row: PeriodRow{Label: two(d.Day()) + " " + monthsEs[d.Month()-1]}}
		weekIdx[report.PeriodKey(noon(d), "week", loc)] = i
	}

	t := Trend{
		Scope:  in.Scope,
		Range:  TrendRange{Since: now.AddDate(0, 0, -6).Format("2006-01-02"), Until: now.Format("2006-01-02")},
		Days:   []PeriodRow{},
		Weeks:  []PeriodRow{},
		Models: []ModelRow{},
		Agents: []AgentRow{},
	}
	add := func(p *period, s report.TrendSession) {
		p.row.Sessions++
		p.row.Turns += s.Turns
		p.row.Tokens += int64(s.TotalTokens)
		p.row.USD += s.USD
		p.inp += s.Input
		p.cw += s.CacheW
		p.cr += s.CacheR
	}
	models := map[string]*ModelRow{}
	var modelOrder []string
	for _, s := range in.Sessions {
		if i, ok := dayIdx[report.PeriodKey(s.Ts, "day", loc)]; ok {
			add(days[i], s)
			t.Totals.Sessions++
			t.Totals.Turns += s.Turns
			t.Totals.Tokens += int64(s.TotalTokens)
			t.Totals.USD += s.USD
		}
		if i, ok := weekIdx[report.PeriodKey(s.Ts, "week", loc)]; ok {
			add(weeks[i], s)
		}
		for _, k := range s.ByModel.Keys {
			f := s.ByModel.Values[k]
			m := models[k]
			if m == nil {
				m = &ModelRow{Model: k}
				models[k] = m
				modelOrder = append(modelOrder, k)
			}
			addModel(m, f)
		}
	}
	for _, p := range days {
		t.Days = append(t.Days, finishPeriod(p))
	}
	for _, p := range weeks {
		t.Weeks = append(t.Weeks, finishPeriod(p))
	}
	for _, k := range modelOrder {
		m := models[k]
		if tot := m.Usage.Input + m.Usage.CacheWrite5m + m.Usage.CacheWrite1h + m.Usage.CacheRead; tot > 0 {
			m.CacheHitPct = m.Usage.CacheRead / tot * 100
		}
		t.Models = append(t.Models, *m)
	}
	sortModelRows(t.Models)

	byRole := map[string]*AgentRow{}
	timed := map[string]int{}
	total := map[string]int64{}
	for _, r := range in.Runs {
		a := byRole[r.Role]
		if a == nil {
			a = &AgentRow{Role: r.Role}
			byRole[r.Role] = a
		}
		a.Runs++
		a.USD += r.CostUSD
		if r.StartedAt > 0 && r.EndedAt >= r.StartedAt {
			timed[r.Role]++
			total[r.Role] += r.EndedAt - r.StartedAt
		}
	}
	for role, a := range byRole {
		if n := timed[role]; n > 0 {
			a.AvgMs = total[role] / int64(n)
		}
		t.Agents = append(t.Agents, *a)
	}
	sort.Slice(t.Agents, func(i, j int) bool {
		a, b := t.Agents[i], t.Agents[j]
		if a.USD != b.USD {
			return a.USD > b.USD
		}
		return a.Role < b.Role
	})
	return t
}

func two(n int) string {
	s := strconv.Itoa(n)
	if len(s) < 2 {
		return "0" + s
	}
	return s
}

func finishPeriod(p *period) PeriodRow {
	if tot := p.inp + p.cw + p.cr; tot > 0 {
		p.row.CacheHitPct = p.cr / tot * 100
	}
	return p.row
}

func addModel(m *ModelRow, f metrics.Finished) {
	m.Calls += f.Calls
	m.USD += f.USD
	m.Estimated = m.Estimated || f.USDEstimated
	u := &m.Usage
	u.Input += f.Usage.Input
	u.CacheWrite5m += f.Usage.CacheWrite5m
	u.CacheWrite1h += f.Usage.CacheWrite1h
	u.CacheRead += f.Usage.CacheRead
	u.Output += f.Usage.Output
	u.Thinking += f.Usage.Thinking
	if u.Speed == "" {
		u.Speed = f.Usage.Speed
	}
}
