package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSummarize(t *testing.T) {
	ms := time.Millisecond
	cases := []struct {
		name string
		in   []time.Duration
		want stats
	}{
		{"empty", nil, stats{}},
		{"one", []time.Duration{5 * ms}, stats{5 * ms, 5 * ms, 5 * ms, 5 * ms}},
		{"ten unsorted", []time.Duration{10 * ms, 1 * ms, 9 * ms, 2 * ms, 8 * ms, 3 * ms, 7 * ms, 4 * ms, 6 * ms, 5 * ms},
			stats{1 * ms, 5 * ms, 9 * ms, 10 * ms}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := summarize(c.in); got != c.want {
				t.Fatalf("got %+v want %+v", got, c.want)
			}
		})
	}
}

func TestFormatRow(t *testing.T) {
	ms := time.Millisecond
	none := formatRow("r", 5*ms, nil, 3, 3)
	if !strings.Contains(none, "failed 3/3") || strings.Contains(none, "under") || strings.Contains(none, "over") {
		t.Fatalf("no-samples row: %s", none)
	}
	some := formatRow("r", 5*ms, []time.Duration{10 * ms}, 2, 3)
	if !strings.Contains(some, "failed 2/3") || !strings.Contains(some, "under") {
		t.Fatalf("partial row: %s", some)
	}
	if ok := formatRow("r", 5*ms, []time.Duration{10 * ms}, 0, 1); strings.Contains(ok, "failed") {
		t.Fatalf("clean row: %s", ok)
	}
}

func TestBenchHook(t *testing.T) {
	t.Setenv("NXY_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	bin := filepath.Join(t.TempDir(), exeName("nxy"))
	goBuild(t, ".", bin, ".")
	var out, errb bytes.Buffer
	code := run([]string{"bench", "hook", "-n", "2", "-bin", bin}, strings.NewReader(""), &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	s := out.String()
	for _, want := range []string{"| filter off |", "| filter on, no cache |", "| filter on |", "threshold 50 ms (14.20)"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
}
