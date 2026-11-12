package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/memory"
	"github.com/mananos/nxy.dev/internal/platform"
	"github.com/mananos/nxy.dev/internal/store/sqlite"
	"github.com/mananos/nxy.dev/internal/tools"
)

// vgGit runs git in dir with a fixed identity and dates, so commits are the same everywhere.
func vgGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Dev", "GIT_AUTHOR_EMAIL=dev@example.com",
		"GIT_COMMITTER_NAME=Dev", "GIT_COMMITTER_EMAIL=dev@example.com",
		"GIT_AUTHOR_DATE=2026-11-01T10:00:00Z", "GIT_COMMITTER_DATE=2026-11-01T10:00:00Z",
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func vgWrite(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// vgNumbered is a file of n lines "linea 1" ... "linea n".
func vgNumbered(n int, change map[int]string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		if s, ok := change[i]; ok {
			b.WriteString(s)
		} else {
			b.WriteString("linea " + string(rune('A'+i-1)))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestViewGoldenPlanFileEngineer(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	root := t.TempDir()
	home := filepath.Join(root, "userdir")
	nxyHome := filepath.Join(root, "nxyhome")
	api := filepath.Join(root, "api")
	web := filepath.Join(root, "web")
	for _, d := range []string{home, nxyHome, api, web} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("NXY_HOME", nxyHome)
	t.Setenv("CLAUDE_PROJECT_DIR", api)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")

	loc, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Fatal(err)
	}
	now := vgAt(t, "2026-11-12T12:00:00Z")

	// two repos: a base commit, then edits, a new untracked file and a deleted one
	for _, d := range []string{api, web} {
		vgGit(t, d, "init", "-q")
		vgGit(t, d, "config", "core.autocrlf", "false")
		vgGit(t, d, "config", "user.name", "Dev")
		vgGit(t, d, "config", "user.email", "dev@example.com")
	}
	vgWrite(t, api, "src/billing/Nota.java", vgNumbered(20, nil))
	vgWrite(t, api, "src/billing/Viejo.java", vgNumbered(5, nil))
	vgWrite(t, web, "src/nota.ts", vgNumbered(8, nil))
	for _, d := range []string{api, web} {
		vgGit(t, d, "add", "-A")
		vgGit(t, d, "commit", "-q", "-m", "base")
	}
	baseAPI, baseWeb := vgGit(t, api, "rev-parse", "HEAD"), vgGit(t, web, "rev-parse", "HEAD")
	vgWrite(t, api, "src/billing/Nota.java", vgNumbered(20, map[int]string{12: "linea nueva 12", 20: "linea nueva 20"}))
	vgWrite(t, api, "src/billing/Credito.java", "class Credito {\n}\n")
	if err := os.Remove(filepath.Join(api, "src", "billing", "Viejo.java")); err != nil {
		t.Fatal(err)
	}
	vgWrite(t, web, "src/nota.ts", vgNumbered(8, map[int]string{5: "linea nueva 5"}))

	// the machine-dependent tools are stubbed
	oldT := viewTools
	t.Cleanup(func() { viewTools = oldT })
	viewTools = viewToolset{
		Indexed: func(string) bool { return true },
		Affected: func(_ context.Context, _ string, files []string) ([]string, bool) {
			for _, f := range files {
				if strings.HasSuffix(f, "Nota.java") {
					return []string{"src/test/NotaTest.java", "src/test/NotaServiceTest.java"}, true
				}
			}
			return nil, true
		},
		RgFound: func() bool { return true },
		Grep: func(_ context.Context, _, word string) []tools.RgRow {
			if word != "Nota" {
				return nil
			}
			return []tools.RgRow{
				{Path: "src/billing/Factura.java", Line: 30, Text: "Nota n = nueva();"},
				{Path: "src/billing/Nota.java", Line: 1, Text: "class Nota {"},
				{Path: "src/web/Listado.java", Line: 7, Text: "import Nota;"},
			}
		},
		FindD2: func() (string, string, bool) { return "d2", "0.7.1", true },
		RenderD2: func(_ context.Context, _, src string) ([]string, error) {
			return []string{"[" + strings.SplitN(src, "\n", 2)[0] + "]"}, nil
		},
	}

	ctx := context.Background()
	dbPath, err := platform.DBPath()
	if err != nil {
		t.Fatal(err)
	}
	clk := vgAt(t, "2026-10-01T10:00:00Z")
	st, err := sqlite.Open(ctx, dbPath, func() time.Time { return clk })
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	const apiKey, webKey = "acme/api", "acme/web"
	create := func(f domain.Feature, created, updated string) string {
		t.Helper()
		clk = vgAt(t, created)
		id, err := st.CreateFeature(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		clk = vgAt(t, updated)
		if err := st.Transition(ctx, id, func(g domain.Feature) (domain.Feature, error) { return g, nil }); err != nil {
			t.Fatal(err)
		}
		return id
	}
	m := func(s string) int64 { return vgMs(t, s) }
	change := func(b domain.Batch, ats ...string) domain.Batch {
		b.Change = nil
		for _, at := range ats {
			b.Change = append(b.Change, domain.Change{At: at, What: b.Title})
		}
		return b
	}

	// P: running in both repos, batch 2 failed
	fp := domain.Feature{
		Repo: apiKey, ProjectDir: api, Home: "ck-p", Holds: []string{"ck-p"},
		Request: "agregar notas de crédito a facturación", PlanRev: 1,
		Plan: vgPlan("Notas de crédito en facturación",
			change(vgBatch(1, "Corregir la nota", "api", "go test ./src/ -run TestNota"), "src/billing/Nota.java:12"),
			change(vgBatch(2, "Crédito y limpieza", "api", "go test ./src/ -run TestCredito", 1), "src/billing/Credito.java (new)", "src/billing/Viejo.java:3"),
			change(vgBatch(3, "Pantalla de notas", "web", "npm test -- nota", 1), "src/nota.ts:5")),
		Repos: []domain.FeatureRepo{
			{Name: "api", Key: apiKey, Root: api, Checkout: api, Branch: "feat/notas", Base: baseAPI},
			{Name: "web", Key: webKey, Root: web, Checkout: web, Branch: "feat/notas", Base: baseWeb},
		},
		State: domain.State{Phase: domain.PhaseBatchFailed, Batch: 2, Batches: 3, Attempt: 1, Steps: []domain.Step{
			{N: 1, Repo: "api", Status: domain.StepGreen, Launched: true, StartedAt: m("2026-11-11T10:00:00Z"), EndedAt: m("2026-11-11T10:20:00Z")},
			{N: 2, Repo: "api", After: []int{1}, Status: domain.StepFailed, Attempt: 1, Launched: true, StartedAt: m("2026-11-11T10:30:00Z"), EndedAt: m("2026-11-11T10:50:00Z")},
			{N: 3, Repo: "web", After: []int{1}},
		}},
		Next: domain.Action{Kind: domain.AskDecision, Batch: 2},
	}
	fp.SetProof(domain.Proof{Batch: 2, Failure: "--- FAIL: TestNota (0.01s)\n    src/billing/Nota.java:12: se esperaba 100.00 y dio 99.99\nsrc/test/NotaTest.java:30: falló\nFAIL"})
	idP := create(fp, "2026-11-11T09:00:00Z", "2026-11-11T11:00:00Z")

	// Q: waiting for approval in one repo, no base
	fq := domain.Feature{
		Repo: apiKey, ProjectDir: api, Home: "ck-q", Holds: []string{"ck-q"},
		Request: "revisar el formato de las notas", PlanRev: 1,
		Plan:  vgPlan("Revisar el formato de las notas", change(vgBatch(1, "Formato", "", "go test ./src/ -run TestFormato"), "src/billing/Nota.java:4")),
		Repos: []domain.FeatureRepo{{Name: "api", Key: apiKey, Root: api, Checkout: api, Branch: "feat/formato"}},
		State: domain.State{Phase: domain.PhaseApproval, Batches: 1},
		Next:  domain.Action{Kind: domain.AskApproval},
	}
	idQ := create(fq, "2026-11-11T12:00:00Z", "2026-11-11T12:30:00Z")

	// E: designing, an agenda with a decided, a current and a delegated fork
	fe := domain.Feature{
		Repo: apiKey, ProjectDir: api, Home: "ck-e", Holds: []string{"ck-e"},
		Request: "cobrar por suscripción", Mode: domain.ModeEngineer,
		Repos: []domain.FeatureRepo{{Name: "api", Key: apiKey, Root: api, Checkout: api}},
		State: domain.State{Phase: domain.PhaseDesigning},
		Design: &domain.Design{
			Current: "B2",
			Today:   "cobro: Cobro\nCobro -> Pasarela",
			Agenda: []domain.Fork{
				{ID: "B1", Title: "Dónde vive el cobro", Status: domain.ForkDecided},
				{ID: "B2", Title: "Cómo se reintenta", Status: domain.ForkOpen, Question: "¿Cómo se reintenta un cobro fallido?", Opinion: "Reintento con cola: es lo que menos toca.",
					Options: []domain.ForkOption{
						{Key: "A", Label: "Cola de reintentos", Summary: "Un worker reintenta con espera creciente.", Pros: []string{"No bloquea"}, Cons: []string{"Una pieza más"}, Touches: "src/billing/", D2: "cola: Cola\nCola -> Cobro"},
						{Key: "B", Label: "Reintento en línea", Summary: "El mismo pedido reintenta tres veces.", Pros: []string{"Simple"}},
					}},
				{ID: "B3", Title: "Formato de los recibos", Status: domain.ForkDelegated},
				{ID: "B4", Title: "Moneda", Status: domain.ForkOpen},
			},
			Decisions: []domain.Decision{{ID: "D1", Scope: domain.ScopeRepo, Text: "El cobro vive en el servicio de facturación", Fork: "B1"}},
		},
	}
	idE := create(fe, "2026-11-12T09:00:00Z", "2026-11-12T10:00:00Z")

	// memory anchored to the file
	clk = vgAt(t, "2026-10-05T10:00:00Z")
	if _, err := st.AddRule(ctx, memory.Rule{Repo: apiKey, Text: "Usar BigDecimal para los importes", Question: "¿Con qué tipo se representan los importes?", Origin: memory.OriginPlan, Paths: []string{"src/billing/**"}, Deliveries: 4}); err != nil {
		t.Fatal(err)
	}
	clk = vgAt(t, "2026-10-06T10:00:00Z")
	if _, err := st.AddRule(ctx, memory.Rule{Repo: apiKey, Text: "Los endpoints devuelven 422 ante datos inválidos", Question: "¿Qué código devuelve un endpoint ante datos inválidos?", Origin: memory.OriginChat}); err != nil {
		t.Fatal(err)
	}
	clk = vgAt(t, "2026-10-10T10:00:00Z")
	if _, err := st.AddLearning(ctx, memory.Learning{Repo: apiKey, Text: "Los importes se guardan en centavos", Files: []string{"src/billing/Nota.java"}}); err != nil {
		t.Fatal(err)
	}
	clk = vgAt(t, "2026-11-05T21:05:00Z")
	if _, err := st.SaveClosedFeature(ctx, memory.ClosedFeature{
		Meta: memory.Meta{ID: "CF-NOTA"}, Title: "Primera versión de las notas", Repos: []string{apiKey},
		Files: []memory.FileRef{{Repo: apiKey, Path: "src/billing/Nota.java"}},
	}); err != nil {
		t.Fatal(err)
	}

	src := newViewSource(st, os.Getenv, api, func() time.Time { return now }, loc)
	defer src.Close()

	check := vgChecker(t, root, []string{idP, "FEAT-P", idQ, "FEAT-Q", idE, "FEAT-E", baseAPI, "BASE-API", baseWeb, "BASE-WEB"})

	for key, id := range map[string]string{"p": idP, "q": idQ} {
		p, ok, err := src.Plan(ctx, id)
		if err != nil || !ok {
			t.Fatalf("plan %s: ok=%v err=%v", key, ok, err)
		}
		check("plan", "plan."+key, p)
	}
	for key, path := range map[string]string{"edit": "src/billing/Nota.java", "new": "src/billing/Credito.java"} {
		f, err := src.File(ctx, idP, path)
		if err != nil {
			t.Fatalf("file %s: %v", key, err)
		}
		check("file", "file."+key, f)
	}
	e, ok, err := src.Engineer(ctx, idE)
	if err != nil || !ok {
		t.Fatalf("engineer: ok=%v err=%v", ok, err)
	}
	check("engineer", "engineer.e", e)
}
