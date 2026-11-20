package app

import (
	"context"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/filter"
)

func TestSessionStartAutoModeRtk(t *testing.T) {
	start := func(s filter.Settings, info filter.EngineInfo, mode string) string {
		h := Hooks{Config: fakeConfig{s}, Engine: fakeEngine{info: info}}
		return h.Handle(context.Background(), HookEvent{Name: "SessionStart", Source: "startup", PermissionMode: mode}).Context
	}
	rtk := filter.EngineInfo{Engine: "rtk", RtkPath: "rtk"}
	if c := start(on, rtk, "auto"); !strings.Contains(c, "rtk git status") {
		t.Errorf("auto: %q", c)
	}
	pinned := filter.EngineInfo{Engine: "rtk", RtkPath: "C:/tools/rtk.exe"}
	if c := start(on, pinned, "auto"); !strings.Contains(c, "PowerShell: `& \"C:/tools/rtk.exe\" git status`") {
		t.Errorf("pinned: %q", c)
	}
	for _, mode := range []string{"default", ""} {
		if c := start(on, rtk, mode); strings.Contains(c, "rtk") {
			t.Errorf("mode %q: %q", mode, c)
		}
	}
	off := on
	off.Enabled = false
	if c := start(off, rtk, "auto"); strings.Contains(c, "rtk") {
		t.Errorf("filter off: %q", c)
	}
	hooked := filter.EngineInfo{Engine: "rtk", RtkPath: "rtk", RtkHookDetected: true}
	if c := start(on, hooked, "auto"); strings.Contains(c, "rtk") {
		t.Errorf("rtk hook: %q", c)
	}
}
