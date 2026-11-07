package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/internal/platform"
	"github.com/mananos/nxy.dev/internal/report"
	"github.com/mananos/nxy.dev/internal/store/sqlite"
)

// legacyWorld is a repo the 1.x worked in: a memory.db under NXY_HOME with its memories, and the
// review and filter logs in the repo's runtime dir.
func legacyWorld(t *testing.T) (repo, memDB string) {
	t.Helper()
	isolate(t)
	home := os.Getenv("NXY_HOME")
	repo = filepath.Join(t.TempDir(), "repo")
	mustWrite := func(p, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(filepath.Join(repo, ".git", "config"), "[remote \"origin\"]\n\turl = https://github.com/o/r.git\n")
	run := filepath.Join(repo, ".nxy", "local")
	mustWrite(filepath.Join(run, "metrics", "filter.jsonl"),
		`{"session_id":"s1","engine":"rtk","filtered_chars":100,"est_tokens":25,"command_kind":"git"}`+"\n"+
			`{"session_id":"s1","engine":"skip","reason":"short","filtered_chars":10,"est_tokens":3}`+"\n"+
			`{"session_id":"s2","engine":"rtk","filtered_chars":50,"est_tokens":13}`+"\n")
	mustWrite(filepath.Join(run, "review.jsonl"),
		`{"kind":"review","id":"a1","ts":5,"findings":[{"id":"R1","lens":"bugs","severity":"high"},{"id":"R2","lens":"tests","severity":"low"}]}`+"\n"+
			`{"kind":"chosen","id":"a1","finding":"R1"}`+"\n")

	memDB = filepath.Join(home, "memory", "memory.db")
	if err := os.MkdirAll(filepath.Dir(memDB), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", memDB)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
CREATE TABLE memories (rowid INTEGER PRIMARY KEY, id TEXT NOT NULL UNIQUE, scope TEXT NOT NULL, project TEXT, area TEXT,
  type TEXT NOT NULL, title TEXT NOT NULL, body TEXT NOT NULL, keywords TEXT NOT NULL DEFAULT '', private INTEGER NOT NULL DEFAULT 0,
  created INTEGER NOT NULL, updated INTEGER NOT NULL);
CREATE TABLE edges (src TEXT NOT NULL, dst TEXT NOT NULL, kind TEXT NOT NULL, weight REAL NOT NULL DEFAULT 1,
  by TEXT NOT NULL DEFAULT 'code', created INTEGER NOT NULL, PRIMARY KEY (src, dst, kind));`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	for _, m := range [][]string{
		{"project-tabs-aaaaa", "project", "github.com/o/r", "convention", "usar tabs", "x"},
		{"project-handoff-bbbbb", "project", "github.com/o/r", "handoff", "handoff: arreglar login", "Goal: arreglar login\n## Next\n- probar"},
		{"project-handoff-done-ccccc", "project", "github.com/o/r", "handoff", "feature: cerrada", "listo"},
		{"project-otro-ddddd", "project", "github.com/o/otro", "convention", "de otro repo", "x"},
	} {
		if _, err := db.Exec(`INSERT INTO memories (id, scope, project, type, title, body, created, updated) VALUES (?,?,?,?,?,?,?,?)`,
			m[0], m[1], m[2], m[3], m[4], m[5], now-1000, now); err != nil {
			t.Fatal(err)
		}
	}
	return repo, memDB
}

func legacyRun(t *testing.T, args ...string) string {
	t.Helper()
	var out, errb bytes.Buffer
	if code := run(args, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("nxy %v: exit %d: %s%s", args, code, out.String(), errb.String())
	}
	return out.String()
}

func TestLegacyImportCommand(t *testing.T) {
	repo, memDB := legacyWorld(t)
	before, err := os.ReadFile(memDB)
	if err != nil {
		t.Fatal(err)
	}

	first := legacyRun(t, "legacy", "import", "--dir", repo)
	for _, want := range []string{
		"memoria: importado ahora", "1 convención → aprendizajes A1", "1 handoff vivo → notas", "1 archivado → features cerradas",
		"review: importado ahora", "1 review, 1 elegido", "filtro: 3 filas nuevas de 2 sesiones",
	} {
		if !strings.Contains(first, want) {
			t.Errorf("first import lacks %q:\n%s", want, first)
		}
	}

	second := legacyRun(t, "legacy", "import", "--dir", repo)
	if strings.Count(second, "ya importado el") != 2 || strings.Contains(second, "importado ahora") || !strings.Contains(second, "filtro: sin filas nuevas") {
		t.Errorf("second import:\n%s", second)
	}

	// What the 1.x writes later enters on the next run, and only that.
	f, err := os.OpenFile(filepath.Join(repo, ".nxy", "local", "metrics", "filter.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"session_id":"s2","engine":"rtk","filtered_chars":5,"est_tokens":2}` + "\n")
	f.Close()
	third := legacyRun(t, "legacy", "import", "--dir", repo)
	if !strings.Contains(third, "filtro: 1 filas nuevas de 1 sesiones") || !strings.Contains(third, "ya importado el") {
		t.Errorf("third import:\n%s", third)
	}

	// What stats and trend read: the totals per session, as the 1.x fields add up.
	rd, ok, err := sqlite.OpenReadOnly(context.Background(), filepath.Join(os.Getenv("NXY_HOME"), "nxy.db"))
	if err != nil || !ok {
		t.Fatalf("OpenReadOnly ok=%v err=%v", ok, err)
	}
	defer rd.Close()
	for session, want := range map[string][2]float64{"s1": {110, 28}, "s2": {55, 15}} {
		gs, err := rd.FilterGroups(context.Background(), platform.FilterCheckout(repo), session)
		if err != nil {
			t.Fatal(err)
		}
		sum := report.SummarizeFilterGroups(gs)
		if sum.FilteredChars != want[0] || sum.EstTokens != want[1] {
			t.Errorf("session %s: chars=%v tokens=%v, want %v", session, sum.FilteredChars, sum.EstTokens, want)
		}
	}

	status := legacyRun(t, "legacy", "status")
	for _, want := range []string{"memoria de github.com/o/r", "review de ", "filtro de ", "leídos", "de "} {
		if !strings.Contains(status, want) {
			t.Errorf("status lacks %q:\n%s", want, status)
		}
	}

	if notes := legacyRun(t, "note", "list", "--all", "--cwd", repo); !strings.Contains(notes, "arreglar login") {
		t.Errorf("imported note missing from note list:\n%s", notes)
	}

	after, err := os.ReadFile(memDB)
	if err != nil || !bytes.Equal(before, after) {
		t.Errorf("memory.db of the 1.x changed (err=%v)", err)
	}
	for _, ext := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(memDB + ext); err == nil {
			t.Errorf("%s appeared next to the 1.x memory.db", ext)
		}
	}
}

