package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/goldentest"
)

// filterStatsCase is the first stats golden case that has filter rows and a plain text output.
func filterStatsCase(t *testing.T) usageCase {
	t.Helper()
	var cases []usageCase
	readGoldenFile(t, "stats.json", &cases)
	for _, c := range cases {
		if len(c.Input.FilterRows) > 0 && c.Output.Code == 0 && len(c.Input.Steps) == 0 {
			return c
		}
	}
	t.Fatal("no stats case with filter rows")
	return usageCase{}
}

func runStatsOf(t *testing.T, w *usageWorld, c *usageCase) string {
	t.Helper()
	u, done := newUsage(w.getenv, func() string { return w.proj }, func() time.Time { return w.now }, w.loc)
	defer done()
	return goldentest.NormRootText(u.Stats(w.expandArgs(c.Input.Args)).Stdout, w.root)
}

func TestUsageReadOnlyImportTwice(t *testing.T) {
	texts := loadUsageTexts(t)
	c := filterStatsCase(t)
	w := materializeUsage(t, &c, texts)
	first := runStatsOf(t, w, &c)
	w.importFilter(t)
	w.importFilter(t)
	if second := runStatsOf(t, w, &c); second != first {
		t.Errorf("importing twice changed the output: %s", diffAt(first, second))
	}
	if first != c.Output.Stdout {
		t.Errorf("stdout differs from the 1.x golden: %s", diffAt(c.Output.Stdout, first))
	}
}

// TestUsageReadOnlySessionUsage: stats and trend read session_usage read only. Without nxy.db they
// do not create it; with a base that predates the table the output is the same as without a base.
func TestUsageReadOnlySessionUsage(t *testing.T) {
	texts := loadUsageTexts(t)
	c := filterStatsCase(t)
	skipFilterImport = true
	t.Cleanup(func() { skipFilterImport = false })
	w := materializeUsage(t, &c, texts)
	dbPath := filepath.Join(w.root, "nxyhome", "nxy.db")

	run := func() (string, string) {
		u, done := newUsage(w.getenv, func() string { return w.proj }, func() time.Time { return w.now }, w.loc)
		defer done()
		if u.SessionCache == nil || u.FilterGroupsMany == nil {
			t.Fatal("newUsage leaves SessionCache or FilterGroupsMany unset")
		}
		args := w.expandArgs(c.Input.Args)
		return goldentest.NormRootText(u.Stats(args).Stdout, w.root), goldentest.NormRootText(u.Trend(nil).Stdout, w.root)
	}
	stats0, trend0 := run()
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("stats or trend created nxy.db (stat err: %v)", err)
	}
	if _, err := os.Stat(filepath.Join(w.root, "nxyhome")); err == nil {
		if _, err := os.Stat(dbPath); err == nil {
			t.Fatal("nxy.db exists")
		}
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE other (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	stats1, trend1 := run()
	if stats1 != stats0 {
		t.Errorf("stats with a base without session_usage differs: %s", diffAt(stats0, stats1))
	}
	if trend1 != trend0 {
		t.Errorf("trend with a base without session_usage differs: %s", diffAt(trend0, trend1))
	}
}

func TestUsageReadOnlyNoDB(t *testing.T) {
	texts := loadUsageTexts(t)
	c := filterStatsCase(t)

	skipFilterImport = true
	t.Cleanup(func() { skipFilterImport = false })
	w := materializeUsage(t, &c, texts)
	got := runStatsOf(t, w, &c)
	if _, err := os.Stat(filepath.Join(w.root, "nxyhome", "nxy.db")); !os.IsNotExist(err) {
		t.Errorf("stats created nxy.db (stat err: %v)", err)
	}

	bare := c
	bare.Input.FilterRows = nil
	wb := materializeUsage(t, &bare, texts)
	if want := runStatsOf(t, wb, &bare); got != want {
		t.Errorf("a world without nxy.db differs from one without filter.jsonl: %s", diffAt(want, got))
	}
}
