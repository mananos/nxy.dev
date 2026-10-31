package main

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/review"
)

const baseWithFinding = `{"lens":"base","rules":[{"id":"R1","ok":true}],"findings":[` +
	`{"severity":"medium","file":"cmd/main.go","line":10,"title":"flag sin validar",` +
	`"scenario":"flag vacío → panic al parsear","fix":"validar antes de parsear"}]}`

const baseClean = `{"lens":"base","rules":[{"id":"R1","ok":true}],"findings":[]}`

func quickReview(t *testing.T, saveJSON string) (repo, id string) {
	t.Helper()
	repo, id = affectedFeature(t, "test -f done.txt")
	code, out, errb := runCmd(t, "", "affected", "--feature", id)
	if code != 0 || !strings.Contains(out, "nxy:reviewer-quick") || !strings.Contains(out, "feature "+id+" review base") {
		t.Fatalf("affected green does not dispatch the reviewer: code %d, out %q, err %q", code, out, errb)
	}
	if out := agentPre(t, repo, "nxy:reviewer-quick", "feature "+id+" review base"); !strings.Contains(out, "updatedInput") || !strings.Contains(out, "adds the flag") {
		t.Fatalf("reviewer prompt not injected: %s", out)
	}
	if code, _, errb := runCmd(t, `{"lens":"base","findings":[]}`, "review", "save", "--feature", id, "--lens", "base"); code != 1 || errb == "" {
		t.Fatalf("incomplete rules accepted: code %d, err %q", code, errb)
	}
	code, out, errb = runCmd(t, saveJSON, "review", "save", "--feature", id, "--lens", "base")
	if code != 0 || !strings.Contains(out, "review base guardada (1 de 1)") {
		t.Fatalf("review save: code %d, out %q, err %q", code, out, errb)
	}
	return repo, id
}

func TestReviewFindingsFixAndDone(t *testing.T) {
	repo, id := quickReview(t, baseWithFinding)
	out := agentPost(t, repo, "nxy:reviewer-quick", nil)
	if !strings.Contains(out, "AskUserQuestion") || !strings.Contains(out, "H1") || !strings.Contains(out, "multiSelect") {
		t.Fatalf("no findings checkpoint: %s", out)
	}
	out = answer(t, repo, review.FixQuestion(id, 1, 1), "H1 flag sin validar")
	if !strings.Contains(out, "review fix") {
		t.Fatalf("no fixer dispatch after choosing: %s", out)
	}
	out = agentPre(t, repo, "nxy:implementer", "feature "+id+" review fix")
	if !strings.Contains(out, "updatedInput") || !strings.Contains(out, "flag vacío") {
		t.Fatalf("fix prompt without the finding: %s", out)
	}
	if out := agentPost(t, repo, "nxy:implementer", nil); !strings.Contains(out, "affected --feature "+id) {
		t.Fatalf("fixer return does not name affected: %s", out)
	}
	code, out, errb := runCmd(t, "", "affected", "--feature", id)
	if code != 0 || !strings.Contains(out, "Listo") || !strings.Contains(out, "R1 ✔") {
		t.Fatalf("no Listo after verified fixes: code %d, out %q, err %q", code, out, errb)
	}
	if _, st, _ := runCmd(t, "", "status"); !strings.Contains(st, "no active feature") {
		t.Fatalf("feature still active: %q", st)
	}
	if out := hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": repo, "prompt": "sigo"}); out != "" {
		t.Fatalf("Listo delivered twice: %q", out)
	}
}

func TestReviewWithoutFindingsIsDone(t *testing.T) {
	repo, _ := quickReview(t, baseClean)
	out := agentPost(t, repo, "nxy:reviewer-quick", nil)
	if !strings.Contains(out, "Listo") || !strings.Contains(out, "R1 ✔") {
		t.Fatalf("clean review does not end in Listo: %s", out)
	}
	if out := hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": repo, "prompt": "sigo"}); out != "" {
		t.Fatalf("Listo delivered twice: %q", out)
	}
}

func TestReviewSaveUsage(t *testing.T) {
	flowRepo(t)
	if code, _, errb := runCmd(t, "", "review"); code != 1 || !strings.Contains(errb, "usage") {
		t.Fatalf("review without subcommand: code %d, err %q", code, errb)
	}
	if code, _, errb := runCmd(t, "{}", "review", "save", "--feature", "x"); code != 1 || !strings.Contains(errb, "usage") {
		t.Fatalf("save without --lens: code %d, err %q", code, errb)
	}
}
