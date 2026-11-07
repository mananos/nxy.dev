package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/internal/goldentest"
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
