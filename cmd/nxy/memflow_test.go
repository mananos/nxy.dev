package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/plan"
	"github.com/mananos/nxy.dev/v2/internal/review"
)

// memPlan is batchPlan with a repo decision (it carries the question a rule needs) and a suite.
func memPlan() string {
	p := strings.Replace(batchPlan("test -f done.txt"), `"criteria"`,
		`"decisions":[{"scope":"repo","text":"Flags are parsed in one place","question":"Where do flags get parsed?"}],"criteria"`, 1)
	return strings.Replace(p, `"risk":"low"}]}`, `"risk":"low"}],"suite":"test -f done.txt"}`, 1)
}

func memShow(t *testing.T) string {
	t.Helper()
	code, out, errb := runCmd(t, "", "mem", "show")
	if code != 0 {
		t.Fatalf("mem show: code %d, err %q", code, errb)
	}
	return out
}

func TestMemoryFlowEndToEnd(t *testing.T) {
	needSh(t)
	repo := flowRepo(t)
	t.Setenv("NXY_CODEGRAPH_PATH", filepath.Join(repo, "no-such-codegraph"))
	if err := os.MkdirAll(filepath.Join(repo, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "cmd", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code, out, errb := runCmd(t, "", "mem", "rule", "Prefer table tests", "--question", "How do we write tests?"); code != 0 || !strings.Contains(out, "guardé la regla K1") {
		t.Fatalf("mem rule: code %d, out %q, err %q", code, out, errb)
	}

	hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": repo, "prompt": "/nxy:feature add a flag"})
	id := featureID(t)
	if out := agentPre(t, repo, "nxy:planner", "feature "+id); !strings.Contains(out, "K1: Prefer table tests") {
		t.Fatalf("planner prompt without K1: %s", out)
	}
	if code, out, errb := runCmd(t, memPlan(), "plan", "save", "--feature", id); code != 0 {
		t.Fatalf("plan save: code %d, out %q, err %q", code, out, errb)
	}
	agentPost(t, repo, "nxy:planner", nil)
	answer(t, repo, "¿Aprobás el plan "+id+" r1 de nxy?", "Aprobar")

	if out := agentPre(t, repo, "nxy:implementer", "feature "+id+" batch 1"); !strings.Contains(out, "K1: Prefer table tests") {
		t.Fatalf("implementer prompt without K1: %s", out)
	}
	if code, out, errb := runCmd(t, "", "batch", "red", "--feature", id); code != 0 {
		t.Fatalf("red: code %d, out %q, err %q", code, out, errb)
	}
	if err := os.WriteFile(filepath.Join(repo, "done.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errb := runCmd(t, "", "batch", "green", "--feature", id); code != 0 {
		t.Fatalf("green: code %d, out %q, err %q", code, out, errb)
	}
	hookCall(t, map[string]any{"hook_event_name": "SubagentStop", "cwd": repo, "agent_id": "a1", "agent_type": "nxy:implementer",
		"last_assistant_message": "batch 1 done\nlearned: main parses flags before the config loads | files: cmd/main.go"})
	if out := agentPost(t, repo, "nxy:implementer", nil); !strings.Contains(out, "affected --feature "+id) {
		t.Fatalf("last green does not name affected: %s", out)
	}
	if show := memShow(t); !strings.Contains(show, "A1 · main parses flags before the config loads") {
		t.Fatalf("A1 not harvested: %s", show)
	}

	if code, out, errb := runCmd(t, "", "affected", "--feature", id); code != 0 || !strings.Contains(out, "nxy:reviewer-quick") {
		t.Fatalf("affected: code %d, out %q, err %q", code, out, errb)
	}
	if out := agentPre(t, repo, "nxy:reviewer-quick", "feature "+id+" review base"); !strings.Contains(out, "K1: Prefer table tests") {
		t.Fatalf("reviewer prompt without K1: %s", out)
	}
	save := `{"lens":"base","criteria":[{"id":"R1","ok":true}],"findings":[` +
		`{"severity":"low","file":"cmd/main.go","line":10,"title":"flag sin validar",` +
		`"scenario":"flag vacío → panic al parsear","fix":"validar antes de parsear","rule":"K1"}],` +
		`"learnings":[{"text":"the flag parser panics on an empty value","files":["cmd/main.go"]}]}`
	if code, out, errb := runCmd(t, save, "review", "save", "--feature", id, "--lens", "base"); code != 0 {
		t.Fatalf("review save: code %d, out %q, err %q", code, out, errb)
	}
	if code, out, _ := runCmd(t, "", "review", "show"); code != 0 || !strings.Contains(out, "base · media] flag sin validar") {
		t.Fatalf("a finding that breaks a rule stays low: code %d, out %q", code, out)
	}
	out := agentPost(t, repo, "nxy:reviewer-quick", nil)
	if !strings.Contains(out, "AskUserQuestion") || !strings.Contains(out, "H1") {
		t.Fatalf("no findings checkpoint: %s", out)
	}
	out = answer(t, repo, review.FixQuestion(id, 1, 1), review.NoneLabel)
	if !strings.Contains(out, plan.RulesQuestionPrefix(id)) || !strings.Contains(out, "Flags are parsed in one place") {
		t.Fatalf("no closing question: %s", out)
	}
	answer(t, repo, plan.RulesQuestion(id, 1, 1), "S1 Flags are parsed in one place")
	if _, st, _ := runCmd(t, "", "status"); !strings.Contains(st, "no hay una feature activa") {
		t.Fatalf("feature still active: %q", st)
	}

	show := memShow(t)
	for _, want := range []string{"K2 · Flags are parsed in one place", "resuelve: Where do flags get parsed? · plan", "A2 · the flag parser panics"} {
		if !strings.Contains(show, want) {
			t.Fatalf("mem show misses %q: %s", want, show)
		}
	}
	m := regexp.MustCompile(`K1 · Prefer table tests\n.*entregada (\d+) · citada (\d+)`).FindStringSubmatch(show)
	if m == nil || m[1] == "0" || m[1] == "1" || m[2] != "1" {
		t.Fatalf("K1 counters: %v in %s", m, show)
	}
	if code, out, _ := runCmd(t, "", "mem", "related", "cmd/main.go"); code != 0 || !strings.Contains(out, id) || !strings.Contains(out, "Flag") {
		t.Fatalf("related does not list the closed feature: code %d, out %q", code, out)
	}

	// another feature stopped before the closing leaves no rules
	hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": repo, "prompt": "/nxy:feature add another flag"})
	id2 := featureID(t)
	agentPre(t, repo, "nxy:planner", "feature "+id2)
	if code, out, errb := runCmd(t, strings.Replace(memPlan(), "Flags are parsed in one place", "Errors are wrapped", 1), "plan", "save", "--feature", id2); code != 0 {
		t.Fatalf("plan save 2: code %d, out %q, err %q", code, out, errb)
	}
	if code, out, _ := runCmd(t, "", "feature", "stop"); code != 0 || !strings.Contains(out, "paré la feature") {
		t.Fatalf("stop: code %d, out %q", code, out)
	}
	if after := memShow(t); strings.Contains(after, "Errors are wrapped") || strings.Contains(after, "K3") {
		t.Fatalf("a stopped feature left a rule: %s", after)
	}
}
