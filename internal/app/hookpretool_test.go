package app

import (
	"context"
	"testing"

	"github.com/matyy-a/nxy.dev/internal/filter"
)

type fakeConfig struct{ s filter.Settings }

func (f fakeConfig) Filter(string) filter.Settings { return f.s }

type fakeEngine struct {
	info filter.EngineInfo
	res  filter.RewriteResult
}

func (f fakeEngine) Info(filter.Settings, string) filter.EngineInfo { return f.info }
func (f fakeEngine) Rewrite(string, string) (filter.RewriteResult, error) {
	return f.res, nil
}

type fakeRules struct{ r filter.Rules }

func (f fakeRules) Rules(string) filter.Rules { return f.r }

func run(s filter.Settings, res filter.RewriteResult, rules filter.Rules, tool, cmd string, has bool) HookResponse {
	h := Hooks{
		Config:      fakeConfig{s},
		Engine:      fakeEngine{filter.EngineInfo{Engine: "rtk", RtkPath: "rtk"}, res},
		Permissions: fakeRules{rules},
	}
	return h.Handle(context.Background(), HookEvent{Name: "PreToolUse", Tool: tool, Command: cmd, HasCommand: has})
}

var on = filter.Settings{Enabled: true, Engine: "rtk", AutoAllow: true}

func TestPreToolBash(t *testing.T) {
	ok := filter.RewriteResult{Code: 0, Stdout: "rtk git status"}
	ask := filter.RewriteResult{Code: 3, Stdout: "rtk git status"}
	off := on
	off.Enabled = false
	noAuto := on
	noAuto.AutoAllow = false
	allowGit := filter.Rules{Allow: []string{"Bash(git status:*)"}}

	if r := run(off, ok, filter.Rules{}, "Bash", "git status", true); r.Rewrite != nil {
		t.Error("filter off: want nothing")
	}
	if r := run(on, ok, filter.Rules{}, "Edit", "git status", true); r.Rewrite != nil {
		t.Error("non-Bash tool: want nothing")
	}
	if r := run(on, ok, filter.Rules{}, "Bash", "", false); r.Rewrite != nil {
		t.Error("no command: want nothing")
	}
	if r := run(on, ok, filter.Rules{Deny: []string{"Bash(git status:*)"}}, "Bash", "git status", true); r.Rewrite != nil {
		t.Error("deny verdict: want nothing")
	}
	if r := run(on, ask, allowGit, "Bash", "git status", true); r.Rewrite == nil || !r.Rewrite.Allow {
		t.Errorf("rtk ask + allow rule: %+v", r.Rewrite)
	}
	if r := run(on, ask, filter.Rules{}, "Bash", "git status", true); r.Rewrite == nil || r.Rewrite.Allow {
		t.Errorf("rtk ask, no rule: %+v", r.Rewrite)
	}
	if r := run(on, ok, filter.Rules{}, "Bash", "git status", true); r.Rewrite == nil || !r.Rewrite.Allow || r.Rewrite.Command != "rtk git status" {
		t.Errorf("rtk allow: %+v", r.Rewrite)
	}
	chain := filter.RewriteResult{Code: 0, Stdout: "rtk git status && rtk git diff"}
	if r := run(on, chain, allowGit, "Bash", "git status && git diff", true); r.Rewrite == nil || r.Rewrite.Allow {
		t.Errorf("chain partly allowed: %+v", r.Rewrite)
	}
	if r := run(noAuto, ok, allowGit, "Bash", "git status", true); r.Rewrite == nil || r.Rewrite.Allow {
		t.Errorf("AutoAllow false: %+v", r.Rewrite)
	}
}

func TestOtherEventsSayNothing(t *testing.T) {
	h := Hooks{Config: fakeConfig{on}, Engine: fakeEngine{}, Permissions: fakeRules{}}
	r := h.Handle(context.Background(), HookEvent{Name: "PostToolUse", Tool: "Bash", Command: "git status", HasCommand: true})
	if r.Rewrite != nil {
		t.Error("PostToolUse: want nothing")
	}
}
