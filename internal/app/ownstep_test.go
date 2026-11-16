package app

import (
	"context"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/filter"
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
	rsave := func(id, lens string) string {
		return "\"" + exe + "\" review save --feature " + id + " --lens " + lens + " <<'JSON'\n{\"lens\":\"x\"}\nJSON"
	}
	rsavePS := func(id, lens string) string {
		return "$OutputEncoding = [System.Text.UTF8Encoding]::new($false)\n@'\n{}\n'@ | & \"" + exe + "\" review save --feature " + id + " --lens " + lens
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
		{"affected main", "Bash", "", `"/opt/nxy/nxy" affected --feature ID`, true, true},
		{"affected implementer", "Bash", roleImplementer, `"/opt/nxy/nxy" affected --feature ID`, true, true},
		{"affected ps", "PowerShell", "", `& "/opt/nxy/nxy" affected --feature ID`, true, true},
		{"affected planner", "Bash", rolePlanner, `"/opt/nxy/nxy" affected --feature ID`, true, false},
		{"affected extra arg", "Bash", "", `"/opt/nxy/nxy" affected --feature ID --x`, true, false},
		{"affected chained", "Bash", "", `"/opt/nxy/nxy" affected --feature ID && ls`, true, false},
		{"affected other id", "Bash", "", `"/opt/nxy/nxy" affected --feature other`, true, false},
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
		{"review save heredoc", "Bash", roleReviewer, rsave("ID", "api"), true, true},
		{"review save quick", "Bash", roleReviewerQuick, rsave("ID", "base"), true, true},
		{"review save powershell", "PowerShell", roleReviewer, rsavePS("ID", "api"), true, true},
		{"review save other id", "Bash", roleReviewer, rsave("other", "api"), true, false},
		{"review save planner", "Bash", rolePlanner, rsave("ID", "api"), true, false},
		{"review save main thread", "Bash", "", rsave("ID", "api"), true, false},
		{"review save refuter", "Bash", roleRefuter, rsave("ID", "api"), true, false},
		{"review save no lens", "Bash", roleReviewer, "\"/opt/nxy/nxy\" review save --feature ID <<'JSON'\n{}\nJSON", true, false},
		{"review save chained", "Bash", roleReviewer, rsave("ID", "api") + "\nrm x", true, false},
		{"review save chained head", "Bash", roleReviewer, "\"/opt/nxy/nxy\" review save --feature ID --lens api && rm x <<'JSON'\n{}\nJSON", true, false},
		{"review refute heredoc", "Bash", roleRefuter, "\"/opt/nxy/nxy\" review refute --feature ID <<'JSON'\n{}\nJSON", true, true},
		{"review refute powershell", "PowerShell", roleRefuter, "$OutputEncoding = [System.Text.UTF8Encoding]::new($false)\n@'\n{}\n'@ | & \"/opt/nxy/nxy\" review refute --feature ID", true, true},
		{"review refute reviewer", "Bash", roleReviewer, "\"/opt/nxy/nxy\" review refute --feature ID <<'JSON'\n{}\nJSON", true, false},
		{"review refute other id", "Bash", roleRefuter, "\"/opt/nxy/nxy\" review refute --feature other <<'JSON'\n{}\nJSON", true, false},
		{"review docs documenter", "Bash", roleDocumenter, `"/opt/nxy/nxy" review docs --feature ID`, true, true},
		{"review docs documenter ps", "PowerShell", roleDocumenter, `& "/opt/nxy/nxy" review docs --feature ID`, true, true},
		{"review docs main thread", "Bash", "", `"/opt/nxy/nxy" review docs --feature ID`, true, false},
		{"review docs reviewer", "Bash", roleReviewer, `"/opt/nxy/nxy" review docs --feature ID`, true, false},
		{"review docs other id", "Bash", roleDocumenter, `"/opt/nxy/nxy" review docs --feature other`, true, false},
		{"review docs chained", "Bash", roleDocumenter, `"/opt/nxy/nxy" review docs --feature ID && rm x`, true, false},
		{"review docs extra arg", "Bash", roleDocumenter, `"/opt/nxy/nxy" review docs --feature ID --x`, true, false},
		{"plain command", "Bash", "", `git status`, true, false},
		{"mem related main", "Bash", "", `"/opt/nxy/nxy" mem related a.go b/c.go`, true, true},
		{"mem related planner", "Bash", rolePlanner, `"/opt/nxy/nxy" mem related a.go`, true, true},
		{"mem related ps", "PowerShell", rolePlanner, `& "/opt/nxy/nxy" mem related a.go`, true, true},
		{"mem related no path", "Bash", "", `"/opt/nxy/nxy" mem related`, true, false},
		{"mem related flag", "Bash", "", `"/opt/nxy/nxy" mem related --x`, true, false},
		{"mem related chained", "Bash", "", `"/opt/nxy/nxy" mem related a.go && rm x`, true, false},
		{"mem related implementer", "Bash", roleImplementer, `"/opt/nxy/nxy" mem related a.go`, true, false},
		{"mem rule main", "Bash", "", `"/opt/nxy/nxy" mem rule x`, true, false},
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

// In auto mode with rtk active, nxy's own steps stay pre-approved and are not
// denied by the rtk rule. The engine would ask for a rewrite of every command,
// so skipping the own-step check would end in a deny here.
func TestOwnStepAllowAutoModeRtk(t *testing.T) {
	dir := t.TempDir()
	exe := "/opt/nxy/nxy"
	cases := []struct {
		name, mode, cmd string
		want            bool
	}{
		{"auto own step", "auto", `/opt/nxy/nxy batch green --feature ID`, true},
		{"auto own step quoted", "auto", `"/opt/nxy/nxy" batch red --feature ID`, true},
		{"default own step", "default", `/opt/nxy/nxy batch green --feature ID`, true},
		{"auto other exe", "auto", `/tmp/evil batch green --feature ID`, false},
	}
	for _, c := range cases {
		h, st := flowHooks(false)
		h.Exe = exe
		h.Config = fakeConfig{on}
		h.Engine = fakeEngine{filter.EngineInfo{Engine: "rtk", RtkPath: "rtk"}, filter.RewriteResult{Code: 3, Stdout: "rtk x"}}
		h.Permissions = fakeRules{}
		id := seed(t, h, st, dir, runningState, runningNext)
		r := h.Handle(context.Background(), HookEvent{Name: "PreToolUse", Tool: "Bash", Command: replaceID(c.cmd, id), HasCommand: true,
			ProjectDir: dir, Cwd: dir, PermissionMode: c.mode})
		if got := r.AllowReason != ""; got != c.want {
			t.Errorf("%s: allow = %v (%q), want %v", c.name, got, r.AllowReason, c.want)
		}
		if c.want && (r.Deny != "" || r.Rewrite != nil) {
			t.Errorf("%s: denied or rewritten: %+v", c.name, r)
		}
		if !c.want && c.mode == "auto" && r.Deny == "" {
			t.Errorf("%s: control case was not denied, test proves nothing: %+v", c.name, r)
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
func (slashFiles) IsDir(string) bool  { return true }
func (slashFiles) Exists(string) bool { return false }

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
