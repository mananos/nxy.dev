package report

import (
	"math"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/goldentest"
	"github.com/mananos/nxy.dev/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/internal/metrics"
)

// platforms maps the process.platform the 1.x golden was generated with to GOOS.
var platforms = map[string]string{"win32": "windows", "linux": "linux"}

func forFile(t *testing.T, file string, fn func(t *testing.T, c goldentest.Case)) {
	t.Helper()
	n := 0
	for _, c := range goldentest.Load(t, "report") {
		if c.File != file {
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) { fn(t, c) })
	}
	if n == 0 {
		t.Fatalf("no cases in %s", file)
	}
}

func TestGoldenBreaks(t *testing.T) {
	forFile(t, "breaks.json", func(t *testing.T, c goldentest.Case) {
		var breaks []metrics.Break
		var totals []float64
		c.In(t, "breaks", &breaks)
		c.In(t, "totals", &totals)
		s := metrics.SummarizeBreaks(breaks)
		lines := make([][]string, len(totals))
		for i, total := range totals {
			lines[i] = FormatBreaks(s, total)
		}
		goldentest.Check(t, c, map[string]any{"lines": lines})
	})
}

func TestGoldenTTLLine(t *testing.T) {
	forFile(t, "ttlline.json", func(t *testing.T, c goldentest.Case) {
		var w metrics.TTLWhatIf
		c.In(t, "w", &w)
		var line *string
		if s, ok := FormatTTLWhatIf(w); ok {
			line = &s
		}
		goldentest.Check(t, c, map[string]any{"line": line})
	})
}

func TestGoldenTiming(t *testing.T) {
	forFile(t, "timing.json", func(t *testing.T, c goldentest.Case) {
		var summary *metrics.TimingSummary
		c.In(t, "summary", &summary)
		goldentest.Check(t, c, map[string]any{"lines": FormatAgentTiming(summary)})
	})
}

func TestGoldenBroad(t *testing.T) {
	forFile(t, "broad.json", func(t *testing.T, c goldentest.Case) {
		var rows []BroadRow
		var thresholds []float64
		c.In(t, "rows", &rows)
		c.In(t, "thresholds", &thresholds)
		out := make([]BroadSummary, len(thresholds))
		for i, th := range thresholds {
			out[i] = SummarizeBroadSearches(rows, th)
		}
		goldentest.Check(t, c, map[string]any{"summaries": out})
	})
}

func pctPtr(s Savings) *float64 {
	p := SavedPct(s)
	if math.IsNaN(p) {
		return nil
	}
	return &p
}

func TestGoldenLedger(t *testing.T) {
	forFile(t, "ledger.json", func(t *testing.T, c goldentest.Case) {
		switch {
		case c.Name == "norm":
			var paths []string
			c.In(t, "paths", &paths)
			got := map[string]any{}
			for js, goos := range platforms {
				out := make([]string, len(paths))
				for i, p := range paths {
					out[i] = NormalizeProjectPath(p, goos)
				}
				got[js] = out
			}
			goldentest.Check(t, c, got)
		case c.Name == "under":
			var pairs [][2]string
			c.In(t, "pairs", &pairs)
			got := map[string]any{}
			for js, goos := range platforms {
				out := make([]bool, len(pairs))
				for i, p := range pairs {
					out[i] = UnderProject(p[0], p[1], goos)
				}
				got[js] = out
			}
			goldentest.Check(t, c, got)
		case strings.HasPrefix(c.Name, "sum:"):
			var rows []LedgerRow
			c.In(t, "rows", &rows)
			s := SumSavings(rows)
			goldentest.Check(t, c, map[string]any{"sum": s, "pct": pctPtr(s)})
		default:
			var rows []LedgerRow
			var session struct {
				Cwd     *string
				FirstTs *int64
				LastTs  *int64
			}
			c.In(t, "rows", &rows)
			c.In(t, "session", &session)
			w := Window{FirstTs: session.FirstTs, LastTs: session.LastTs}
			if session.Cwd != nil {
				w.Cwd = *session.Cwd
			}
			got := map[string]any{}
			for js, goos := range platforms {
				got[js] = SessionSavings(rows, w, goos)
			}
			goldentest.Check(t, c, got)
		}
	})
}

func TestGoldenStatusline(t *testing.T) {
	bases := map[string]goldentest.Case{}
	for _, b := range goldentest.Load(t, "report") {
		if b.File == "statusline-bases.json" {
			bases[b.Name] = b
		}
	}
	var defaults map[string]any
	bases["defaults"].In(t, "statusline", &defaults)
	if defaults == nil {
		t.Fatal("statusline-bases.json has no defaults")
	}
	forFile(t, "statusline.json", func(t *testing.T, c goldentest.Case) {
		var name string
		var set map[string]any
		var unset []string
		var extra map[string]any
		c.In(t, "base", &name)
		c.In(t, "set", &set)
		c.In(t, "unset", &unset)
		c.In(t, "extra", &extra)
		base, ok := bases["base:"+name]
		if !ok {
			t.Fatalf("no base %q", name)
		}
		var input any
		var cache transcript.SessionCache
		var ctx struct {
			AgentCount int     `json:"agentCount"`
			Branch     *string `json:"branch"`
			Now        int64   `json:"now"`
		}
		base.In(t, "input", &input)
		base.In(t, "cache", &cache)
		base.In(t, "ctx", &ctx)
		sl := map[string]any{}
		for k, v := range defaults {
			sl[k] = v
		}
		for _, k := range unset {
			delete(sl, k)
		}
		for k, v := range set {
			sl[k] = v
		}
		metrics := map[string]any{"subscription": false, "statusline": sl}
		for k, v := range extra {
			metrics[k] = v
		}
		cfg := map[string]any{"metrics": metrics}
		sc := StatusCtx{AgentCount: ctx.AgentCount, Now: ctx.Now}
		if ctx.Branch != nil {
			sc.Branch, sc.HasBranch = *ctx.Branch, true
		}
		line := RenderStatusline(input, cache, sc, SettingsFrom(cfg))
		goldentest.Check(t, c, map[string]any{"line": line})
	})
}

func TestGoldenDailyFromGain(t *testing.T) {
	forFile(t, "dailyfromgain.json", func(t *testing.T, c goldentest.Case) {
		var gain any
		c.In(t, "gain", &gain)
		days := DailyFromGain(gain)
		pairs := make([]any, len(days))
		for i, d := range days {
			pairs[i] = []any{d.Date, d.Savings}
		}
		if pairs == nil {
			pairs = []any{}
		}
		goldentest.Check(t, c, map[string]any{"daily": pairs})
	})
}
