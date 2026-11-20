package app

import (
	"context"
	"reflect"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/filter"
	"github.com/mananos/nxy.dev/v2/internal/report"
)

func postHooks(s filter.Settings) (Hooks, *fakeLegacyStore) {
	st := newFakeLegacyStore()
	h := Hooks{
		Config:      fakeConfig{s},
		Engine:      fakeEngine{info: filter.EngineInfo{Engine: "rtk", RtkPath: "rtk"}},
		LegacyData:  &fakeLegacyData{},
		LegacyStore: st,
	}
	return h, st
}

var metricsOn = filter.Settings{Enabled: true, Metrics: true, Engine: "rtk"}

func post(h Hooks, session, cmd, response string) HookResponse {
	return h.Handle(context.Background(), HookEvent{
		Name: "PostToolUse", Tool: "Bash", SessionID: session, ProjectDir: "/p",
		Command: cmd, HasCommand: true, ToolResponse: []byte(response),
	})
}

// wantGroup is the group the 1.x row of the same call folds to, so stats read the same thing.
func wantGroup(row map[string]any) report.FilterGroup {
	return report.GroupFilterRows([]any{row})[0]
}

func oneAdded(t *testing.T, st *fakeLegacyStore) report.FilterGroup {
	t.Helper()
	if len(st.added) != 1 {
		t.Fatalf("added = %+v", st.added)
	}
	if st.added[0].checkout != "/p/.nxy/local" || st.added[0].session != "s1" {
		t.Errorf("where = %+v", st.added[0])
	}
	return st.added[0].g
}

func TestPostToolBashRtk(t *testing.T) {
	h, st := postHooks(metricsOn)
	if r := post(h, "s1", "rtk git status", `{"stdout":"abcdefghi","stderr":"xy","interrupted":false}`); r != (HookResponse{}) {
		t.Errorf("answered %+v", r)
	}
	want := wantGroup(map[string]any{"engine": "rtk", "reason": "rtk", "command_kind": "git-status", "filtered_chars": 12.0, "est_tokens": 3.0})
	if got := oneAdded(t, st); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v want %+v", got, want)
	}
}

func TestPostToolBashSkipReason(t *testing.T) {
	h, st := postHooks(metricsOn)
	post(h, "s1", "git status", `{"stdout":"hello","stderr":""}`)
	want := wantGroup(map[string]any{"engine": "skip", "reason": "rtk-none", "command_kind": "git-status", "filtered_chars": 5.0, "est_tokens": 2.0})
	if got := oneAdded(t, st); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v want %+v", got, want)
	}
}

func TestPostToolBashFilterOff(t *testing.T) {
	s := metricsOn
	s.Enabled = false
	h, st := postHooks(s)
	post(h, "s1", "ls -la", `{"stdout":"a","stderr":""}`)
	want := wantGroup(map[string]any{"engine": "off", "reason": "filter-disabled", "command_kind": "ls", "filtered_chars": 1.0, "est_tokens": 1.0})
	if got := oneAdded(t, st); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v want %+v", got, want)
	}
}

func TestPostToolBashMetricsOff(t *testing.T) {
	s := metricsOn
	s.Metrics = false
	h, st := postHooks(s)
	post(h, "s1", "ls", `{"stdout":"a"}`)
	if len(st.added) != 0 {
		t.Errorf("wrote with metrics off: %+v", st.added)
	}
}

func TestPostToolBashNoSession(t *testing.T) {
	h, st := postHooks(metricsOn)
	post(h, "", "ls", `{"stdout":"a"}`)
	if len(st.added) != 0 {
		t.Errorf("wrote without a session: %+v", st.added)
	}
}

func TestPostToolBashOldStringResponse(t *testing.T) {
	h, st := postHooks(metricsOn)
	post(h, "s1", "ls", `"abcde"`)
	if got := oneAdded(t, st); got.Chars != 5 || got.Tokens != 2 {
		t.Errorf("got %+v", got)
	}
}

func TestPostToolBashUTF16(t *testing.T) {
	h, st := postHooks(metricsOn)
	// "é" is 1 code unit, the emoji 2: 3 units, 1 token.
	post(h, "s1", "ls", `{"stdout":"é😀"}`)
	if got := oneAdded(t, st); got.Chars != 3 || got.Tokens != 1 {
		t.Errorf("got %+v", got)
	}
}

func TestPostToolBashNoResponse(t *testing.T) {
	h, st := postHooks(metricsOn)
	post(h, "s1", "ls", ``)
	if got := oneAdded(t, st); got.Chars != 0 || got.Tokens != 0 || got.Rows != 1 {
		t.Errorf("got %+v", got)
	}
}

func TestPostToolBashSameCallsSumInOneGroup(t *testing.T) {
	h, st := postHooks(metricsOn)
	post(h, "s1", "rtk go test ./...", `{"stdout":"abcd"}`)
	post(h, "s1", "rtk go test ./...", `{"stdout":"abcdefgh"}`)
	var folded []report.FilterGroup
	for _, a := range st.added {
		folded = report.MergeFilterGroups(folded, []report.FilterGroup{a.g})
	}
	want := report.GroupFilterRows([]any{
		map[string]any{"engine": "rtk", "command_kind": "go-test", "filtered_chars": 4.0, "est_tokens": 1.0},
		map[string]any{"engine": "rtk", "command_kind": "go-test", "filtered_chars": 8.0, "est_tokens": 2.0},
	})
	if len(want) != 1 || !reflect.DeepEqual(folded, want) {
		t.Errorf("folded %+v want %+v", folded, want)
	}
}

func TestPostToolBashNoStoreIsQuiet(t *testing.T) {
	h, _ := postHooks(metricsOn)
	h.LegacyStore = nil
	if r := post(h, "s1", "ls", `{"stdout":"a"}`); r != (HookResponse{}) {
		t.Errorf("answered %+v", r)
	}
}
