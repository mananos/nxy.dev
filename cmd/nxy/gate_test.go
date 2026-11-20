package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/platform"
)

func gateRepo(t *testing.T) string {
	t.Helper()
	isolate(t)
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestGateStatusWithoutLedger(t *testing.T) {
	repo := gateRepo(t)
	code, out, _ := runCmd(t, "", "gate", "--cwd", repo)
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, want := range []string{
		"gate de escritura:  activo (",
		"gate de lectura:    activo (se frena un grep/glob/find amplio con más de 100k tokens",
		"edición única:      no",
		"escape de búsqueda: no",
		"decisiones:         todavía no pasó ninguna edición por el gate en este repo",
		"búsquedas:          0 frenadas, 0 dejadas pasar",
		"búsquedas amplias:  ninguna registrada (desde ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status misses %q:\n%s", want, out)
		}
	}
}

func TestGateStatusWithLedger(t *testing.T) {
	repo := gateRepo(t)
	ctx := 120000.0
	now := time.Now().UnixMilli()
	platform.RecordGate(repo, platform.GateRow{Ts: now, Tool: "Edit", Allow: false, Reason: "code", ContextTokens: &ctx, Threshold: 100000})
	platform.RecordGate(repo, platform.GateRow{Ts: now, Tool: "Edit", Allow: true, Reason: "text", ContextTokens: &ctx, Threshold: 100000})
	platform.RecordGate(repo, platform.GateRow{Ts: now, Tool: "Grep", Allow: false, Reason: "over-threshold", ContextTokens: &ctx, Threshold: 100000, Kind: "read"})
	_, out, _ := runCmd(t, "", "gate", "status", "--cwd", repo)
	for _, want := range []string{"decisiones:         1 de 2 ediciones bloqueadas (50%)", "contexto al editar: p10 120k", "búsquedas:          1 frenadas, 0 dejadas pasar"} {
		if !strings.Contains(out, want) {
			t.Errorf("status misses %q:\n%s", want, out)
		}
	}
}

func TestGateOnceArmsEscapes(t *testing.T) {
	repo := gateRepo(t)
	_, out, _ := runCmd(t, "", "gate", "once", "--cwd", repo)
	if !strings.Contains(out, "próxima edición de un archivo de código") || !strings.Contains(out, "edición única:      armado") || !strings.Contains(out, "escape de búsqueda: no") {
		t.Fatalf("once:\n%s", out)
	}
	_, out, _ = runCmd(t, "", "gate", "once", "search", "--cwd", repo)
	if !strings.Contains(out, "próxima búsqueda amplia del hilo principal") || !strings.Contains(out, "escape de búsqueda: armado") {
		t.Fatalf("once search:\n%s", out)
	}
}

func TestGateReadsOnOffWritesUserConfig(t *testing.T) {
	repo := gateRepo(t)
	userCfg := filepath.Join(os.Getenv("NXY_HOME"), "config.json")
	repoCfg := filepath.Join(repo, ".nxy", "config.json")
	_, out, _ := runCmd(t, "", "gate", "reads", "off", "--cwd", repo)
	if !strings.Contains(out, "gate reads: OFF") || !strings.Contains(out, "gate de lectura:    apagado") {
		t.Fatalf("reads off:\n%s", out)
	}
	b, _ := os.ReadFile(userCfg)
	if !strings.Contains(string(b), `"reads": false`) {
		t.Fatalf("user config:\n%s", b)
	}
	if _, err := os.Stat(repoCfg); err == nil {
		t.Fatal("repo config written without --repo")
	}
	_, out, _ = runCmd(t, "", "gate", "reads", "on", "--cwd", repo)
	if !strings.Contains(out, "gate reads: ON") || !strings.Contains(out, "gate de lectura:    activo") {
		t.Fatalf("reads on:\n%s", out)
	}
}

func TestGateReadsRepoFlagWritesRepoConfig(t *testing.T) {
	repo := gateRepo(t)
	userCfg := filepath.Join(os.Getenv("NXY_HOME"), "config.json")
	repoCfg := filepath.Join(repo, ".nxy", "config.json")
	_, out, _ := runCmd(t, "", "gate", "reads", "off", "--repo", "--cwd", repo)
	if !strings.Contains(out, "gate reads: OFF") || !strings.Contains(out, "gate de lectura:    apagado") {
		t.Fatalf("reads off --repo:\n%s", out)
	}
	b, _ := os.ReadFile(repoCfg)
	if !strings.Contains(string(b), `"reads": false`) {
		t.Fatalf("repo config:\n%s", b)
	}
	if _, err := os.Stat(userCfg); err == nil {
		t.Fatal("user config written with --repo")
	}
}

// nxy stats is the same without a gate ledger and gains the three gate lines when the repo has rows.
func TestStatsGateLines(t *testing.T) {
	texts := loadUsageTexts(t)
	var cases []usageCase
	readGoldenFile(t, "stats.json", &cases)
	for i := range cases {
		c := &cases[i]
		json := false
		for _, a := range c.Input.Args {
			json = json || strings.HasPrefix(a, "--json")
		}
		if c.Output.Code != 0 || json || len(c.Input.Steps) > 0 || c.Input.Session == "" {
			continue
		}
		w := materializeUsage(t, c, texts)
		u, done := newUsage(w.getenv, func() string { return w.proj }, func() time.Time { return w.now }, w.loc)
		t.Cleanup(done)
		before := u.Stats(w.expandArgs(c.Input.Args))
		if strings.Contains(before.Stdout, "decisiones:") {
			t.Fatalf("gate lines without a ledger:\n%s", before.Stdout)
		}
		ctx := 90000.0
		platform.RecordGate(w.proj, platform.GateRow{Ts: w.now.UnixMilli(), Tool: "Edit", Allow: false, Reason: "code", ContextTokens: &ctx, Threshold: 100000})
		after := u.Stats(w.expandArgs(c.Input.Args))
		if !strings.HasPrefix(after.Stdout, before.Stdout) ||
			!strings.Contains(after.Stdout, "decisiones:         1 de 1 ediciones bloqueadas (100%)") ||
			!strings.Contains(after.Stdout, "contexto al editar: p10 90k") ||
			!strings.Contains(after.Stdout, "búsquedas:          0 frenadas, 0 dejadas pasar") {
			t.Fatalf("stats with ledger (case %s):\n%s", c.Name, after.Stdout)
		}
		return
	}
	t.Skip("no suitable stats case")
}

func TestGateReadsRefusesBrokenConfig(t *testing.T) {
	for _, useRepo := range []bool{false, true} {
		repo := gateRepo(t)
		cfgPath := filepath.Join(os.Getenv("NXY_HOME"), "config.json")
		args := []string{"gate", "reads", "off", "--cwd", repo}
		if useRepo {
			cfgPath = filepath.Join(repo, ".nxy", "config.json")
			args = append(args, "--repo")
		}
		if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
			t.Fatal(err)
		}
		const broken = "{ not json"
		if err := os.WriteFile(cfgPath, []byte(broken), 0o644); err != nil {
			t.Fatal(err)
		}
		code, out, _ := runCmd(t, "", args...)
		if code != 1 || !strings.Contains(out, "roto") {
			t.Fatalf("repo=%v (%d):\n%s", useRepo, code, out)
		}
		if after, _ := os.ReadFile(cfgPath); string(after) != broken {
			t.Fatalf("repo=%v broken config modified:\n%s", useRepo, after)
		}
	}
}
