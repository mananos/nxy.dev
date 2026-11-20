package app

import (
	"context"
	"strings"
	"testing"
)

func TestScoutDispatchGetsTheLocateCommand(t *testing.T) {
	h, _ := flowHooks(false)
	h.Exe = "/opt/nxy/nxy"
	h.Roles = fakeRoles{"scout": {"haiku", "low"}}
	ev := agentEv("PreToolUse", roleScout)
	ev.AgentPrompt = "Where is `cancelBooking` handled?\nNot in \"src/old\"; $HOME & friends | tee"
	r := h.Handle(context.Background(), ev)
	if r.Deny != "" || r.Model != "haiku" || r.Effort != "low" {
		t.Fatalf("response = %+v", r)
	}
	if !strings.HasPrefix(r.Prompt, ev.AgentPrompt) {
		t.Errorf("the dispatch prompt must stay first: %q", r.Prompt)
	}
	const want = `"/opt/nxy/nxy" locate "Where is cancelBooking handled? Not in src/old HOME friends tee"`
	if !strings.Contains(r.Prompt, want) {
		t.Errorf("prompt lacks %s:\n%s", want, r.Prompt)
	}
	// what nxy puts in the prompt is what nxy pre-approves
	for _, line := range strings.Split(r.Prompt, "\n") {
		if strings.HasPrefix(line, `"/opt/nxy/nxy" locate`) && !h.parseLocate(line, false) {
			t.Errorf("not pre-approved: %s", line)
		}
	}
}

func TestScoutQuestionNeutralisesSmartQuotes(t *testing.T) {
	got := scoutQuestion("a‘b’c‚d‛e“f”g„h")
	if got != "a b c d e f g h" {
		t.Errorf("scoutQuestion = %q", got)
	}
}

func TestScoutPromptWithoutExeOrFromASubagentIsUntouched(t *testing.T) {
	h, _ := flowHooks(false)
	ev := agentEv("PreToolUse", roleScout)
	ev.AgentPrompt = "where"
	if r := h.Handle(context.Background(), ev); r.Prompt != "" {
		t.Errorf("no exe: %+v", r)
	}
	h.Exe = "/opt/nxy/nxy"
	ev.AgentID = "sub"
	if r := h.Handle(context.Background(), ev); r.Prompt != "" {
		t.Errorf("from a subagent: %+v", r)
	}
	ev = agentEv("PreToolUse", roleReviewer)
	ev.AgentPrompt = "where"
	if r := h.Handle(context.Background(), ev); strings.Contains(r.Prompt, "locate") {
		t.Errorf("other roles get no locate: %+v", r)
	}
}

func TestScoutLocateIsPreApprovedWithoutAFeature(t *testing.T) {
	h, _ := flowHooks(false)
	h.Exe = "/opt/nxy/nxy"
	h.Config = fakeConfig{}
	cases := []struct {
		name, tool, agent, cmd string
		want                   bool
	}{
		{"search", "Bash", roleScout, `"/opt/nxy/nxy" locate "where is x"`, true},
		{"unquoted exe", "Bash", roleScout, `/opt/nxy/nxy locate "where is x"`, true},
		{"callers", "Bash", roleScout, `"/opt/nxy/nxy" locate --callers cancelBooking`, true},
		{"impact", "Bash", roleScout, `"/opt/nxy/nxy" locate --impact cancelBooking`, true},
		{"status", "Bash", roleScout, `"/opt/nxy/nxy" locate --status`, true},
		{"powershell", "PowerShell", roleScout, `& "/opt/nxy/nxy" locate "where is x"`, true},
		{"cd to the project", "Bash", roleScout, `cd /p && "/opt/nxy/nxy" locate "x"`, true},
		{"cd elsewhere", "Bash", roleScout, `cd /other && "/opt/nxy/nxy" locate "x"`, false},
		{"index rebuild", "Bash", roleScout, `"/opt/nxy/nxy" locate --index`, false},
		{"cwd flag", "Bash", roleScout, `"/opt/nxy/nxy" locate --cwd /etc x`, false},
		{"extra word", "Bash", roleScout, `"/opt/nxy/nxy" locate a b`, false},
		{"callers needs a symbol", "Bash", roleScout, `"/opt/nxy/nxy" locate --callers`, false},
		{"chained", "Bash", roleScout, `"/opt/nxy/nxy" locate "x"; rm -rf /`, false},
		{"substitution", "Bash", roleScout, `"/opt/nxy/nxy" locate "$(whoami)"`, false},
		{"other binary", "Bash", roleScout, `"/usr/bin/other" locate "x"`, false},
		{"other command", "Bash", roleScout, `"/opt/nxy/nxy" stats`, false},
		{"main thread", "Bash", "", `"/opt/nxy/nxy" locate "x"`, false},
		{"another role", "Bash", roleReviewer, `"/opt/nxy/nxy" locate "x"`, false},
	}
	for _, c := range cases {
		agentID := ""
		if c.agent != "" {
			agentID = "sub-1"
		}
		r := h.Handle(context.Background(), HookEvent{Name: "PreToolUse", Tool: c.tool, Command: c.cmd, HasCommand: true,
			AgentID: agentID, AgentType: c.agent, ProjectDir: "/p", Cwd: "/p"})
		if got := r.AllowReason != ""; got != c.want {
			t.Errorf("%s: allow = %v (%q), want %v", c.name, got, r.AllowReason, c.want)
		}
	}
}
