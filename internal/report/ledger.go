package report

import (
	"math"
	"regexp"
	"strings"

	"github.com/mananos/nxy.dev/internal/jscompat"
)

// LedgerRow is one command of rtk's savings ledger (core/filter/ledger.mjs).
type LedgerRow struct {
	Ts      int64   `json:"ts"`      // epoch ms
	Project string  `json:"project"` // rtk's cwd when it ran, normalized
	Input   float64 `json:"input"`   // tokens the raw command produced
	Output  float64 `json:"output"`  // tokens rtk let through
	Saved   float64 `json:"saved"`   // input - output
}

// Savings are the totals of a set of ledger rows.
type Savings struct {
	Commands float64 `json:"commands"`
	Input    float64 `json:"input"`
	Output   float64 `json:"output"`
	Saved    float64 `json:"saved"`
}

var trailingSlashes = regexp.MustCompile(`/+$`)

// NormalizeProjectPath gives forward slashes, no Windows extended-length prefix and no trailing
// slash; case-folded when goos is "windows" (process.platform "win32" in the 1.x).
func NormalizeProjectPath(p, goos string) string {
	s := strings.TrimPrefix(p, `\\?\`)
	s = strings.ReplaceAll(s, `\`, "/")
	s = trailingSlashes.ReplaceAllString(s, "")
	if goos == "windows" {
		s = strings.ToLower(s)
	}
	return s
}

// UnderProject is true when rtk ran inside cwd (the same dir or a subdir).
func UnderProject(rowProject, cwd, goos string) bool {
	base := NormalizeProjectPath(cwd, goos)
	if base == "" {
		return true
	}
	return rowProject == base || strings.HasPrefix(rowProject, base+"/")
}

// SumSavings adds the rows in order.
func SumSavings(rows []LedgerRow) Savings {
	var s Savings
	for _, r := range rows {
		s.Commands++
		s.Input += r.Input
		s.Output += r.Output
		s.Saved += r.Saved
	}
	return s
}

// SavedPct is the percentage saved, or NaN (JS null) when nothing went through.
func SavedPct(s Savings) float64 {
	if s.Input != 0 && !math.IsNaN(s.Input) {
		return float64(s.Saved/s.Input) * 100
	}
	return math.NaN()
}

// Window is the part of a session that SessionSavings needs.
type Window struct {
	Cwd     string
	FirstTs *int64
	LastTs  *int64
}

// SessionSavings sums the rows inside the session's time window that ran under its cwd.
func SessionSavings(rows []LedgerRow, w Window, goos string) Savings {
	if w.FirstTs == nil || w.LastTs == nil {
		return Savings{}
	}
	lo := *w.FirstTs - 5_000
	hi := *w.LastTs + 60_000
	var in []LedgerRow
	for _, r := range rows {
		if r.Ts >= lo && r.Ts <= hi && UnderProject(r.Project, w.Cwd, goos) {
			in = append(in, r)
		}
	}
	return SumSavings(in)
}

// Day is one entry of DailyFromGain.
type Day struct {
	Date string
	Savings
}

// DailyFromGain reads `rtk gain -a --format json` into per-day totals, in the order the days
// first appear (a Map in the 1.x; a repeated date keeps its first position and the last values).
func DailyFromGain(gain any) []Day {
	var out []Day
	idx := map[string]int{}
	list, _ := jscompat.Get(gain, "daily").([]any)
	for _, d := range list {
		date := jscompat.Get(d, "date")
		if !jscompat.Truthy(date) {
			continue
		}
		num := func(k string) float64 {
			n := jscompat.ToNumber(jscompat.Get(d, k))
			if n == 0 || math.IsNaN(n) {
				return 0
			}
			return n
		}
		day := Day{Date: jscompat.String(date), Savings: Savings{Commands: num("commands"), Input: num("input_tokens"), Output: num("output_tokens"), Saved: num("saved_tokens")}}
		if i, ok := idx[day.Date]; ok {
			out[i] = day
			continue
		}
		idx[day.Date] = len(out)
		out = append(out, day)
	}
	return out
}
