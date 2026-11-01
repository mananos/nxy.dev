package report

import (
	"github.com/mananos/nxy.dev/internal/jscompat"
)

// StatuslineSettings are cfg.metrics.statusline read with JS semantics.
type StatuslineSettings struct {
	Color             bool    // on unless the value is exactly false
	Preset            string  // a string, else ""
	Layout            string  // a string, else ""
	Separator         string  // meaningful only when HasSeparator
	HasSeparator      bool    // false when the value is null or missing (`??` falls back)
	Brand             string  // "" when falsy (no brand segment)
	CtxWarnTokens     float64 // ToNumber: null is 0, a missing key NaN
	CtxCritTokens     float64
	TurnWarnUsd       float64
	TurnCritUsd       float64
	PromptCacheTtlMin float64
	Theme             map[string]string // own string-valued keys of metrics.statusline.theme
}

// Settings is the part of the merged config the usage reports read.
type Settings struct {
	ProjectsDir         string  // metrics.projectsDir ("" = default)
	Subscription        bool    // metrics.subscription is truthy
	CacheBreakThreshold float64 // metrics.cacheBreakThreshold; 100000 when null or missing
	GateContextTokens   float64 // gate.contextTokens; 0 when null or missing
	Statusline          StatuslineSettings
}

// defaultBreakThreshold is parseSession's `?? 100000`.
const defaultBreakThreshold = 100000

// TruthyString is String(v) for a truthy v and "" otherwise (where JS uses `v ? v : null` or
// `v || ""` and then writes the value out).
func TruthyString(v any) string {
	if !jscompat.Truthy(v) {
		return ""
	}
	return jscompat.String(v)
}

func onlyString(v any) string {
	s, _ := v.(string)
	return s
}

func nullish(v any) bool { return v == nil || jscompat.IsUndefined(v) }

// SettingsFrom reads the merged config (as decoded JSON) the way the 1.x scripts do.
func SettingsFrom(cfg map[string]any) Settings {
	var root any = cfg
	m := jscompat.Get(root, "metrics")
	sl := jscompat.Get(m, "statusline")
	s := Settings{
		ProjectsDir:         onlyString(jscompat.Get(m, "projectsDir")),
		Subscription:        jscompat.Truthy(jscompat.Get(m, "subscription")),
		CacheBreakThreshold: defaultBreakThreshold,
		GateContextTokens:   0,
	}
	if v := jscompat.Get(m, "cacheBreakThreshold"); !nullish(v) {
		s.CacheBreakThreshold = jscompat.ToNumber(v)
	}
	if v := jscompat.Get(root, "gate", "contextTokens"); !nullish(v) {
		s.GateContextTokens = jscompat.ToNumber(v)
	}
	st := StatuslineSettings{
		Color:             jscompat.Get(sl, "color") != false,
		Preset:            onlyString(jscompat.Get(sl, "preset")),
		Layout:            onlyString(jscompat.Get(sl, "layout")),
		Brand:             TruthyString(jscompat.Get(sl, "brand")),
		CtxWarnTokens:     jscompat.ToNumber(jscompat.Get(sl, "ctxWarnTokens")),
		CtxCritTokens:     jscompat.ToNumber(jscompat.Get(sl, "ctxCritTokens")),
		TurnWarnUsd:       jscompat.ToNumber(jscompat.Get(sl, "turnWarnUsd")),
		TurnCritUsd:       jscompat.ToNumber(jscompat.Get(sl, "turnCritUsd")),
		PromptCacheTtlMin: jscompat.ToNumber(jscompat.Get(sl, "promptCacheTtlMin")),
		Theme:             map[string]string{},
	}
	if sep := jscompat.Get(sl, "separator"); !nullish(sep) {
		st.HasSeparator = true
		st.Separator = TruthyString(sep)
	}
	if theme, ok := jscompat.Get(sl, "theme").(map[string]any); ok {
		for k, v := range theme {
			if str, ok := v.(string); ok {
				st.Theme[k] = str
			}
		}
	}
	s.Statusline = st
	return s
}
