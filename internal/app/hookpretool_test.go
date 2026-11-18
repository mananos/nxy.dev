package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/filter"
)

type fakeConfig struct{ s filter.Settings }

func (f fakeConfig) Filter(string) filter.Settings { return f.s }

type fakeEngine struct {
	info filter.EngineInfo
	res  filter.RewriteResult
}

func (f fakeEngine) Info(filter.Settings, string) filter.EngineInfo { return f.info }
func (f fakeEngine) Rewrite(filter.EngineInfo, string) (filter.RewriteResult, error) {
	return f.res, nil
}

type fakeRules struct{ r filter.Rules }

func (f fakeRules) Rules(string) filter.Rules { return f.r }

func run(s filter.Settings, res filter.RewriteResult, rules filter.Rules, tool, cmd string, has bool) HookResponse {
	return runMode(s, res, rules, tool, cmd, has, "", "rtk")
}

func runMode(s filter.Settings, res filter.RewriteResult, rules filter.Rules, tool, cmd string, has bool, mode, rtkPath string) HookResponse {
	h := Hooks{
		Config:      fakeConfig{s},
		Engine:      fakeEngine{filter.EngineInfo{Engine: "rtk", RtkPath: rtkPath}, res},
		Permissions: fakeRules{rules},
	}
	return h.Handle(context.Background(), HookEvent{Name: "PreToolUse", Tool: tool, Command: cmd, HasCommand: has, PermissionMode: mode})
}

var on = filter.Settings{Enabled: true, Engine: "rtk", AutoAllow: true}

func TestPreToolBashAutoMode(t *testing.T) {
	ok := filter.RewriteResult{Code: 0, Stdout: "rtk git status"}
	ask := filter.RewriteResult{Code: 3, Stdout: "rtk git status"}
	allowGit := filter.Rules{Allow: []string{"Bash(git status:*)"}}
	noAuto := on
	noAuto.AutoAllow = false
	auto := func(s filter.Settings, res filter.RewriteResult, rules filter.Rules, tool, cmd string) HookResponse {
		return runMode(s, res, rules, tool, cmd, true, "auto", "rtk")
	}

	if r := auto(on, ask, filter.Rules{}, "Bash", "git status"); r.Rewrite != nil || !strings.Contains(r.Deny, "rtk git status") {
		t.Errorf("ask, no rule: %+v", r)
	}
	if r := auto(on, ask, allowGit, "Bash", "git status"); r.Rewrite == nil || !r.Rewrite.Allow || r.Deny != "" {
		t.Errorf("allow rule: %+v", r)
	}
	if r := auto(noAuto, ok, allowGit, "Bash", "git status"); r.Rewrite != nil || r.Deny == "" {
		t.Errorf("AutoAllow false: %+v", r)
	}
	if r := auto(on, ok, filter.Rules{}, "Bash", "rtk git status"); r.Rewrite != nil || r.Deny != "" {
		t.Errorf("already wrapped: %+v", r)
	}
	if r := auto(on, ok, filter.Rules{Deny: []string{"Bash(git status:*)"}}, "Bash", "git status"); r.Rewrite != nil || r.Deny != "" {
		t.Errorf("deny verdict: %+v", r)
	}
	if r := auto(on, ask, filter.Rules{}, "PowerShell", "git status"); r.Rewrite != nil || r.Deny == "" {
		t.Errorf("powershell: %+v", r)
	}
	if r := runMode(on, ask, filter.Rules{}, "Bash", "git status", true, "auto", "C:/tools/rtk.exe"); r.Rewrite != nil || !strings.Contains(r.Deny, `"C:/tools/rtk.exe" git status`) {
		t.Errorf("pinned path: %+v", r)
	}
	if r := runMode(on, ask, filter.Rules{}, "PowerShell", "git status", true, "auto", "C:/tools/rtk.exe"); r.Rewrite != nil || !strings.Contains(r.Deny, `& "C:/tools/rtk.exe" git status`) {
		t.Errorf("powershell pinned path: %+v", r)
	}
	for _, mode := range []string{"default", ""} {
		if r := runMode(on, ask, filter.Rules{}, "Bash", "git status", true, mode, "rtk"); r.Deny != "" || r.Rewrite == nil || r.Rewrite.Allow || r.Rewrite.Command != "rtk git status" {
			t.Errorf("mode %q: %+v", mode, r)
		}
	}
}

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

