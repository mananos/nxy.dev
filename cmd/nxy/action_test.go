package main

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/platform"
	"github.com/mananos/nxy.dev/v2/internal/store/sqlite"
)

// seedAsk makes a feature with one held question and returns their ids.
func seedAsk(t *testing.T) (featureID, askID string) {
	t.Helper()
	repo := flowRepo(t)
	path, err := platform.DBPath()
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	s, err := sqlite.Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	featureID, err = s.CreateFeature(ctx, domain.Feature{
		Repo: platform.RepoKey(repo), ProjectDir: repo,
		State: domain.State{Phase: domain.PhaseRunningBatch, Batch: 1, Batches: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := s.CreatePendingAsk(ctx, domain.PendingAsk{FeatureID: featureID, SessionID: "s1", ToolUseID: "tu1",
		Questions: []domain.AskedQuestion{{Question: "¿Seguimos?", Options: []string{"Sí", "No"}}}})
	if err != nil {
		t.Fatal(err)
	}
	return featureID, row.ID
}

func readAsk(t *testing.T, id string) domain.PendingAsk {
	t.Helper()
	path, _ := platform.DBPath()
	s, err := sqlite.Open(t.Context(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	row, err := s.PendingAsk(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

// liveTUI starts a heartbeat in the test's run dir and sets its nonce, as the TUI's subprocess has it.
func liveTUI(t *testing.T) string {
	t.Helper()
	run, err := platform.RunDir()
	if err != nil {
		t.Fatal(err)
	}
	nonce, stop := platform.HeartbeatNonce(run)
	t.Cleanup(stop)
	t.Setenv(platform.NonceEnv, nonce)
	return nonce
}

func TestActionDecideNeedsTheTUINonce(t *testing.T) {
	_, id := seedAsk(t)
	t.Setenv(platform.NonceEnv, "")
	if code, _, errb := runCmd(t, "", "action", "decide", "--ask", id, "--answer", "Sí"); code != 1 || !strings.Contains(errb, "sólo lo corre la TUI") {
		t.Fatalf("no nonce: code %d, err %q", code, errb)
	}
	liveTUI(t)
	t.Setenv(platform.NonceEnv, "inventado")
	if code, _, errb := runCmd(t, "", "action", "decide", "--ask", id, "--answer", "Sí"); code != 1 || !strings.Contains(errb, "sólo lo corre la TUI") {
		t.Fatalf("bad nonce: code %d, err %q", code, errb)
	}
	if row := readAsk(t, id); row.Answers != nil {
		t.Errorf("answered without proof: %+v", row)
	}
}

func TestSelfEnvCarriesTheNonce(t *testing.T) {
	env := selfEnv([]string{"A=1", platform.NonceEnv + "=viejo"}, "n0nce")
	got := 0
	for _, e := range env {
		if e == platform.NonceEnv+"=n0nce" {
			got++
		} else if strings.HasPrefix(e, platform.NonceEnv+"=") {
			t.Errorf("stale nonce kept: %q", e)
		}
	}
	if got != 1 || len(env) != 2 {
		t.Errorf("env = %v", env)
	}
	if env := selfEnv([]string{"A=1"}, ""); len(env) != 1 {
		t.Errorf("empty nonce added: %v", env)
	}
}

func TestActionDecide(t *testing.T) {
	_, id := seedAsk(t)
	liveTUI(t)
	if code, _, errb := runCmd(t, "", "action", "decide", "--ask", id, "--answer", "Quizás"); code != 1 || !strings.Contains(errb, "no es una opción") {
		t.Fatalf("bad label: code %d, err %q", code, errb)
	}
	if code, _, errb := runCmd(t, "", "action", "decide", "--ask", id); code != 1 || !strings.Contains(errb, "uso") {
		t.Fatalf("no answer: code %d, err %q", code, errb)
	}
	code, out, errb := runCmd(t, "", "action", "decide", "--ask", id, "--answer", "Sí")
	if code != 0 {
		t.Fatalf("decide: code %d, out %q, err %q", code, out, errb)
	}
	if row := readAsk(t, id); len(row.Answers) != 1 || row.Answers[0] != "Sí" || row.AnsweredBy != "tui" {
		t.Errorf("row = %+v", row)
	}
	if code, _, errb := runCmd(t, "", "action", "decide", "--ask", id, "--answer", "No"); code != 1 || !strings.Contains(errb, "ya contestada") {
		t.Errorf("second decide: code %d, err %q", code, errb)
	}
	if code, _, errb := runCmd(t, "", "action", "decide", "--ask", "nope", "--release"); code != 1 || !strings.Contains(errb, "ya no está esperando") {
		t.Errorf("gone: code %d, err %q", code, errb)
	}
}

func TestActionDecideRelease(t *testing.T) {
	_, id := seedAsk(t)
	liveTUI(t)
	if code, _, errb := runCmd(t, "", "action", "decide", "--ask", id, "--release"); code != 0 {
		t.Fatalf("release: code %d, err %q", code, errb)
	}
	if row := readAsk(t, id); row.Answers == nil || len(row.Answers) != 0 || row.AnsweredBy != "terminal" {
		t.Errorf("row = %+v", row)
	}
}

func TestActionPick(t *testing.T) {
	repo := flowRepo(t)
	path, err := platform.DBPath()
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	s, err := sqlite.Open(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	fid, err := s.CreateFeature(ctx, domain.Feature{
		Repo: platform.RepoKey(repo), ProjectDir: repo, Mode: domain.ModeEngineer,
		State: domain.State{Phase: domain.PhaseDesigning},
		Design: &domain.Design{Current: "B1", Agenda: []domain.Fork{{ID: "B1", Title: "t", Status: domain.ForkOpen,
			Options: []domain.ForkOption{{Key: "A", Label: "En memoria"}, {Key: "B", Label: "En disco"}}}}},
	})
	s.Close()
	if err != nil {
		t.Fatal(err)
	}
	pick := func(label string) (int, string, string) {
		return runCmd(t, "", "action", "pick", "--feature", fid, "--fork", "B1", "--option", label)
	}
	read := func() *domain.ForkPick {
		s, err := sqlite.Open(ctx, path, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		f, err := s.Feature(ctx, fid)
		if err != nil {
			t.Fatal(err)
		}
		return f.Design.Pick
	}
	t.Setenv(platform.NonceEnv, "")
	if code, _, errb := pick("En disco"); code != 1 || !strings.Contains(errb, "sólo lo corre la TUI") {
		t.Fatalf("no nonce: code %d, err %q", code, errb)
	}
	if read() != nil {
		t.Fatal("picked without proof")
	}
	liveTUI(t)
	if code, _, errb := pick("Quizás"); code != 1 || errb == "" {
		t.Fatalf("bad label: code %d, err %q", code, errb)
	}
	if code, out, errb := pick("En disco"); code != 0 || !strings.Contains(out, "guardada") {
		t.Fatalf("pick: code %d, out %q, err %q", code, out, errb)
	}
	if p := read(); p == nil || p.Label != "En disco" {
		t.Fatalf("pick = %+v", p)
	}
	if code, _, errb := pick("En memoria"); code != 0 {
		t.Fatalf("change: code %d, err %q", code, errb)
	}
	if p := read(); p == nil || p.Label != "En memoria" {
		t.Fatalf("pick = %+v", p)
	}
}

func TestFeatureStopByIDReleasesTheAsk(t *testing.T) {
	fid, id := seedAsk(t)
	code, out, errb := runCmd(t, "", "feature", "stop", "--feature", fid)
	if code != 0 || !strings.Contains(out, "paré la feature") {
		t.Fatalf("stop: code %d, out %q, err %q", code, out, errb)
	}
	if row := readAsk(t, id); row.Answers == nil || row.AnsweredBy != "stopped" {
		t.Errorf("row = %+v", row)
	}
}
