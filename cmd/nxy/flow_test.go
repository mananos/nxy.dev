package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// flowRepo makes a temp repo, enters it and returns its directory as the process sees it.
func flowRepo(t *testing.T) string {
	t.Helper()
	isolate(t)
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git", "config"), []byte("[core]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

func runCmd(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

func hookCall(t *testing.T, payload map[string]any) string {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	code, out, errb := runCmd(t, string(b), "hook")
	if code != 0 {
		t.Fatalf("hook exit %d: %s", code, errb)
	}
	return out
}

func featureID(t *testing.T) string {
	t.Helper()
	_, out, _ := runCmd(t, "", "status")
	for _, l := range strings.Split(out, "\n") {
		if id, ok := strings.CutPrefix(l, "feature: "); ok {
			return strings.TrimSpace(id)
		}
	}
	t.Fatalf("no feature in status: %q", out)
	return ""
}

const flowPlan = `{"title":"Flag","rules":[{"id":"R1","text":"adds the flag","test":"tdd"}],` +
	`"batches":[{"n":1,"title":"flag","covers":["R1"],"change":[{"at":"cmd/main.go:10","what":"parse the flag"}],` +
	`"test":{"kind":"new","name":"TestFlag","accept":"go test ./cmd/"},"risk":"low"}]}`

func TestFeatureFlowEndToEnd(t *testing.T) {
	repo := flowRepo(t)
	request := "add a flag\nwith  two lines"

	out := hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": repo, "prompt": "/nxy:feature " + request})
	if !strings.Contains(out, "nxy:planner") {
		t.Fatalf("no planner dispatch: %s", out)
	}
	id := featureID(t)

	out = hookCall(t, map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Agent", "cwd": repo,
		"tool_input": map[string]any{"subagent_type": "nxy:planner", "prompt": "feature " + id}})
	var upd map[string]any
	if err := json.Unmarshal([]byte(out), &upd); err != nil {
		t.Fatalf("bad json %q: %v", out, err)
	}
	b, _ := json.Marshal(upd)
	if !strings.Contains(string(b), "updatedInput") || !strings.Contains(string(b), "with  two lines") {
		t.Fatalf("prompt not injected with the literal request: %s", out)
	}

	if code, _, errb := runCmd(t, `{"title":"x","bogus":1}`, "plan", "save", "--feature", id); code != 1 || !strings.Contains(errb, "bogus") {
		t.Fatalf("unknown field: code %d, stderr %q", code, errb)
	}
	if code, _, errb := runCmd(t, `{"title":"","rules":[],"batches":[]}`, "plan", "save", "--feature", id); code != 1 || errb == "" {
		t.Fatalf("invalid plan: code %d, stderr %q", code, errb)
	}
	code, stdout, errb := runCmd(t, flowPlan, "plan", "save", "--feature", id)
	if code != 0 || !strings.Contains(stdout, "plan r1 saved") {
		t.Fatalf("valid plan: code %d, out %q, err %q", code, stdout, errb)
	}

	out = hookCall(t, map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Agent", "cwd": repo,
		"tool_input": map[string]any{"subagent_type": "nxy:planner", "prompt": "x"}})
	if !strings.Contains(out, "additionalContext") || !strings.Contains(out, "verbatim") {
		t.Fatalf("no checkpoint to show: %s", out)
	}

	question := "Approve nxy plan " + id + " r1?"
	hookCall(t, map[string]any{"hook_event_name": "PostToolUse", "tool_name": "AskUserQuestion", "cwd": repo,
		"tool_response": map[string]any{"answers": map[string]any{question: "Approve"}}})
	if _, out, _ := runCmd(t, "", "status"); !strings.Contains(out, "Lote 1") {
		t.Fatalf("status after approve: %q", out)
	}

	edit := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Edit", "cwd": repo,
		"tool_input": map[string]any{"file_path": filepath.Join(repo, "a.go")}}
	if out := hookCall(t, edit); !strings.Contains(out, `"deny"`) {
		t.Fatalf("edit not denied: %q", out)
	}

	planFile := filepath.Join(os.Getenv("NXY_HOME"), "plans", id+".md")
	if err := os.MkdirAll(filepath.Dir(planFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planFile, []byte("# plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := runCmd(t, "", "feature", "stop"); code != 0 || !strings.Contains(out, "stopped") {
		t.Fatalf("stop: code %d, out %q", code, out)
	}
	if _, err := os.Stat(planFile); !os.IsNotExist(err) {
		t.Fatalf("plan file not deleted: %v", err)
	}
	if _, out, _ := runCmd(t, "", "status"); !strings.Contains(out, "no active feature") {
		t.Fatalf("status after stop: %q", out)
	}
	if out := hookCall(t, edit); out != "" {
		t.Fatalf("edit denied after stop: %q", out)
	}
	if code, _, _ := runCmd(t, "", "feature", "stop"); code != 1 {
		t.Fatalf("second stop exit %d", code)
	}
}

func TestBashHookDoesNotOpenDB(t *testing.T) {
	repo := flowRepo(t)
	hookCall(t, map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "cwd": repo,
		"tool_input": map[string]any{"command": "git status"}})
	if _, err := os.Stat(filepath.Join(os.Getenv("NXY_HOME"), "nxy.db")); !os.IsNotExist(err) {
		t.Fatalf("db opened by a Bash hook: %v", err)
	}
}
