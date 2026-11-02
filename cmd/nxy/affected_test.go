package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// affectedFeature takes a one-batch feature (suite is the plan's Suite, "" for none) through
// planning, approval and the red/green of its batch; the feature ends at the affected tests.
func affectedFeature(t *testing.T, suite string) (repo, id string) {
	t.Helper()
	return affectedFeatureWith(t, suite, "cmd/main.go", nil)
}

// affectedFeatureWith is affectedFeature where the plan changes the file `changed` and the repo
// holds `files` (path -> text) before the feature starts.
func affectedFeatureWith(t *testing.T, suite, changed string, files map[string]string) (repo, id string) {
	t.Helper()
	needSh(t)
	repo = flowRepo(t)
	t.Setenv("NXY_CODEGRAPH_PATH", filepath.Join(repo, "no-such-codegraph"))
	for p, body := range files {
		full := filepath.Join(repo, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hookCall(t, map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": repo, "prompt": "/nxy:feature add a flag"})
	id = featureID(t)
	agentPre(t, repo, "nxy:planner", "feature "+id)
	p := strings.Replace(batchPlan("test -f done.txt"), "cmd/main.go:10", changed+":10", 1)
	if suite != "" {
		p = strings.Replace(p, `"risk":"low"}]}`, `"risk":"low"}],"suite":`+strconvQuote(suite)+`}`, 1)
	}
	if code, out, errb := runCmd(t, p, "plan", "save", "--feature", id); code != 0 {
		t.Fatalf("plan save: code %d, out %q, err %q", code, out, errb)
	}
	agentPost(t, repo, "nxy:planner", nil)
	answer(t, repo, "Approve nxy plan "+id+" r1?", "Approve")
	agentPre(t, repo, "nxy:implementer", "feature "+id+" batch 1")
	if code, out, errb := runCmd(t, "", "batch", "red", "--feature", id); code != 0 {
		t.Fatalf("red: code %d, out %q, err %q", code, out, errb)
	}
	if err := os.WriteFile(filepath.Join(repo, "done.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errb := runCmd(t, "", "batch", "green", "--feature", id); code != 0 {
		t.Fatalf("green: code %d, out %q, err %q", code, out, errb)
	}
	out := agentPost(t, repo, "nxy:implementer", nil)
	if !strings.Contains(out, "all batches are green") || !strings.Contains(out, "affected --feature "+id) {
		t.Fatalf("last green does not name affected: %s", out)
	}
	return repo, id
}

func TestAffectedGreenWithSuite(t *testing.T) {
	_, id := affectedFeature(t, "test -f done.txt")
	code, out, errb := runCmd(t, "", "affected", "--feature", id)
	if code != 0 || !strings.Contains(out, "verde") || !strings.Contains(out, "nxy:reviewer-quick") {
		t.Fatalf("affected: code %d, out %q, err %q", code, out, errb)
	}
}

func TestAffectedUnverifiedWithoutSuite(t *testing.T) {
	repo, id := affectedFeature(t, "")
	code, out, errb := runCmd(t, "", "affected", "--feature", id)
	if code != 0 || !strings.Contains(out, "sin verificar") || !strings.Contains(out, "Afectados sin verificar") {
		t.Fatalf("affected: code %d, out %q, err %q", code, out, errb)
	}
	out = answer(t, repo, "nxy feature "+id+": the affected tests could not be verified — what now?", "Seguir")
	if !strings.Contains(out, "nxy:reviewer-quick") {
		t.Fatalf("Seguir does not go to review: %s", out)
	}
}

func TestAffectedRedFixAndFailed(t *testing.T) {
	repo, id := affectedFeature(t, "exit 3")
	code, out, errb := runCmd(t, "", "affected", "--feature", id)
	if code != 1 || !strings.Contains(out, "rojo (exit 3)") || !strings.Contains(out, "affected fix") {
		t.Fatalf("red: code %d, out %q, err %q", code, out, errb)
	}
	if out := agentPre(t, repo, "nxy:implementer", "feature "+id+" affected fix"); !strings.Contains(out, "updatedInput") || !strings.Contains(out, "exit 3") {
		t.Fatalf("fix prompt without the failure: %s", out)
	}
	if out := agentPost(t, repo, "nxy:implementer", nil); !strings.Contains(out, "affected --feature "+id) {
		t.Fatalf("fix return does not name affected: %s", out)
	}
	code, out, errb = runCmd(t, "", "affected", "--feature", id)
	if code != 1 || !strings.Contains(out, "Afectados fallidos") {
		t.Fatalf("second red: code %d, out %q, err %q", code, out, errb)
	}
}
