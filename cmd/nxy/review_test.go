package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/docs"
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

// docsReview takes a feature that changes cmd/greeter.go through a clean quick review; the repo
// holds `files` first. It returns what the main thread sees when the reviewer comes back.
func docsReview(t *testing.T, files map[string]string) (repo, id, out string) {
	t.Helper()
	repo, id = affectedFeatureWith(t, "test -f done.txt", "cmd/greeter.go", files)
	if code, o, errb := runCmd(t, "", "affected", "--feature", id); code != 0 || !strings.Contains(o, "nxy:reviewer-quick") {
		t.Fatalf("affected: code %d, out %q, err %q", code, o, errb)
	}
	agentPre(t, repo, "nxy:reviewer-quick", "feature "+id+" review base")
	if code, o, errb := runCmd(t, baseClean, "review", "save", "--feature", id, "--lens", "base"); code != 0 {
		t.Fatalf("review save: code %d, out %q, err %q", code, o, errb)
	}
	return repo, id, agentPost(t, repo, "nxy:reviewer-quick", nil)
}

func TestReviewWithDocsAsksAndTheDocumenterFinishes(t *testing.T) {
	repo, id, out := docsReview(t, map[string]string{"README.md": "intro\nThe greeter prints a hello.\n"})
	if !strings.Contains(out, "Actualizar docs") || !strings.Contains(out, "Dejar") || !strings.Contains(out, "AskUserQuestion") || strings.Contains(out, "Listo") {
		t.Fatalf("no docs question: %s", out)
	}
	question := docs.QuestionText(id)
	out = answer(t, repo, question, "mejor no sé")
	if !strings.Contains(out, "pick one of the options") {
		t.Fatalf("free text accepted: %s", out)
	}
	out = answer(t, repo, question, "Actualizar docs")
	if !strings.Contains(out, "nxy:documenter") || !strings.Contains(out, "feature "+id+" docs") {
		t.Fatalf("no documenter dispatch: %s", out)
	}
	out = agentPre(t, repo, "nxy:documenter", "feature "+id+" docs")
	if !strings.Contains(out, "updatedInput") || !strings.Contains(out, "README.md:2: The greeter prints a hello.") || !strings.Contains(out, "review docs --feature "+id) {
		t.Fatalf("documenter prompt not injected: %s", out)
	}
	edit := func(name string) string {
		return hookCall(t, map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Edit", "cwd": repo,
			"agent_id": "d1", "agent_type": "nxy:documenter", "tool_input": map[string]any{"file_path": filepath.Join(repo, name)}})
	}
	if out := edit("README.md"); !strings.Contains(out, "allow") {
		t.Errorf("the listed doc was not allowed: %s", out)
	}
	if out := edit("cmd/greeter.go"); !strings.Contains(out, "deny") {
		t.Errorf("a code file was not refused: %s", out)
	}
	code, packet, errb := runCmd(t, "", "review", "docs", "--feature", id)
	if code != 0 || !strings.Contains(packet, "# nxy docs packet") || !strings.Contains(packet, "README.md:2:") {
		t.Fatalf("packet: code %d, out %q, err %q", code, packet, errb)
	}
	out = agentPost(t, repo, "nxy:documenter", nil)
	if !strings.Contains(out, "Listo") {
		t.Fatalf("no Listo after the documenter: %s", out)
	}
	if _, st, _ := runCmd(t, "", "status"); !strings.Contains(st, "no active feature") {
		t.Fatalf("feature still active: %q", st)
	}
}

func TestReviewWithDocsLeaveCloses(t *testing.T) {
	repo, id, _ := docsReview(t, map[string]string{"docs/guide.md": "greeter is described here\n"})
	out := answer(t, repo, docs.QuestionText(id), "Dejar")
	if !strings.Contains(out, "Listo") || strings.Contains(out, "nxy:documenter") {
		t.Fatalf("Dejar did not close: %s", out)
	}
}

func TestReviewWithoutDocsClosesWithoutAsking(t *testing.T) {
	_, _, out := docsReview(t, map[string]string{"README.md": "nothing about it\n", "src/notes.md": "greeter (not a user doc)\n"})
	if !strings.Contains(out, "Listo") || strings.Contains(out, "Actualizar docs") {
		t.Fatalf("a repo without docs naming the change was asked: %s", out)
	}
}

func TestReviewDocsWithoutDocsSaysSo(t *testing.T) {
	_, id := affectedFeature(t, "test -f done.txt")
	code, out, errb := runCmd(t, "", "review", "docs", "--feature", id)
	if code != 0 || !strings.Contains(out, "no docs mention what this change touched") {
		t.Fatalf("code %d, out %q, err %q", code, out, errb)
	}
	if code, _, errb := runCmd(t, "", "review", "docs", "--feature", "nope"); code != 1 || errb == "" {
		t.Fatalf("unknown feature: code %d, err %q", code, errb)
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