func TestPreToolBashDeniesActionDecide(t *testing.T) {
	cmds := map[string]string{
		"Bash":       "nxy action decide --ask 01J --answer Aprobar",
		"PowerShell": "& 'C:\\Users\\x\\.nxy\\bin\\nxy.cmd' action decide --ask 01J --release",
	}
	more := []string{
		`"/home/u/.nxy/bin/nxy" action decide --ask X --answer A`,
		"cd /p && nxy.exe action decide --ask X --answer A",
	}
	for tool, cmd := range cmds {
		for _, agent := range []string{"", "a1"} {
			h := Hooks{Config: fakeConfig{on}, Engine: fakeEngine{}, Permissions: fakeRules{}}
			r := h.Handle(context.Background(), HookEvent{Name: "PreToolUse", Tool: tool, Command: cmd, HasCommand: true, AgentID: agent})
			if !strings.Contains(r.Deny, "action decide") {
				t.Errorf("%s agent=%q: %+v", tool, agent, r)
			}
		}
	}
	for _, cmd := range more {
		h := Hooks{Config: fakeConfig{on}, Engine: fakeEngine{}, Permissions: fakeRules{}}
		if r := h.Handle(context.Background(), HookEvent{Name: "PreToolUse", Tool: "Bash", Command: cmd, HasCommand: true}); r.Deny == "" {
			t.Errorf("%q not denied", cmd)
		}
	}
	h := Hooks{Config: fakeConfig{on}, Engine: fakeEngine{}, Permissions: fakeRules{}}
	if r := h.Handle(context.Background(), HookEvent{Name: "PreToolUse", Tool: "Bash", Command: "nxy feature list", HasCommand: true}); r.Deny != "" {
		t.Errorf("other nxy command denied: %+v", r)
	}
}

// noStore panics on any call: a hook that must not open the DB.
type noStore struct{ Store }

func TestPreToolBashShellWrites(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	bash := func(cmd, agent, agentType string) HookEvent {
		return HookEvent{Name: "PreToolUse", Tool: "Bash", Command: cmd, HasCommand: true, ProjectDir: root, Cwd: root, AgentID: agent, AgentType: agentType}
	}
	withFeature := func() (Hooks, *memStore, string) {
		h, st := flowHooks(false)
		h.Config = fakeConfig{}
		id := seed(t, h, st, root, runningState, runningNext)
		return h, st, id
	}
	deny := []string{"echo x > f", "sed -i s/a/b/ src/a.go", "cd sub && echo x >> f", "tee out.txt", "cp a b"}
	for _, c := range deny {
		h, _, _ := withFeature()
		r := h.Handle(context.Background(), bash(c, "", ""))
		if !strings.Contains(r.Deny, "does not write code") {
			t.Errorf("%q: deny = %q", c, r.Deny)
		}
	}
	allow := []string{"echo x > " + filepath.ToSlash(filepath.Join(outside, "f")), "echo x > .git/note", "echo x > .nxy/note", "ls", "echo x > /dev/null", "git status 2>&1"}
	for _, c := range allow {
		h, _, _ := withFeature()
		if r := h.Handle(context.Background(), bash(c, "", "")); r.Deny != "" {
			t.Errorf("%q: deny = %q", c, r.Deny)
		}
	}

	t.Run("no feature", func(t *testing.T) {
		h, _ := flowHooks(false)
		h.Config = fakeConfig{}
		if r := h.Handle(context.Background(), bash("echo x > f", "", "")); r.Deny != "" {
			t.Errorf("deny = %q", r.Deny)
		}
	})
	t.Run("implementer write is counted", func(t *testing.T) {
		h, st, id := withFeature()
		if r := h.Handle(context.Background(), bash("echo x > f", "a1", roleImplementer)); r.Deny != "" {
			t.Fatalf("deny = %q", r.Deny)
		}
		if g := st.feats[id]; g.ProofOf(g.State.Batch).Edits != 1 {
			t.Errorf("proof = %+v", g.Proofs)
		}
		h.Handle(context.Background(), bash("ls", "a1", roleImplementer))
		if g := st.feats[id]; g.ProofOf(g.State.Batch).Edits != 1 {
			p := g.Proofs
			t.Errorf("plain command counted: %+v", p)
		}
	})
	t.Run("other subagent is not counted", func(t *testing.T) {
		h, st, id := withFeature()
		h.Handle(context.Background(), bash("echo x > f", "a1", "nxy:planner"))
		if g := st.feats[id]; g.Proof != nil || len(g.Proofs) > 0 {
			t.Errorf("proof = %+v %+v", g.Proof, g.Proofs)
		}
	})
	t.Run("plain commands do not touch the store", func(t *testing.T) {
		h := Hooks{Store: noStore{}, Repos: fakeRepos{}, Config: fakeConfig{}}
		for _, c := range []string{"ls -la", "git status 2>&1", "echo x > /dev/null", "echo x > " + filepath.ToSlash(filepath.Join(outside, "f"))} {
			h.Handle(context.Background(), bash(c, "", ""))
		}
	})
}

func TestOtherEventsSayNothing(t *testing.T) {
	h := Hooks{Config: fakeConfig{on}, Engine: fakeEngine{}, Permissions: fakeRules{}}
	r := h.Handle(context.Background(), HookEvent{Name: "PostToolUse", Tool: "Bash", Command: "git status", HasCommand: true})
	if r.Rewrite != nil {
		t.Error("PostToolUse: want nothing")
	}
}
