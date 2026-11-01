package report

import (
	"math"
	"strings"

	"github.com/mananos/nxy.dev/internal/format"
	"github.com/mananos/nxy.dev/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/internal/jscompat"
	"github.com/mananos/nxy.dev/internal/pricing"
)

// Presets is a palette per role (PRESETS of hosts/claude-code/statusline.mjs). `label` and `value`
// are appended to a segment's own style.
var Presets = map[string]map[string]string{
	"classic": {
		"brand": "bold 80", "separator": "dim", "path": "muted", "branch": "bold", "model": "text", "effort": "110",
		"where": "", "ctx": "", "turn": "", "session": "", "cache": "", "limits": "",
		"label": "muted", "value": "bold", "gauge": "80", "gaugeEmpty": "dim", "warn": "bold yellow", "crit": "bold red",
		"pad": "", "gap": " ",
	},
	"vivid": {
		"brand": "bold 213", "separator": "dim", "path": "114", "branch": "bold 114", "model": "141", "effort": "bold 141",
		"where": "114", "ctx": "75", "turn": "252", "session": "80", "cache": "176", "limits": "245",
		"label": "", "value": "bold", "gauge": "75", "gaugeEmpty": "dim", "warn": "bold 214", "crit": "bold 203",
		"pad": "", "gap": " ",
	},
	"powerline": {
		"brand": "bold 16 bg:80", "separator": "", "path": "253 bg:238", "branch": "bold 255 bg:238", "model": "253 bg:60", "effort": "bold 255 bg:60",
		"where": "bg:238", "ctx": "253 bg:24", "turn": "253 bg:94", "session": "253 bg:23", "cache": "253 bg:53", "limits": "250 bg:236",
		"label": "", "value": "bold", "gauge": "117", "gaugeEmpty": "245", "warn": "bold 220", "crit": "bold 203",
		"pad": " ", "gap": " ",
	},
}

// StatusCtx is what the renderer gets besides the input and the cache.
type StatusCtx struct {
	AgentCount int
	Branch     string
	HasBranch  bool  // false: the branch is unknown (null)
	Now        int64 // ms
}

type painter struct {
	seg   string
	theme map[string]string
	color bool
}

func (p painter) hue() string { return p.theme[p.seg] }

func (p painter) label(t string) string {
	return format.Colorize(t, p.hue()+" "+p.theme["label"], p.color)
}

func (p painter) styled(t, sev, role string) string {
	if sev != "" {
		return format.Colorize(t, p.hue()+" "+p.theme[sev], p.color)
	}
	return format.Colorize(t, p.hue()+" "+p.theme[role], p.color)
}

func (p painter) value(t, sev string) string         { return p.styled(t, sev, "value") }
func (p painter) role(r, t, sev string) string       { return p.styled(t, sev, r) }
func (p painter) raw(t string) string                { return format.Colorize(t, p.hue(), p.color) }
func (p painter) dot() string                        { return p.label(" · ") }
func numField(v any, keys ...string) (float64, bool) { return jscompat.Num(jscompat.Get(v, keys...)) }

