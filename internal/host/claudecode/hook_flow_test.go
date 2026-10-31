package claudecode

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/app"
	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/plan"
	"github.com/mananos/nxy.dev/internal/platform"
)

func TestDecodeNewFields(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	ev, err := Decode(strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"Agent","cwd":"/tmp/x",
"agent_id":"a1","agent_type":"nxy:planner","prompt":"/nxy:feature hola","source":"resume","tool_use_id":"tu1",
"tool_input":{"subagent_type":"nxy:planner","prompt":"feature f1","file_path":"/repo/a.go"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if ev.AgentID != "a1" || ev.AgentType != "nxy:planner" || ev.Prompt != "/nxy:feature hola" || ev.Source != "resume" ||
		ev.ToolUseID != "tu1" || ev.Subagent != "nxy:planner" || ev.AgentPrompt != "feature f1" || ev.FilePath != "/repo/a.go" {
		t.Fatalf("got %+v", ev)
	}
	ev, _ = Decode(strings.NewReader(`{"tool_name":"NotebookEdit","tool_input":{"notebook_path":"/repo/n.ipynb"}}`))
	if ev.FilePath != "/repo/n.ipynb" {
		t.Fatalf("notebook path: %+v", ev)
	}
	ev, _ = Decode(strings.NewReader(`{"hook_event_name":"PreToolUse","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"Q1?"},{"question":"Q2?"}],"answers":{"Q1?":"x"}}}`))
	if len(ev.Questions) != 2 || ev.Questions[1] != "Q2?" || !ev.InputHasAnswers {
		t.Fatalf("questions: %+v", ev)
	}
}

func TestDecodeAgentAsync(t *testing.T) {
	post := func(tool, event, resp string) bool {
		ev, err := Decode(strings.NewReader(`{"hook_event_name":"` + event + `","tool_name":"` + tool + `","tool_response":` + resp + `}`))
		if err != nil {
			t.Fatal(err)
		}
		return ev.AgentAsync
	}
	for _, r := range []string{
		`{"status":"async_launched"}`,
		`"Async agent launched successfully. agentId: x"`,
		`[{"type":"text","text":"Async agent launched successfully."}]`,
		`{"content":[{"type":"text","text":"Async agent launched successfully."}]}`,
		`{"isAsync":true,"status":"async_launched","agentId":"x1"}`,
	} {
		if !post("Agent", "PostToolUse", r) {
			t.Errorf("should be async: %s", r)
		}
	}
	normal := `{"content":[{"type":"text","text":"plan r1 saved"}]}`
	if post("Agent", "PostToolUse", normal) {
		t.Error("normal result is not async")
	}
	if post("Agent", "PreToolUse", `{"status":"async_launched"}`) {
		t.Error("PreToolUse is not async")
	}
	if post("Bash", "PostToolUse", `{"status":"async_launched"}`) {
		t.Error("Bash is not async")
	}
}

func TestDecodeFilePathNative(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	ev, _ := Decode(strings.NewReader(`{"tool_name":"Edit","cwd":"/tmp/x","tool_input":{"file_path":"/c/repo/a.go"}}`))
	if want := platform.NativePath("/c/repo/a.go"); ev.FilePath != want {
		t.Errorf("FilePath = %q, want %q", ev.FilePath, want)
	}
	if ev.ProjectDir != platform.NativePath("/tmp/x") {
		t.Errorf("ProjectDir = %q", ev.ProjectDir)
	}
}

func TestDecodeAnswers(t *testing.T) {
	ev, _ := Decode(strings.NewReader(`{"hook_event_name":"PostToolUse","tool_name":"AskUserQuestion","tool_response":{"answers":{"Q?":"Approve"}}}`))
	if ev.Answers["Q?"] != "Approve" {
		t.Fatalf("got %+v", ev.Answers)
	}
}

func TestDecodeAnswersFromTranscript(t *testing.T) {
	dir := t.TempDir()
	tp := filepath.Join(dir, "t.jsonl")
	lines := []string{
		`{"type":"user","isSidechain":true,"message":{"content":[{"type":"tool_result","tool_use_id":"tu1"}]},"toolUseResult":{"answers":{"Q?":"Side"}}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu1","name":"AskUserQuestion"}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu9"}]},"toolUseResult":{"answers":{"Q?":"Other"}}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1"}]},"toolUseResult":{"answers":{"Q?":"Change"}}}`,
	}
	if err := os.WriteFile(tp, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse", "tool_name": "AskUserQuestion",
		"tool_use_id": "tu1", "transcript_path": tp, "tool_response": map[string]any{},
	})
	ev, err := Decode(bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Answers["Q?"] != "Change" {
		t.Fatalf("got %+v", ev.Answers)
	}
}

func encode(t *testing.T, ev app.HookEvent, resp app.HookResponse) map[string]any {
	t.Helper()
	var b bytes.Buffer
	if err := Encode(&b, ev, resp); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b.Bytes(), &m); err != nil {
		t.Fatalf("output %q: %v", b.String(), err)
	}
	return m
}

func TestEncodeResponses(t *testing.T) {
	m := encode(t, app.HookEvent{Name: "PreToolUse"}, app.HookResponse{Deny: "no"})
	h := m["hookSpecificOutput"].(map[string]any)
	if h["permissionDecision"] != "deny" || h["permissionDecisionReason"] != "no" {
		t.Errorf("deny: %v", m)
	}

	m = encode(t, app.HookEvent{Name: "PreToolUse", ToolInput: json.RawMessage(`{"subagent_type":"nxy:planner","prompt":"old"}`)},
		app.HookResponse{Prompt: "new <b>"})
	h = m["hookSpecificOutput"].(map[string]any)
	ui := h["updatedInput"].(map[string]any)
	if h["permissionDecision"] != "allow" || ui["prompt"] != "new <b>" || ui["subagent_type"] != "nxy:planner" {
		t.Errorf("prompt: %v", m)
	}

	m = encode(t, app.HookEvent{Name: "UserPromptSubmit"}, app.HookResponse{Deny: "uso"})
	if m["decision"] != "block" || m["reason"] != "uso" {
		t.Errorf("block: %v", m)
	}

	f := domain.Feature{ID: "f1", Next: domain.Action{Kind: domain.DispatchPlanner}}
	for _, name := range []string{"UserPromptSubmit", "SessionStart", "PostToolUse"} {
		m = encode(t, app.HookEvent{Name: name}, app.HookResponse{Next: &app.Next{Feature: f}, Notice: "aviso"})
		h = m["hookSpecificOutput"].(map[string]any)
		if h["hookEventName"] != name || !strings.HasPrefix(h["additionalContext"].(string), "nxy: feature f1") || m["systemMessage"] != "aviso" {
			t.Errorf("%s: %v", name, m)
		}
	}

	m = encode(t, app.HookEvent{Name: "Stop"}, app.HookResponse{Next: &app.Next{Feature: f}})
	if m["decision"] != "block" || !strings.Contains(m["reason"].(string), "nxy: feature") {
		t.Errorf("stop: %v", m)
	}
	var b bytes.Buffer
	if err := Encode(&b, app.HookEvent{Name: "Stop"}, app.HookResponse{}); err != nil || b.Len() != 0 {
		t.Errorf("stop zero: %q %v", b.String(), err)
	}
	m = encode(t, app.HookEvent{Name: "PostToolUse"}, app.HookResponse{Context: "espera"})
	h = m["hookSpecificOutput"].(map[string]any)
	if h["additionalContext"] != "espera" {
		t.Errorf("context: %v", m)
	}
}

func approvalFeature() domain.Feature {
	return domain.Feature{
		ID: "f1", PlanRev: 2,
		Plan: &domain.Plan{
			Title: "Titulo del plan",
			Rules: []domain.Rule{{ID: "R1", Text: "comportamiento uno", Test: "tdd"}},
		},
		Next: domain.Action{Kind: domain.AskApproval},
	}
}

func TestQuestionsInputFallsBackToAskingAgain(t *testing.T) {
	f := approvalFeature()
	f.Plan.Questions = []domain.Question{{Question: "Cual?", Header: "H", Options: []domain.Option{{Label: "A", Description: "a", Recommended: true}, {Label: "B", Description: "b"}}}}
	f.Answers = []domain.Answer{{Question: "Cual?", Answer: "A"}}
	f.Next = domain.Action{Kind: domain.AskQuestions}
	if s := Instruction(f, ""); !strings.Contains(s, `"question":"Cual?"`) {
		t.Errorf("nothing left should ask again: %s", s)
	}
}

func TestInstructionPerAction(t *testing.T) {
	f := approvalFeature()
	s := Instruction(f, "")
	for _, want := range []string{"verbatim", plan.Checkpoint(*f.Plan), plan.ApprovalQuestion("f1", 2), plan.Approve, plan.Change, plan.ViewFull, "AskUserQuestion"} {
		if !strings.Contains(s, want) {
			t.Errorf("AskApproval missing %q in:\n%s", want, s)
		}
	}

	f.Plan.Questions = []domain.Question{{Question: "Cual?", Header: "H", Options: []domain.Option{{Label: "A", Description: "a", Recommended: true}, {Label: "B", Description: "b"}}}}
	f.Next = domain.Action{Kind: domain.AskQuestions}
	if s := Instruction(f, ""); !strings.Contains(s, "verbatim") || !strings.Contains(s, `"question":"Cual?"`) {
		t.Errorf("AskQuestions: %s", s)
	}

	f.Next = domain.Action{Kind: domain.DispatchPlanner}
	if s := Instruction(f, ""); !strings.Contains(s, "`nxy:planner`") || !strings.Contains(s, "feature f1") {
		t.Errorf("planner: %s", s)
	}
	f.Next = domain.Action{Kind: domain.AskChangeNote}
	if s := Instruction(f, ""); !strings.HasPrefix(s, "nxy:") || !strings.Contains(s, "next message") {
		t.Errorf("change note: %s", s)
	}
	f.Next = domain.Action{Kind: domain.DispatchImplementer, Batch: 2}
	if s := Instruction(f, "nota final"); !strings.Contains(s, "`nxy:implementer`") || !strings.Contains(s, "feature f1 batch 2") || !strings.Contains(s, "background") || !strings.HasSuffix(s, "nota final") {
		t.Errorf("implementer: %s", s)
	}
	f.Next = domain.Action{Kind: domain.DispatchImplementer, Batch: 2, Retry: true}
	if s := Instruction(f, ""); !strings.Contains(s, "batch 2 failed verification") || !strings.Contains(s, "retries once") {
		t.Errorf("retry: %s", s)
	}
	f.Next = domain.Action{Kind: domain.AskDecision}
	f.State = domain.State{Phase: domain.PhaseBatchFailed, Batch: 2}
	f.Proof = &domain.Proof{Batch: 2, Failure: "green never passed: boom"}
	s = Instruction(f, "")
	for _, want := range []string{"--- Lote 2 fallido ---", "green never passed: boom", plan.DecisionQuestion("f1", 2), plan.Retry, plan.Continue, plan.Stop} {
		if !strings.Contains(s, want) {
			t.Errorf("batch_failed missing %q in:\n%s", want, s)
		}
	}
	st := StopInstruction(f, "")
	if strings.Count(st, "green never passed: boom") != 1 || !strings.Contains(st, "do not repeat it") {
		t.Errorf("stop batch_failed: %s", st)
	}
	f.State = domain.State{Phase: domain.PhasePlannerFailed}
	if s := Instruction(f, ""); !strings.Contains(s, plan.PlannerFailedQuestion("f1")) || !strings.Contains(s, plan.Retry) || strings.Contains(s, plan.Continue) {
		t.Errorf("planner_failed: %s", s)
	}
	f.Next = domain.Action{Kind: domain.RunAffected}
	f.State = domain.State{Phase: domain.PhaseAffected}
	if s := Instruction(f, ""); !strings.Contains(s, "all batches are green") || !strings.Contains(s, "`nxy feature stop`") {
		t.Errorf("affected: %s", s)
	}
	af := approvalFeature()
	if s := StopInstruction(af, ""); strings.Count(s, plan.Checkpoint(*af.Plan)) != 1 || !strings.Contains(s, "do not repeat it") || strings.Contains(s, "verbatim") {
		t.Errorf("stop approval: %s", s)
	}
	f.Next = domain.Action{Kind: domain.DispatchReviewer}
	f.State = domain.State{Phase: domain.PhaseReviewing}
	if s := Instruction(f, ""); !strings.Contains(s, "not available yet") {
		t.Errorf("default: %s", s)
	}
}

func TestLegacyActive(t *testing.T) {
	write := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name, settings, installed string
		want                      bool
	}{
		{"active 1.x", `{"enabledPlugins":{"nxy@mk":true}}`, `{"plugins":{"nxy@mk":[{"version":"1.3.2"}]}}`, true},
		{"disabled", `{"enabledPlugins":{"nxy@mk":false}}`, `{"plugins":{"nxy@mk":[{"version":"1.3.2"}]}}`, false},
		{"2.x", `{"enabledPlugins":{"nxy@mk":true}}`, `{"plugins":{"nxy@mk":[{"version":"2.0.0"}]}}`, false},
		{"other plugin", `{"enabledPlugins":{"foo@mk":true}}`, `{"plugins":{"foo@mk":[{"version":"1.0.0"}]}}`, false},
		{"no installed file", `{"enabledPlugins":{"nxy@mk":true}}`, ``, false},
		{"no settings", ``, `{"plugins":{"nxy@mk":[{"version":"1.3.2"}]}}`, false},
	}
	for _, c := range cases {
		home, proj := t.TempDir(), t.TempDir()
		if c.settings != "" {
			write(filepath.Join(home, ".claude", "settings.json"), c.settings)
		}
		if c.installed != "" {
			write(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), c.installed)
		}
		if got := LegacyActive(home, proj); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
