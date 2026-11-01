package report

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/mananos/nxy.dev/internal/jscompat"
)

func decode(t *testing.T, text string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestSummarizeFilter(t *testing.T) {
	rows := decode(t, `[
		{"engine":"rtk","command_kind":"git","filtered_chars":100,"est_tokens":25},
		{"engine":"skip","reason":"short","command_kind":"ls","filtered_chars":10,"est_tokens":3},
		{"engine":"skip","reason":"short","command_kind":"ls","filtered_chars":0},
		{"engine":"skip","reason":"tiny","command_kind":"git","filtered_chars":0},
		{"engine":"skip","reason":"tiny","command_kind":"cat","filtered_chars":500},
		{"engine":"skip","reason":"tiny","command_kind":null},
		{"command_kind":"cat","filtered_chars":null,"est_tokens":5},
		{}
	]`).([]any)
	s := SummarizeFilter(rows)
	got, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"rows":8,"byEngine":{"rtk":1,"skip":5,"undefined":2},"skipReasons":[["tiny",3],["short",2]],"filteredChars":610,"estTokens":33,` +
		`"byKind":[{"kind":"cat","rows":2,"rtk":0,"chars":500,"tokens":5},{"kind":"git","rows":2,"rtk":1,"chars":100,"tokens":25},` +
		`{"kind":"ls","rows":2,"rtk":0,"chars":10,"tokens":3},{"kind":null,"rows":1,"rtk":0,"chars":0,"tokens":0},{"rows":1,"rtk":0,"chars":0,"tokens":0}]}`
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestSummarizeFilterEmpty(t *testing.T) {
	got, _ := json.Marshal(SummarizeFilter(nil))
	want := `{"rows":0,"byEngine":{},"skipReasons":[],"filteredChars":0,"estTokens":0,"byKind":[]}`
	if string(got) != want {
		t.Errorf("got %s", got)
	}
}

func TestSettingsFrom(t *testing.T) {
	cfg := decode(t, `{
		"metrics": {
			"projectsDir": "/p", "subscription": 1, "cacheBreakThreshold": "5",
			"statusline": {"color": 0, "preset": "vivid", "layout": "two-line", "separator": null, "brand": "",
				"ctxWarnTokens": null, "ctxCritTokens": "7", "turnWarnUsd": true, "turnCritUsd": "abc",
				"theme": {"brand": "red", "n": 5, "o": {}, "x": "bold"}}
		},
		"gate": {"contextTokens": null}
	}`).(map[string]any)
	s := SettingsFrom(cfg)
	if s.ProjectsDir != "/p" || !s.Subscription || s.CacheBreakThreshold != 5 || s.GateContextTokens != 0 {
		t.Errorf("metrics/gate: %+v", s)
	}
	sl := s.Statusline
	if !sl.Color {
		t.Error("color is off only when exactly false")
	}
	if sl.HasSeparator || sl.Brand != "" || sl.Preset != "vivid" || sl.Layout != "two-line" {
		t.Errorf("statusline: %+v", sl)
	}
	if sl.CtxWarnTokens != 0 || sl.CtxCritTokens != 7 || sl.TurnWarnUsd != 1 || !math.IsNaN(sl.TurnCritUsd) || !math.IsNaN(sl.PromptCacheTtlMin) {
		t.Errorf("thresholds: %+v", sl)
	}
	if want := map[string]string{"brand": "red", "x": "bold"}; !reflect.DeepEqual(sl.Theme, want) {
		t.Errorf("theme: %v", sl.Theme)
	}
}

func TestSettingsFromDefaultsAndOff(t *testing.T) {
	s := SettingsFrom(nil)
	if s.CacheBreakThreshold != 100000 || s.GateContextTokens != 0 || s.Subscription || !s.Statusline.Color {
		t.Errorf("empty config: %+v", s)
	}
	cfg := decode(t, `{"metrics":{"statusline":{"color":false,"separator":""}},"gate":{"contextTokens":"250000"}}`).(map[string]any)
	s = SettingsFrom(cfg)
	if s.Statusline.Color || !s.Statusline.HasSeparator || s.Statusline.Separator != "" || s.GateContextTokens != 250000 {
		t.Errorf("explicit config: %+v", s)
	}
	if !jscompat.IsUndefined(jscompat.Undefined) {
		t.Fatal("sanity")
	}
}
