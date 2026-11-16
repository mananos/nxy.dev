package main

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestMemGlobalRuleReachesOnlyItsRoles(t *testing.T) {
	needSh(t)
	repo := flowRepo(t)
	t.Setenv("NXY_CODEGRAPH_PATH", filepath.Join(repo, "no-such-codegraph"))

	code, out, errb := runCmd(t, "", "mem", "rule", "Answer in Spanish", "--global", "--roles", "planner", "--question", "Which language do we write in?")
	if code != 0 || !strings.Contains(out, "saved global rule G1") {
		t.Fatalf("mem rule --global: code %d, out %q, err %q", code, out, errb)
	}
	if code, _, _ := runCmd(t, "", "mem", "rule", "Roles on a repo rule", "--roles", "planner", "--question", "q?"); code == 0 {
		t.Fatal("--roles without --global was accepted")
	}

	hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": repo, "prompt": "/nxy:feature add a flag"})
	id := featureID(t)
	out = agentPre(t, repo, "nxy:planner", "feature "+id)
	if !strings.Contains(out, "G1:") || !strings.Contains(out, "the K rule wins") {
		t.Fatalf("planner prompt without G1 and the precedence line: %s", out)
	}
	if code, out, errb := runCmd(t, memPlan(), "plan", "save", "--feature", id); code != 0 {
		t.Fatalf("plan save: code %d, out %q, err %q", code, out, errb)
	}
	agentPost(t, repo, "nxy:planner", nil)
	answer(t, repo, "Approve nxy plan "+id+" r1?", "Approve")
	if out := agentPre(t, repo, "nxy:implementer", "feature "+id+" batch 1"); strings.Contains(out, "G1:") {
		t.Fatalf("implementer prompt carries a planner-only G1: %s", out)
	}
}

func TestMemGlobalEditByULIDAndEmptyRef(t *testing.T) {
	flowRepo(t)
	if code, _, _ := runCmd(t, "", "mem", "note", "a learning", "--files", "/srv/dev/a.go"); code != 0 {
		t.Fatal("note failed")
	}
	code, out, errb := runCmd(t, "", "mem", "export")
	if code != 0 {
		t.Fatalf("export: %d %q", code, errb)
	}
	m := regexp.MustCompile(`"id":\s*"([0-9A-Za-z]{20,})"`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no id in export: %s", out)
	}
	if code, out, errb := runCmd(t, "", "mem", "edit", m[1], "--text", "learning edited"); code != 0 || !strings.Contains(out, "edited A1") {
		t.Fatalf("edit by ULID: code %d, out %q, err %q", code, out, errb)
	}
	if show := memShow(t); !strings.Contains(show, "learning edited") {
		t.Fatalf("learning not edited: %s", show)
	}
	if code, _, errb := runCmd(t, "", "mem", "edit", "", "--text", "x"); code == 0 || strings.Contains(errb, "panic") {
		t.Fatalf("empty ref: code %d, err %q", code, errb)
	}
}

func TestMemGlobalEditRemoveRestore(t *testing.T) {
	flowRepo(t)
	if code, out, errb := runCmd(t, "", "mem", "rule", "Global one", "--global", "--question", "q?"); code != 0 {
		t.Fatalf("global rule: code %d, out %q, err %q", code, out, errb)
	}
	if code, out, errb := runCmd(t, "", "mem", "rule", "Keep tabs", "--question", "Tabs or spaces?"); code != 0 || !strings.Contains(out, "saved rule K1") {
		t.Fatalf("rule: code %d, out %q, err %q", code, out, errb)
	}
	if code, out, errb := runCmd(t, "", "mem", "edit", "G1", "--text", "Global edited"); code != 0 || !strings.Contains(out, "edited G1") {
		t.Fatalf("edit G1: code %d, out %q, err %q", code, out, errb)
	}
	if show := memShow(t); !strings.Contains(show, "G1 · Global edited") || !strings.Contains(show, "K1 · Keep tabs") {
		t.Fatalf("show misses the edit: %s", show)
	}
	code, out, errb := runCmd(t, "", "mem", "rm", "K1")
	if code != 0 || !strings.Contains(out, "deleted K1") {
		t.Fatalf("rm K1: code %d, out %q, err %q", code, out, errb)
	}
	if show := memShow(t); strings.Contains(show, "K1 · Keep tabs") {
		t.Fatalf("K1 still shown after rm: %s", show)
	}
	if code, out, errb := runCmd(t, "", "mem", "restore", "K1"); code != 0 || !strings.Contains(out, "restored K1") {
		t.Fatalf("restore K1: code %d, out %q, err %q", code, out, errb)
	}
	if show := memShow(t); !strings.Contains(show, "K1 · Keep tabs") {
		t.Fatalf("K1 not back after restore: %s", show)
	}
	if code, out, errb := runCmd(t, "", "mem", "keep", "K1"); code != 0 || !strings.Contains(out, "kept K1: nxy no la vuelve a proponer") {
		t.Fatalf("keep K1: code %d, out %q, err %q", code, out, errb)
	}
	if code, _, _ := runCmd(t, "", "mem", "keep", "K99"); code == 0 {
		t.Fatal("keep of an unknown ref must fail")
	}

	if code, _, _ := runCmd(t, "", "mem", "note", "a learning", "--files", "/srv/dev/a.go"); code != 0 {
		t.Fatal("note failed")
	}
	if code, _, errb := runCmd(t, "", "mem", "edit", "A1"); code == 0 || !strings.Contains(errb, "nothing to change") {
		t.Fatalf("edit A1 with no flag: code %d, err %q", code, errb)
	}
	if code, _, _ := runCmd(t, "", "mem", "edit", "A1", "--question", "q?"); code == 0 {
		t.Fatal("--question on a learning was accepted")
	}
}
