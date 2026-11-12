package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/internal/memory"
	"github.com/mananos/nxy.dev/internal/platform"
	"github.com/mananos/nxy.dev/internal/setup"
	"github.com/mananos/nxy.dev/internal/store/sqlite"
	"github.com/mananos/nxy.dev/internal/views"
)

// vgRoot is the folder every temp path becomes in the goldens.
const vgRoot = "/srv/dev"

func vgAt(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func vgMs(t *testing.T, s string) int64 { return vgAt(t, s).UnixMilli() }

func vgPlan(title string, batches ...domain.Batch) *domain.Plan {
	return &domain.Plan{
		Title:    title,
		Criteria: []domain.Criterion{{ID: "R1", Text: title, Test: domain.TestTDD}},
		Batches:  batches,
	}
}

func vgBatch(n int, title, repo, accept string, depends ...int) domain.Batch {
	return domain.Batch{
		N: n, Title: title, Covers: []string{"R1"}, Repo: repo, Depends: depends, Risk: domain.RiskLow,
		Change: []domain.Change{{At: "src/billing/File.java:10", What: title}},
		Test:   domain.BatchTest{Kind: domain.BatchTestNew, Name: "Test" + title, Accept: accept},
	}
}

// vgCopyTranscripts copies the fake transcripts into the projects folder of cwd and sets the mtimes.
func vgCopyTranscripts(t *testing.T, dst string, mtimes map[string]time.Time) {
	t.Helper()
	src := filepath.Join("..", "..", "testdata", "golden", "views", "transcripts")
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		to := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(to, b, 0o644); err != nil {
			return err
		}
		key := strings.TrimSuffix(filepath.ToSlash(rel), ".meta.json")
		key = strings.TrimSuffix(key, ".jsonl")
		if m, ok := mtimes[key]; ok {
			return os.Chtimes(to, m, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func vgEncode(t *testing.T, v any) []byte {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// vgBad are the personal paths no golden may hold (the list of TestViewsNoPersonalPaths).
var vgBad = []string{"/home/", "/Users/", `C:\\Users`, `C:\Users`, "C:/Users"}

func TestViewGolden(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "userdir") // not "home": "/home/" is on the personal-path list
	nxyHome := filepath.Join(root, "nxyhome")
	cwd := filepath.Join(root, "app")
	web := filepath.Join(root, "web")
	pagos := filepath.Join(root, "pagos")
	pagosWT := filepath.Join(root, "pagos-wt")
	for _, d := range []string{home, nxyHome, filepath.Join(cwd, "src", "billing"), web, pagos, pagosWT} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(cwd, "src", "billing", "Nota.java"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("NXY_HOME", nxyHome)
	t.Setenv("CLAUDE_PROJECT_DIR", cwd)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")

	loc, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Fatal(err)
	}
	now := vgAt(t, "2026-11-11T12:00:00Z")

	// the machine-dependent parts are stubbed
	oldS, oldB, oldW := viewSetupState, viewBinary, viewWorktrees
	t.Cleanup(func() { viewSetupState, viewBinary, viewWorktrees = oldS, oldB, oldW })
	viewSetupState = func(string, func(string) string) setup.State {
		return setup.State{
			Rtk: setup.ToolInfo{Found: true, Version: "0.39.0", Path: "/usr/local/bin/rtk", OnPath: true},
			Rg:  setup.ToolInfo{Found: true, Version: "14.1.0", Path: "/usr/bin/rg", OnPath: true},
		}
	}
	viewBinary = func() views.Binary {
		return views.Binary{Installed: "2.0.0-dev", Wanted: "2.0.0-dev", Path: "~/.nxy/bin/nxy", Download: "none"}
	}
	dirty := 2
	viewWorktrees = func(_ context.Context, dir string) (platform.WorktreeList, error) {
		if !platform.SamePath(dir, pagos) {
			return platform.WorktreeList{}, errors.New("no es un repo git")
		}
		return platform.WorktreeList{Main: pagos, RepoName: "pagos", Rows: []platform.WorktreeRow{
			{Path: pagos, Branch: "main", Main: true},
			{Path: pagosWT, Branch: "feat/pagos-v3", Dirty: &dirty, Ahead: 1},
			{Path: filepath.Join(root, "pagos-old"), Branch: "fix/viejo"},
		}}, nil
	}

	// config: a user layer and a repo layer
	if err := os.WriteFile(filepath.Join(nxyHome, "config.json"), []byte(`{"roles":{"reviewer":{"model":"opus"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cwd, ".nxy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".nxy", "config.json"), []byte(`{"roles":{"reviewer":{"effort":"medium"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// transcripts under the projects folder of cwd
	slug := transcript.ProjectSlug(cwd)
	vgCopyTranscripts(t, filepath.Join(home, ".claude", "projects", slug), map[string]time.Time{
		"sess-a":                        vgAt(t, "2026-11-11T10:56:00Z"),
		"sess-a/subagents/agent-a1impl": vgAt(t, "2026-11-10T16:20:00Z"),
		"sess-b":                        vgAt(t, "2026-11-11T11:59:00Z"),
		"sess-b/subagents/agent-b2live": vgAt(t, "2026-11-11T11:59:30Z"),
		"sess-b/subagents/agent-bscout": vgAt(t, "2026-11-11T09:09:00Z"),
	})

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

	appKey := platform.RepoKey(cwd)
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
	run := func(r domain.AgentRun) {
		t.Helper()
		if err := st.RecordAgentRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	m := func(s string) int64 { return vgMs(t, s) }

	// A: two repos, batch 3 red, batch 2 is a step saved before F5.2a (no start, no end)
	fa := domain.Feature{
		Repo: appKey, ProjectDir: cwd, Home: "ck-a-api", Holds: []string{"ck-a-api", "ck-a-web"},
		Request: "agregar notas de crédito a facturación", PlanRev: 1,
		Plan: vgPlan("Notas de crédito en facturación",
			vgBatch(1, "Modelo de nota de crédito", "api", "go test ./internal/billing/ -run TestNota"),
			vgBatch(2, "Pantalla de notas", "web", "npm test -- nota"),
			vgBatch(3, "Endpoint de notas", "api", "go test ./internal/billing/ -run TestEndpoint", 1),
			vgBatch(4, "Listado en la web", "web", "npm test -- listado", 3)),
		Repos: []domain.FeatureRepo{
			{Name: "api", Key: appKey, Root: cwd, Checkout: cwd, Branch: "feat/notas"},
			{Name: "web", Key: "acme/web", Root: web, Checkout: web + "-feat-notas", Branch: "feat/notas"},
		},
		State: domain.State{Phase: domain.PhaseBatchFailed, Batch: 3, Batches: 4, Attempt: 1, Steps: []domain.Step{
			{N: 1, Repo: "api", Status: domain.StepGreen, Launched: true, StartedAt: m("2026-11-10T16:00:00Z"), EndedAt: m("2026-11-10T16:20:00Z")},
			{N: 2, Repo: "web", Status: domain.StepGreen, Launched: true},
			{N: 3, Repo: "api", After: []int{1}, Status: domain.StepFailed, Attempt: 1, Launched: true, StartedAt: m("2026-11-11T10:00:00Z"), EndedAt: m("2026-11-11T10:40:00Z")},
			{N: 4, Repo: "web", After: []int{3}},
		}},
		Next:  domain.Action{Kind: domain.AskDecision, Batch: 3},
		Lanes: []domain.BatchLane{{Batch: 1, AgentID: "a1impl"}, {Batch: 3, AgentID: "a3impl"}},
	}
	fa.SetProof(domain.Proof{Batch: 3, Failure: "--- FAIL: TestEndpoint (0.01s)\n    endpoint_test.go:42: want 100.00, got 99.99\nFAIL\nFAIL\tacme/billing\t0.012s"})
	idA := create(fa, "2026-11-10T15:00:00Z", "2026-11-11T11:00:00Z")

	// B: batches 2 and 3 running in parallel, a live implementer for 2
	fb := domain.Feature{
		Repo: appKey, ProjectDir: cwd, Home: "ck-b", Holds: []string{"ck-b"},
		Request: "exportar facturas a CSV", PlanRev: 1,
		Plan: vgPlan("Exportar facturas a CSV",
			vgBatch(1, "Formato CSV", "", "go test ./internal/export/ -run TestFormat"),
			vgBatch(2, "Servicio de exportación", "", "go test ./internal/export/ -run TestService", 1),
			vgBatch(3, "Comando de exportación", "", "go test ./cmd/ -run TestExport", 1),
			vgBatch(4, "Documentación", "", "go test ./docs/", 2, 3)),
		State: domain.State{Phase: domain.PhaseRunningBatch, Batch: 2, Batches: 4, Steps: []domain.Step{
			{N: 1, Status: domain.StepGreen, Launched: true, StartedAt: m("2026-11-11T09:10:00Z"), EndedAt: m("2026-11-11T09:30:00Z")},
			{N: 2, After: []int{1}, Status: domain.StepRunning, Launched: true, StartedAt: m("2026-11-11T11:00:00Z")},
			{N: 3, After: []int{1}, Status: domain.StepRunning, Launched: true, StartedAt: m("2026-11-11T11:10:00Z")},
			{N: 4, After: []int{2, 3}},
		}},
		Next:  domain.Action{Kind: domain.AwaitBatches, Batch: 2, Batches: []int{2, 3}},
		Lanes: []domain.BatchLane{{Batch: 1, AgentID: "b1impl"}, {Batch: 2, AgentID: "b2live"}},
	}
	idB := create(fb, "2026-11-11T09:00:00Z", "2026-11-11T11:30:00Z")

	// C: done today
	fc := domain.Feature{
		Repo: appKey, ProjectDir: cwd, Home: "ck-c",
		Request: "corregir el redondeo de IVA", PlanRev: 1,
		Plan: vgPlan("Corregir el redondeo de IVA", vgBatch(1, "Redondeo por línea", "", "go test ./internal/billing/ -run TestIva")),
		State: domain.State{Phase: domain.PhaseDone, Batches: 1, Outcome: domain.OutcomeSuccess, Steps: []domain.Step{
			{N: 1, Status: domain.StepGreen, Launched: true, StartedAt: m("2026-11-10T20:30:00Z"), EndedAt: m("2026-11-10T21:00:00Z")},
		}},
		Next: domain.Action{Kind: domain.Close},
	}
	idC := create(fc, "2026-11-10T20:00:00Z", "2026-11-11T05:00:00Z")

	// D: paused at the approval question, with a worktree
	fd := domain.Feature{
		Repo: "acme/pagos", ProjectDir: pagos, Home: "ck-d", Paused: true,
		Request: "migrar pagos a la API v3", PlanRev: 2,
		Plan: vgPlan("Migrar pagos a la API v3",
			vgBatch(1, "Cliente de la API v3", "", "go test ./pay/ -run TestClient"),
			vgBatch(2, "Cambiar el servicio de cobro", "", "go test ./pay/ -run TestCharge", 1)),
		Repos: []domain.FeatureRepo{{Name: "pagos", Key: "acme/pagos", Root: pagos, Checkout: pagosWT, Branch: "feat/pagos-v3"}},
		State: domain.State{Phase: domain.PhaseApproval, Batches: 2},
		Next:  domain.Action{Kind: domain.AskApproval},
	}
	idD := create(fd, "2026-11-09T14:00:00Z", "2026-11-10T14:00:00Z")

	// agent runs and the sessions linked to the features
	run(domain.AgentRun{FeatureID: idA, AgentID: "a-plan", Role: "planner", Model: "claude-sonnet-4-6", Effort: "high", Calls: 4, TokensIn: 3000, TokensOut: 1200, CacheRead: 9000, CacheWrite: 2000, CostUSD: 0.05, StartedAt: m("2026-11-10T15:02:00Z"), EndedAt: m("2026-11-10T15:09:00Z")})
	run(domain.AgentRun{FeatureID: idA, AgentID: "a-scout", Role: "scout", Model: "claude-haiku-5-5", Effort: "low", Calls: 3, TokensIn: 1000, TokensOut: 300, CacheRead: 2000, CostUSD: 0.004, StartedAt: m("2026-11-10T15:03:00Z"), EndedAt: m("2026-11-10T15:06:00Z")})
	run(domain.AgentRun{FeatureID: idA, AgentID: "a1impl", Batch: 1, Role: "implementer", Model: "claude-sonnet-4-6", Effort: "medium", Calls: 2, TokensIn: 900, TokensOut: 1100, CacheRead: 9000, CostUSD: 0.12, StartedAt: m("2026-11-10T16:00:00Z"), EndedAt: m("2026-11-10T16:20:00Z")})
	run(domain.AgentRun{FeatureID: idA, AgentID: "a3impl", Batch: 3, Role: "implementer", Model: "claude-sonnet-4-6", Effort: "medium", Calls: 5, TokensIn: 2000, TokensOut: 2600, CacheRead: 18000, CostUSD: 0.2, StartedAt: m("2026-11-11T10:00:00Z"), EndedAt: m("2026-11-11T10:40:00Z")})
	run(domain.AgentRun{FeatureID: idA, AgentID: "a3fix", Batch: 3, Role: "fixer", Model: "claude-sonnet-4-6", Effort: "medium", Calls: 2, TokensIn: 700, TokensOut: 500, CacheRead: 6000, CostUSD: 0.05, StartedAt: m("2026-11-11T10:45:00Z"), EndedAt: m("2026-11-11T10:55:00Z")})
	run(domain.AgentRun{FeatureID: idB, AgentID: "b-plan", Role: "planner", Model: "claude-sonnet-4-6", Effort: "high", Calls: 3, TokensIn: 2500, TokensOut: 900, CacheRead: 7000, CostUSD: 0.04, StartedAt: m("2026-11-11T09:02:00Z"), EndedAt: m("2026-11-11T09:08:00Z")})
	run(domain.AgentRun{FeatureID: idB, AgentID: "b1impl", Batch: 1, Role: "implementer", Model: "claude-sonnet-4-6", Effort: "medium", Calls: 4, TokensIn: 1200, TokensOut: 1500, CacheRead: 11000, CostUSD: 0.08, StartedAt: m("2026-11-11T09:10:00Z"), EndedAt: m("2026-11-11T09:30:00Z")})
	run(domain.AgentRun{FeatureID: idC, AgentID: "c1impl", Batch: 1, Role: "implementer", Model: "claude-sonnet-4-6", Effort: "medium", Calls: 3, TokensIn: 800, TokensOut: 900, CacheRead: 5000, CostUSD: 0.03, StartedAt: m("2026-11-10T20:30:00Z"), EndedAt: m("2026-11-10T21:00:00Z")})
	clk = vgAt(t, "2026-11-10T15:00:30Z")
	if err := st.LinkFeature(ctx, "sess-a", idA); err != nil {
		t.Fatal(err)
	}
	clk = vgAt(t, "2026-11-11T09:00:30Z")
	if err := st.LinkFeature(ctx, "sess-b", idB); err != nil {
		t.Fatal(err)
	}

	// memory of the repo of cwd, and the global one
	clk = vgAt(t, "2026-10-05T10:00:00Z")
	if _, err := st.AddRule(ctx, memory.Rule{Repo: appKey, Text: "Usar BigDecimal para los importes", Question: "¿Con qué tipo se representan los importes?", Origin: memory.OriginPlan, FeatureID: idC, Paths: []string{"src/billing/**"}, Deliveries: 4, Citations: 2, LastCitedAt: m("2026-11-09T10:00:00Z")}); err != nil {
		t.Fatal(err)
	}
	clk = vgAt(t, "2026-10-06T10:00:00Z")
	if _, err := st.AddRule(ctx, memory.Rule{Repo: appKey, Text: "Los endpoints devuelven 422 ante datos inválidos", Question: "¿Qué código devuelve un endpoint ante datos inválidos?", Origin: memory.OriginChat, Deliveries: 10, Citations: 0}); err != nil {
		t.Fatal(err)
	}
	clk = vgAt(t, "2026-10-07T10:00:00Z")
	if _, err := st.AddRule(ctx, memory.Rule{Repo: appKey, Text: "Cada migración trae su rollback", Question: "¿Qué lleva una migración?", Origin: memory.OriginReview, FeatureID: idA, Deliveries: 3}); err != nil {
		t.Fatal(err)
	}
	clk = vgAt(t, "2026-10-08T10:00:00Z")
	if _, err := st.AddGlobalRule(ctx, memory.Rule{Text: "Sin comentarios que repitan el código", Question: "¿Cómo se comenta el código?", Origin: memory.OriginManual, Roles: []string{"implementer", "reviewer"}, Deliveries: 6, Citations: 1, LastCitedAt: m("2026-11-10T12:00:00Z")}); err != nil {
		t.Fatal(err)
	}
	clk = vgAt(t, "2026-10-09T10:00:00Z")
	if _, err := st.AddLearning(ctx, memory.Learning{Repo: appKey, Text: "El redondeo de IVA va por línea, no por total", Files: []string{"src/billing/Iva.java"}, FeatureID: idC}); err != nil {
		t.Fatal(err)
	}
	clk = vgAt(t, "2026-10-10T10:00:00Z")
	for range 2 {
		if _, err := st.AddLearning(ctx, memory.Learning{Repo: appKey, Text: "Los importes se guardan en centavos", Files: []string{"src/billing/Nota.java"}, FeatureID: idA}); err != nil {
			t.Fatal(err)
		}
	}
	// a global learning of the 1.x
	gtext := "Preferir composición a herencia en los servicios"
	gl := memory.Learning{
		Meta:  memory.Meta{ID: "01LEGACYGLOBALLEARNING0000", CreatedAt: m("2026-10-11T10:00:00Z"), UpdatedAt: m("2026-10-11T10:00:00Z")},
		Short: "A9", Repo: memory.GlobalRepo, Text: gtext, Hash: memory.LearningHash(gtext, nil), Seen: 1, LastSeenAt: m("2026-10-11T10:00:00Z"),
	}
	if err := st.ApplyImport(ctx, memory.Diff{Learnings: []memory.Change[memory.Learning]{{Kind: memory.New, Row: gl}}}); err != nil {
		t.Fatal(err)
	}
	clk = vgAt(t, "2026-11-10T21:05:00Z")
	if _, err := st.SaveClosedFeature(ctx, memory.ClosedFeature{
		Meta: memory.Meta{ID: "CF-IVA"}, Title: "Corregir el redondeo de IVA", Repos: []string{appKey},
		Files: []memory.FileRef{{Repo: appKey, Path: "src/billing/Iva.java"}, {Repo: "acme/web", Path: "src/Total.tsx"}},
	}); err != nil {
		t.Fatal(err)
	}

	// build the views through the source, with a fixed clock
	src := newViewSource(st, os.Getenv, cwd, func() time.Time { return now }, loc)
	defer src.Close()

	var pairs []string
	pairs = append(pairs, idA, "FEAT-A", idB, "FEAT-B", idC, "FEAT-C", idD, "FEAT-D")
	pairs = append(pairs, transcript.ProjectSlug(cwd), transcript.ProjectSlug(vgRoot+"/app"))
	check := vgChecker(t, root, pairs)

	cockpit, err := src.Cockpit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	check("cockpit", "cockpit", cockpit)

	for key, id := range map[string]string{"a": idA, "b": idB} {
		ag, err := src.Agents(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		check("agents", "agents."+key, ag)
	}
	for key, id := range map[string]string{"a": idA, "d": idD} {
		d, ok, err := src.Decision(ctx, id)
		if err != nil || !ok {
			t.Fatalf("decision %s: ok=%v err=%v", key, ok, err)
		}
		check("decision", "decision."+key, d)
	}
	if _, ok, err := src.Decision(ctx, idB); err != nil || ok {
		t.Fatalf("a running feature asks nothing: ok=%v err=%v", ok, err)
	}

	for _, scope := range []string{views.ScopeRepo, views.ScopeGlobal} {
		mem, err := src.Memory(ctx, scope)
		if err != nil {
			t.Fatal(err)
		}
		check("memory", "memory."+scope, mem)
	}
	stats, err := src.Stats(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	check("stats", "stats", stats)
	trend, err := src.Trend(ctx)
	if err != nil {
		t.Fatal(err)
	}
	check("trend", "trend", trend)
	cfg, err := src.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	check("config", "config", cfg)
}

// vgChecker compares a view with its golden. pairs are the replacements (old, new) for ids and the
// like; every spelling of the temp root becomes vgRoot after them.
func vgChecker(t *testing.T, root string, pairs []string) func(viewID, name string, v any) {
	rootVariants := []string{root, filepath.ToSlash(root), strings.ReplaceAll(root, `\`, `\\`)}
	if real, err := filepath.EvalSymlinks(root); err == nil && real != root {
		rootVariants = append(rootVariants, real, filepath.ToSlash(real), strings.ReplaceAll(real, `\`, `\\`))
	}
	all := append([]string{}, pairs...)
	for _, v := range rootVariants {
		all = append(all, v, vgRoot)
	}
	norm := strings.NewReplacer(all...)

	return func(viewID, name string, v any) {
		t.Helper()
		got := []byte(norm.Replace(string(vgEncode(t, v))))
		if v != nil {
			if err := views.Decode(got, views.New(viewID)); err != nil {
				t.Errorf("%s: strict decode: %v", name, err)
			}
		}
		for _, bad := range vgBad {
			if strings.Contains(string(got), bad) {
				t.Errorf("%s holds a personal path (%q)", name, bad)
			}
		}
		path := filepath.Join("..", "..", "testdata", "golden", "views", name+".json")
		if os.Getenv("NXY_WRITE_GOLDEN") == "1" {
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatal(err)
			}
			return
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v (NXY_WRITE_GOLDEN=1 writes it)", name, err)
			return
		}
		w := strings.ReplaceAll(string(want), "\r\n", "\n")
		if w != string(got) {
			t.Errorf("%s differs from %s (NXY_WRITE_GOLDEN=1 rewrites it):\n%s", name, path, vgFirstDiff(w, string(got)))
		}
		for _, bad := range vgBad {
			if strings.Contains(w, bad) {
				t.Errorf("golden %s holds a personal path (%q)", path, bad)
			}
		}
	}
}

// vgFirstDiff is the first line where two texts differ, with the line number.
func vgFirstDiff(want, got string) string {
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			return "line " + strconv.Itoa(i+1) + ":\n  want " + w + "\n  got  " + g
		}
	}
	return "(same lines)"
}