func TestLegacyImportNothingToImport(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	out := legacyRun(t, "legacy", "import", "--dir", dir)
	if !strings.Contains(out, "nada que importar") {
		t.Errorf("out = %q", out)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("NXY_HOME"), "nxy.db")); err == nil {
		t.Error("nxy.db was created with nothing to import")
	}
	if out := legacyRun(t, "legacy", "status"); !strings.Contains(out, "nada importado") {
		t.Errorf("status = %q", out)
	}
}

func TestLegacySessionStartImportsOnce(t *testing.T) {
	repo, _ := legacyWorld(t)
	start := `{"hook_event_name":"SessionStart","source":"startup","session_id":"s9","cwd":` + jsonString(repo) + `,"project_dir":` + jsonString(repo) + `}`
	var out, errb bytes.Buffer
	t.Setenv("CLAUDE_PROJECT_DIR", repo)
	if code := run([]string{"hook"}, strings.NewReader(start), &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "datos de la 1.x importados") {
		t.Fatalf("first start: %q", out.String())
	}
	out.Reset()
	if code := run([]string{"hook"}, strings.NewReader(start), &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if strings.Contains(out.String(), "datos de la 1.x importados") {
		t.Errorf("second start repeats the notice: %q", out.String())
	}
}

func jsonString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
