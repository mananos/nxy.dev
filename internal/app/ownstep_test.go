package app

import (
	"context"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

func TestOwnStepAllow(t *testing.T) {
	dir := t.TempDir()
	exe := "/opt/nxy/nxy"
	bash := func(id string) string {
		return "\"" + exe + "\" plan save --feature " + id + " <<'JSON'\n{\"title\":\"x > y; rm\"}\nJSON"
	}
	ps := func(id string) string {
		return "$OutputEncoding = [System.Text.UTF8Encoding]::new($false)\n@'\n{\"title\":\"x\"}\n'@ | & \"" + exe + "\" plan save --feature " + id
	}
	cases := []struct {
		name, tool, agent, cmd string
		active                 bool
		want                   bool
	}{
		{"red quoted", "Bash", "", `"/opt/nxy/nxy" batch red --feature ID`, true, true},
		{"green unquoted", "Bash", "", `/opt/nxy/nxy batch green --feature ID`, true, true},
		{"green implementer", "Bash", roleImplementer, `"/opt/nxy/nxy" batch green --feature ID`, true, true},
		{"ps call operator", "PowerShell", roleImplementer, `& "/opt/nxy/nxy" batch red --feature ID`, true, true},
		{"call operator in bash", "Bash", "", `& "/opt/nxy/nxy" batch red --feature ID`, true, false},
		{"other feature", "Bash", "", `"/opt/nxy/nxy" batch red --feature other`, true, false},
		{"no active feature", "Bash", "", `"/opt/nxy/nxy" batch red --feature ID`, false, false},
		{"other exe", "Bash", "", `"/tmp/evil" batch red --feature ID`, true, false},
		{"extra arg", "Bash", "", `"/opt/nxy/nxy" batch red --feature ID --x`, true, false},
		{"unknown kind", "Bash", "", `"/opt/nxy/nxy" batch stop --feature ID`, true, false},
		{"and", "Bash", "", `"/opt/nxy/nxy" batch red --feature ID && rm -rf x`, true, false},
		{"semicolon", "Bash", "", `"/opt/nxy/nxy" batch red --feature ID; ls`, true, false},
		{"pipe", "Bash", "", `"/opt/nxy/nxy" batch red --feature ID | cat`, true, false},
		{"redirect", "Bash", "", `"/opt/nxy/nxy" batch red --feature ID > f`, true, false},
		{"background", "Bash", "", `"/opt/nxy/nxy" batch red --feature ID &`, true, false},
		{"substitution", "Bash", "", `"/opt/nxy/nxy" batch red --feature $(id)`, true, false},
		{"planner batch", "Bash", rolePlanner, `"/opt/nxy/nxy" batch red --feature ID`, true, false},
		{"plan save heredoc", "Bash", rolePlanner, bash("ID"), true, true},
		{"plan save other id", "Bash", rolePlanner, bash("other"), true, false},
		{"plan save from main thread", "Bash", "", bash("ID"), true, false},
		{"plan save from implementer", "Bash", roleImplementer, bash("ID"), true, false},
		{"plan save command after heredoc", "Bash", rolePlanner, bash("ID") + "\nrm -rf x", true, false},
		{"plan save command in heredoc head", "Bash", rolePlanner, "\"/opt/nxy/nxy\" plan save --feature ID && rm x <<'JSON'\n{}\nJSON", true, false},
		{"plan save early terminator", "Bash", rolePlanner, "\"/opt/nxy/nxy\" plan save --feature ID <<'JSON'\n{}\nJSON\nrm x\nJSON", true, false},
		{"plan save powershell", "PowerShell", rolePlanner, ps("ID"), true, true},
		{"plan save powershell other id", "PowerShell", rolePlanner, ps("other"), true, false},
		{"plan save powershell extra command", "PowerShell", rolePlanner, ps("ID") + "; rm x", true, false},
		{"plan save powershell no encoding", "PowerShell", rolePlanner, "@'\n{}\n'@ | & \"/opt/nxy/nxy\" plan save --feature ID", true, false},
		{"plain command", "Bash", "", `git status`, true, false},
	}
	for _, c := range cases {
		h, st := flowHooks(false)
		h.Exe = exe
		h.Config = fakeConfig{}
		id := "none"
		if c.active {
			id = seed(t, h, st, dir, runningState, runningNext)
		}
		cmd := replaceID(c.cmd, id)
		agentID := ""
		if c.agent != "" {
			agentID = "agent-1"
		}
		r := h.Handle(context.Background(), HookEvent{Name: "PreToolUse", Tool: c.tool, Command: cmd, HasCommand: true,
			AgentID: agentID, AgentType: c.agent, ProjectDir: dir, Cwd: dir})
		if got := r.AllowReason != ""; got != c.want {
			t.Errorf("%s: allow = %v (%q), want %v", c.name, got, r.AllowReason, c.want)
		}
		if r.AllowReason != "" && r.Rewrite != nil {
			t.Errorf("%s: allow combined with a rewrite", c.name)
		}
	}
}

// replaceID puts the seeded feature id where the case wrote ID (as a whole word).
func replaceID(cmd, id string) string {
	out := ""
	for i := 0; i < len(cmd); {
		if i+2 <= len(cmd) && cmd[i:i+2] == "ID" && (i+2 == len(cmd) || !isWordByte(cmd[i+2])) && (i == 0 || cmd[i-1] == ' ') {
			out += id
			i += 2
			continue
		}
		out += string(cmd[i])
		i++
	}
	return out
}

func isWordByte(b byte) bool { return isLetter(b) || (b >= '0' && b <= '9') || b == '_' }

type slashFiles struct{}

func (slashFiles) NativePath(p string) string {
	if rest, ok := strings.CutPrefix(p, "/c/"); ok {
		return "C:/" + rest
	}
	return p
}
func (slashFiles) IsDir(string) bool { return true }

func TestOwnStepLeadingCd(t *testing.T) {
	old := goos
	goos = "windows"
	defer func() { goos = old }()
	dir := "C:/Users/Me/proj"
	step := ` "/opt/nxy/nxy" batch red --feature `
	cases := []struct {
		name, prefix string
		want         bool
	}{
		{"quoted", `cd "C:/Users/Me/proj" &&`, true},
		{"unquoted", `cd C:/Users/Me/proj &&`, true},
		{"single quoted", `cd 'c:\users\me\proj' &&`, true},
		{"git bash form", `cd /c/Users/Me/proj &&`, true},
		{"trailing slash", `cd "C:/Users/Me/proj/" &&`, true},
		{"outside dir", `cd "C:/Users/Me/other" &&`, false},
		{"two cds", `cd C:/Users/Me/proj && cd C:/Users/Me/proj &&`, false},
		{"semicolon", `cd C:/Users/Me/proj ;`, false},
		{"cd alone", `cd C:/Users/Me/proj`, false},
		{"substitution", `cd $HOME &&`, false},
	}
	for _, c := range cases {
		h, st := flowHooks(false)
		h.Exe = "/opt/nxy/nxy"
		h.Config = fakeConfig{}
		h.Files = slashFiles{}
		id := seed(t, h, st, dir, runningState, runningNext)
		ev := HookEvent{Name: "PreToolUse", Tool: "Bash", Command: c.prefix + step + id, HasCommand: true, ProjectDir: dir, Cwd: dir}
		if got := h.Handle(context.Background(), ev).AllowReason != ""; got != c.want {
			t.Errorf("%s: allow = %v, want %v", c.name, got, c.want)
		}
	}
	h, st := flowHooks(false)
	h.Exe = "/opt/nxy/nxy"
	h.Config = fakeConfig{}
	id := seed(t, h, st, dir, runningState, runningNext)
	ev := HookEvent{Name: "PreToolUse", Tool: "Bash", Command: `cd "C:/Users/Me/proj" && ` + step + id + ` && rm x`, HasCommand: true, ProjectDir: dir, Cwd: dir}
	if h.Handle(context.Background(), ev).AllowReason != "" {
		t.Error("chained after the step allowed")
	}
}

func TestOwnStepWindowsPath(t *testing.T) {
	old := goos
	goos = "windows"
	defer func() { goos = old }()
	dir := t.TempDir()
	h, st := flowHooks(false)
	h.Exe = "C:/Users/Me/bin/nxy.exe"
	h.Config = fakeConfig{}
	id := seed(t, h, st, dir, domain.State{Phase: domain.PhaseRunningBatch, Batch: 1, Batches: 1}, runningNext)
	for _, exe := range []string{`"C:\Users\me\BIN\nxy.exe"`, `C:/Users/Me/bin/nxy.exe`, `"c:/users/me/bin/nxy"`} {
		ev := HookEvent{Name: "PreToolUse", Tool: "PowerShell", Command: "& " + exe + " batch green --feature " + id, HasCommand: true, ProjectDir: dir, Cwd: dir}
		if r := h.Handle(context.Background(), ev); r.AllowReason == "" {
			t.Errorf("%s: not allowed", exe)
		}
	}
	ev := HookEvent{Name: "PreToolUse", Tool: "PowerShell", Command: `& "C:\other\nxy.exe" batch green --feature ` + id, HasCommand: true, ProjectDir: dir, Cwd: dir}
	if r := h.Handle(context.Background(), ev); r.AllowReason != "" {
		t.Error("another exe allowed")
	}
}
