package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/platform"
	"github.com/mananos/nxy.dev/internal/store/sqlite"
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

func TestBackgroundPlannerEndToEnd(t *testing.T) {
	repo := flowRepo(t)
	hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": repo, "prompt": "/nxy:feature add a flag"})
	id := featureID(t)

	hookCall(t, map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Agent", "cwd": repo,
		"tool_input": map[string]any{"subagent_type": "nxy:planner", "prompt": "feature " + id}})
	out := hookCall(t, map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Agent", "cwd": repo,
		"tool_input":    map[string]any{"subagent_type": "nxy:planner", "prompt": "x"},
		"tool_response": map[string]any{"isAsync": true, "status": "async_launched", "agentId": "a1"}})
	if !strings.Contains(out, "background") {
		t.Fatalf("launch does not say wait: %q", out)
	}
	if _, st, _ := runCmd(t, "", "status"); !strings.Contains(st, "state: Trabajando\nstep: Planificando\n") {
		t.Fatalf("status after launch: %q", st)
	}

	if code, so, errb := runCmd(t, flowPlan, "plan", "save", "--feature", id); code != 0 || !strings.Contains(so, "plan r1 saved") {
		t.Fatalf("plan save: code %d, out %q, err %q", code, so, errb)
	}
	if out := hookCall(t, map[string]any{"hook_event_name": "SubagentStop", "cwd": repo,
		"agent_id": "a1", "agent_type": "nxy:planner"}); out != "" {
		t.Fatalf("SubagentStop should print nothing: %q", out)
	}
	out = hookCall(t, map[string]any{"hook_event_name": "Stop", "cwd": repo})
	if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "do not repeat it") || !strings.Contains(out, "AskUserQuestion") {
		t.Fatalf("Stop did not deliver the checkpoint: %q", out)
	}
	if out := hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": repo, "prompt": "sigo"}); out != "" {
		t.Fatalf("checkpoint delivered twice: %q", out)
	}
}

func TestPlanSaveOnStoppedFeature(t *testing.T) {
	repo := flowRepo(t)
	hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": repo, "prompt": "/nxy:feature add a flag"})
	id := featureID(t)
	if code, _, _ := runCmd(t, "", "feature", "stop"); code != 0 {
		t.Fatalf("stop exit %d", code)
	}
	code, _, errb := runCmd(t, flowPlan, "plan", "save", "--feature", id)
	if code != 1 || !strings.Contains(errb, "not planning") {
		t.Fatalf("save on stopped: code %d, stderr %q", code, errb)
	}
	path, err := platform.DBPath()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s, err := sqlite.Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	f, err := s.Feature(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if f.Plan != nil || f.PlanRev != 0 || f.State.Phase != domain.PhaseDone {
		t.Fatalf("stopped feature changed: plan %v, rev %d, phase %s", f.Plan, f.PlanRev, f.State.Phase)
	}
	if out := hookCall(t, map[string]any{"hook_event_name": "SubagentStop", "cwd": repo,
		"agent_id": "a1", "agent_type": "nxy:planner"}); out != "" {
		t.Fatalf("SubagentStop of a stopped feature printed %q", out)
	}
}

func batchPlan(accept string) string {
	return `{"title":"Flag","rules":[{"id":"R1","text":"adds the flag","test":"tdd"}],` +
		`"batches":[{"n":1,"title":"flag","covers":["R1"],"change":[{"at":"cmd/main.go:10","what":"parse the flag"}],` +
		`"test":{"kind":"new","name":"TestFlag","accept":` + strconvQuote(accept) + `},"risk":"low"}]}`
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func needSh(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
}

func agentPre(t *testing.T, repo, role, prompt string) string {
	t.Helper()
	return hookCall(t, map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Agent", "cwd": repo,
		"tool_input": map[string]any{"subagent_type": role, "prompt": prompt}})
}

func agentPost(t *testing.T, repo, role string, resp map[string]any) string {
	t.Helper()
	p := map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Agent", "cwd": repo,
		"tool_input": map[string]any{"subagent_type": role, "prompt": "x"}}
	if resp != nil {
		p["tool_response"] = resp
	}
	return hookCall(t, p)
}

func answer(t *testing.T, repo, question, label string) string {
	t.Helper()
	return hookCall(t, map[string]any{"hook_event_name": "PostToolUse", "tool_name": "AskUserQuestion", "cwd": repo,
		"tool_response": map[string]any{"answers": map[string]any{question: label}}})
}

func subagentWrite(t *testing.T, repo, name string) string {
	t.Helper()
	return hookCall(t, map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Write", "cwd": repo,
		"agent_id": "a1", "agent_type": "nxy:implementer",
		"tool_input": map[string]any{"file_path": filepath.Join(repo, name)}})
}

// approvedBatchFeature takes a feature through planning and approval; returns its id.
func approvedBatchFeature(t *testing.T, repo, accept string) string {
	t.Helper()
	hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": repo, "prompt": "/nxy:feature add a flag"})
	id := featureID(t)
	agentPre(t, repo, "nxy:planner", "feature "+id)
	if code, out, errb := runCmd(t, batchPlan(accept), "plan", "save", "--feature", id); code != 0 {
		t.Fatalf("plan save: code %d, out %q, err %q", code, out, errb)
	}
	agentPost(t, repo, "nxy:planner", nil)
	answer(t, repo, "Approve nxy plan "+id+" r1?", "Approve")
	return id
}