// RenderStatusline is the pure renderer of statusline.mjs: everything that needs I/O is already in
// input (Claude Code's JSON, decoded), the cache and ctx.
func RenderStatusline(input any, c transcript.SessionCache, ctx StatusCtx, s Settings) string {
	sl := s.Statusline
	color := sl.Color
	preset, ok := Presets[sl.Preset]
	if !ok {
		preset = Presets["classic"]
	}
	theme := make(map[string]string, len(preset)+len(sl.Theme))
	for k, v := range preset {
		theme[k] = v
	}
	for k, v := range sl.Theme {
		theme[k] = v
	}
	twoLine := sl.Layout == "two-line"
	cells := 8
	if twoLine {
		cells = 16
	}
	paint := func(seg string) painter { return painter{seg: seg, theme: theme, color: color} }
	wrap := func(seg, text string) string {
		if theme["pad"] == "" {
			return text
		}
		pad := format.Colorize(theme["pad"], theme[seg], color)
		return pad + text + pad
	}

	main := c.Main
	if main == nil {
		main = transcript.NewIncrementalState()
	}
	states := []*transcript.IncrementalState{main}
	c.Agents.Each(func(_ string, st *transcript.IncrementalState) { states = append(states, st) })
	var tokensIn, tokensOut, usdSum float64
	usdPartial, usdEstimated, haveData := false, false, false
	for _, st := range states {
		tokensIn += pricing.TotalInput(st.Usage)
		tokensOut += st.Usage.Output
		usdSum += st.Usd
		if st.UsdUnknown > 0 {
			usdPartial = true
		}
		if st.UsdEstimated != nil && *st.UsdEstimated > 0 {
			usdEstimated = true
		}
		if st.Calls > 0 {
			haveData = true
		}
	}
	sub := ""
	if s.Subscription {
		sub = "~"
	}
	var modelIDv any = jscompat.Undefined
	if v := jscompat.Get(input, "model", "id"); jscompat.Truthy(v) {
		modelIDv = v
	} else if main.Model != nil && *main.Model != "" {
		modelIDv = *main.Model
	}
	modelID, modelIsString := modelIDv.(string)
	if !jscompat.IsUndefined(modelIDv) && !modelIsString {
		modelID = jscompat.String(modelIDv)
	}

	// brand, where, model
	brand := ""
	if sl.Brand != "" {
		brand = wrap("brand", format.Colorize(sl.Brand, theme["brand"], color))
	}
	where := ""
	if cwd, ok := jscompat.Get(input, "cwd").(string); ok && cwd != "" {
		pw := paint("where")
		dirName := cwd
		parts := strings.FieldsFunc(cwd, func(r rune) bool { return r == '/' || r == '\\' })
		if len(parts) > 0 {
			dirName = parts[len(parts)-1]
		}
		branch := ""
		if ctx.HasBranch && ctx.Branch != "" {
			branch = pw.raw(" ") + pw.role("branch", ctx.Branch, "")
		}
		where = wrap("where", pw.role("path", dirName, "")+branch)
	}
	pm := paint("model")
	var modelName any = jscompat.Or(jscompat.Get(input, "model", "display_name"), modelIDv, "?")
	modelStr := jscompat.String(modelName)
	effortSeg := ""
	if e := jscompat.Get(input, "effort", "level"); jscompat.Truthy(e) {
		effortSeg = pm.dot() + pm.role("effort", jscompat.String(e), "")
	}
	modelSeg := wrap("model", pm.role("model", modelStr, "")+effortSeg)

	// context: gauge, %, absolute tokens, severity by tokens
	ctxPct, hasPct := numField(input, "context_window", "used_percentage")
	ctxTokens, hasTokens := numField(input, "context_window", "total_input_tokens")
	if size, ok := numField(input, "context_window", "context_window_size"); !hasTokens && hasPct && ok {
		ctxTokens, hasTokens = jscompat.Round(float64((ctxPct/100)*size)), true
	}
	tokensOrNaN := math.NaN()
	if hasTokens {
		tokensOrNaN = ctxTokens
	}
	pc := paint("ctx")
	ctxSeg := pc.label("ctx") + pc.raw(" ") + pc.value("?%", "")
	if hasPct {
		sev := format.Severity(tokensOrNaN, sl.CtxWarnTokens, sl.CtxCritTokens)
		bar := format.Gauge(ctxPct, cells)
		filled := strings.TrimRight(bar, "▱")
		painted := pc.role("gauge", filled, sev) + pc.role("gaugeEmpty", bar[len(filled):], "")
		abs := ""
		if hasTokens {
			abs = pc.dot() + pc.value(format.FmtTokens(ctxTokens), sev)
		}
		ctxSeg = pc.label("ctx") + pc.raw(" ") + painted + pc.raw(" ") + pc.value(jscompat.String(jscompat.Round(ctxPct))+"%", sev) + abs
	}
	ctxSeg = wrap("ctx", ctxSeg)

	// current turn
	pt := paint("turn")
	turnSeg := ""
	if main.Turns > 0 && haveData {
		var turnUsd, turnUnknown float64
		for _, st := range states {
			turnUsd += st.TurnUsd
			turnUnknown += float64(st.TurnUsdUnknown)
		}
		sev := ""
		if turnUnknown <= 0 {
			sev = format.Severity(turnUsd, sl.TurnWarnUsd, sl.TurnCritUsd)
		}
		turnSeg = wrap("turn", pt.label("turno")+pt.raw(" ")+pt.value(format.FmtUsdPartial(turnUsd, turnUnknown > 0, usdEstimated)+sub, sev))
	}

	// session
	ps := paint("session")
	agents := ""
	if ctx.AgentCount != 0 {
		plural := ""
		if ctx.AgentCount > 1 {
			plural = "s"
		}
		agents = ps.dot() + ps.label("+"+jscompat.String(float64(ctx.AgentCount))+" agent"+plural)
	}
	var sessionSeg string
	if haveData {
		sessionSeg = wrap("session", ps.label("sesión")+ps.raw(" ")+ps.value(format.FmtUsdPartial(usdSum, usdPartial, usdEstimated)+sub, "")+ps.dot()+
			ps.value(format.FmtTokens(tokensIn)+"→"+format.FmtTokens(tokensOut), "")+agents)
	} else {
		sessionSeg = wrap("session", ps.label("no calls yet"))
	}

	// prompt cache
	pk := paint("cache")
	pcache := jscompat.Get(input, "prompt_cache")
	now := float64(ctx.Now)
	nowSec := now / 1000
	cold := false
	idleMs, hasIdle := 0.0, false
	expires, hasExpires := numField(pcache, "expires_at")
	if warm, isBool := jscompat.Get(pcache, "warm").(bool); isBool {
		cold = !warm && jscompat.Get(pcache, "caching_observed") != false
		if hasExpires {
			idleMs, hasIdle = math.Max(0, now-float64(expires*1000)), true
		}
	} else if main.LastTs != nil {
		idleMs, hasIdle = now-float64(*main.LastTs), true
		cold = idleMs >= float64(math.Max(0, sl.PromptCacheTtlMin)*60_000)
	}
	cacheSeg := ""
	if cold {
		recache, hasRecache := numField(pcache, "recache_tokens_if_cold")
		if !hasRecache {
			recache, hasRecache = ctxTokens, hasTokens
		}
		var parts []string
		if hasRecache {
			parts = append(parts, pk.value("❄"+format.FmtTokens(recache), "crit"))
			var u pricing.Usage
			if jscompat.Get(pcache, "ttl") == "1h" {
				u.CacheWrite1h = recache
			} else {
				u.CacheWrite5m = recache
			}
			if modelID != "" && modelIsString {
				if price, ok := pricing.CostFor(modelID, u, nil); ok {
					parts = append(parts, pk.value(format.FmtUsd(price)+sub, "crit"))
				}
			}
		} else {
			d := ""
			if hasIdle {
				d = format.FmtDuration(idleMs)
			}
			parts = append(parts, pk.value("❄"+d, "crit"))
		}
		cacheSeg = wrap("cache", strings.Join(parts, pk.raw(" ")))
	} else if hit, ok := numField(pcache, "hit_ratio"); ok {
		left := ""
		if hasExpires {
			if l := float64(math.Max(0, expires-nowSec) * 1000); l >= 60_000 {
				left = pk.dot() + pk.value(format.FmtDuration(l), "")
			}
		}
		cacheSeg = wrap("cache", pk.label("cache")+pk.raw(" ")+pk.value(format.FmtPct(float64(hit*100), 0), "")+left)
	}

	// subscription windows
	pl := paint("limits")
	var windows []string
	for _, w := range []struct{ key, label string }{{"five_hour", "5h"}, {"seven_day", "7d"}, {"spend_limit", "spend"}} {
		if v, ok := numField(input, "rate_limits", w.key, "used_percentage"); ok {
			windows = append(windows, pl.label(w.label)+pl.raw(" ")+pl.value(jscompat.String(jscompat.Round(v))+"%", ""))
		}
	}
	limitsSeg := ""
	if len(windows) > 0 {
		limitsSeg = wrap("limits", strings.Join(windows, pl.dot()))
	}

	// layout
	sepText := sl.Separator
	if !sl.HasSeparator {
		sepText = "⟡"
		if theme["pad"] != "" {
			sepText = ""
		}
	}
	sep := theme["gap"]
	if sepText != "" {
		sep = theme["gap"] + format.Colorize(sepText, theme["separator"], color) + theme["gap"]
	}
	line := func(segs ...string) string {
		var out []string
		for _, s := range segs {
			if s != "" {
				out = append(out, s)
			}
		}
		return strings.Join(out, sep)
	}
	if twoLine {
		var lines []string
		for _, l := range []string{line(brand, where, modelSeg, cacheSeg, limitsSeg), line(ctxSeg, turnSeg, sessionSeg)} {
			if l != "" {
				lines = append(lines, l)
			}
		}
		return strings.Join(lines, "\n")
	}
	return line(brand, where, modelSeg, ctxSeg, turnSeg, sessionSeg, cacheSeg, limitsSeg)
}