func TestBatchFlowEndToEnd(t *testing.T) {
	needSh(t)
	repo := flowRepo(t)
	id := approvedBatchFeature(t, repo, "test -f done.txt")

	out := agentPre(t, repo, "nxy:implementer", "feature "+id+" batch 1")
	if !strings.Contains(out, "updatedInput") || !strings.Contains(out, "test -f done.txt") || !strings.Contains(out, "add a flag") {
		t.Fatalf("implementer prompt not injected: %s", out)
	}
	if code, out, errb := runCmd(t, "", "batch", "green", "--feature", id); code != 1 {
		t.Fatalf("green before red: code %d, out %q, err %q", code, out, errb)
	}
	if code, out, errb := runCmd(t, "", "batch", "red", "--feature", id); code != 0 || !strings.Contains(out, "red recorded") {
		t.Fatalf("red: code %d, out %q, err %q", code, out, errb)
	}
	if out := subagentWrite(t, repo, "done.txt"); strings.Contains(out, "deny") {
		t.Fatalf("subagent write denied: %s", out)
	}
	if err := os.WriteFile(filepath.Join(repo, "done.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errb := runCmd(t, "", "batch", "green", "--feature", id); code != 0 || !strings.Contains(out, "green recorded") {
		t.Fatalf("green: code %d, out %q, err %q", code, out, errb)
	}
	out = agentPost(t, repo, "nxy:implementer", nil)
	if !strings.Contains(out, "all batches are green") {
		t.Fatalf("no next step after the last green batch: %s", out)
	}
}

func TestBatchRedRefusedWhenAcceptPasses(t *testing.T) {
	needSh(t)
	repo := flowRepo(t)
	id := approvedBatchFeature(t, repo, "exit 0")
	agentPre(t, repo, "nxy:implementer", "feature "+id+" batch 1")
	if code, out, errb := runCmd(t, "", "batch", "red", "--feature", id); code != 1 || strings.Contains(out, "red recorded") {
		t.Fatalf("red with a passing Accept: code %d, out %q, err %q", code, out, errb)
	}
}

func TestBatchRetryAndDecision(t *testing.T) {
	needSh(t)
	repo := flowRepo(t)
	id := approvedBatchFeature(t, repo, "test -f done.txt")

	agentPre(t, repo, "nxy:implementer", "feature "+id+" batch 1")
	if code, _, _ := runCmd(t, "", "batch", "red", "--feature", id); code != 0 {
		t.Fatalf("red exit %d", code)
	}
	if err := os.WriteFile(filepath.Join(repo, "done.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runCmd(t, "", "batch", "green", "--feature", id); code != 0 {
		t.Fatalf("green exit %d", code)
	}
	subagentWrite(t, repo, "late.txt") // a write after the green voids it
	if out := agentPost(t, repo, "nxy:implementer", nil); !strings.Contains(out, "failed verification") {
		t.Fatalf("no retry after a voided green: %s", out)
	}

	out := agentPre(t, repo, "nxy:implementer", "feature "+id+" batch 1")
	if !strings.Contains(out, "Previous attempt failed") {
		t.Fatalf("retry prompt without the failure: %s", out)
	}
	// the second attempt returns without proving anything
	out = agentPost(t, repo, "nxy:implementer", nil)
	if !strings.Contains(out, "Lote 1 fallido") {
		t.Fatalf("no batch_failed question: %s", out)
	}
	answer(t, repo, "nxy feature "+id+": batch 1 failed twice — what now?", "Parar")
	if _, st, _ := runCmd(t, "", "status"); !strings.Contains(st, "no active feature") {
		t.Fatalf("status after Parar: %q", st)
	}
}

func TestBackgroundImplementerEndToEnd(t *testing.T) {
	needSh(t)
	repo := flowRepo(t)
	id := approvedBatchFeature(t, repo, "test -f done.txt")

	agentPre(t, repo, "nxy:implementer", "feature "+id+" batch 1")
	out := agentPost(t, repo, "nxy:implementer", map[string]any{"isAsync": true, "status": "async_launched", "agentId": "a1"})
	if !strings.Contains(out, "background") {
		t.Fatalf("launch does not say wait: %q", out)
	}
	if out := agentPre(t, repo, "nxy:implementer", "feature "+id+" batch 1"); !strings.Contains(out, "deny") {
		t.Fatalf("second dispatch while in the background not denied: %q", out)
	}
	if code, _, _ := runCmd(t, "", "batch", "red", "--feature", id); code != 0 {
		t.Fatalf("red exit %d", code)
	}
	if err := os.WriteFile(filepath.Join(repo, "done.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runCmd(t, "", "batch", "green", "--feature", id); code != 0 {
		t.Fatalf("green exit %d", code)
	}
	if out := hookCall(t, map[string]any{"hook_event_name": "SubagentStop", "cwd": repo,
		"agent_id": "a1", "agent_type": "nxy:implementer"}); out != "" {
		t.Fatalf("SubagentStop should print nothing: %q", out)
	}
	out = hookCall(t, map[string]any{"hook_event_name": "Stop", "cwd": repo})
	if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "all batches are green") {
		t.Fatalf("Stop did not deliver the result: %q", out)
	}
	if out := hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": repo, "prompt": "sigo"}); out != "" {
		t.Fatalf("result delivered twice: %q", out)
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
