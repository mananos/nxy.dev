package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

// benchThreshold is the per-hook startup budget of decision 14.20.
const benchThreshold = 50 * time.Millisecond

type stats struct {
	Min, Median, P90, Max time.Duration
}

// summarize returns min/median/p90/max of the samples (nearest-rank percentile).
func summarize(samples []time.Duration) stats {
	if len(samples) == 0 {
		return stats{}
	}
	s := append([]time.Duration(nil), samples...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	rank := func(p float64) time.Duration {
		i := int(p*float64(len(s))+0.999999) - 1
		if i < 0 {
			i = 0
		}
		if i >= len(s) {
			i = len(s) - 1
		}
		return s[i]
	}
	return stats{Min: s[0], Median: rank(0.5), P90: rank(0.9), Max: s[len(s)-1]}
}

func ms(d time.Duration) string { return fmt.Sprintf("%.1f", float64(d.Microseconds())/1000) }

func runBench(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "hook" {
		fmt.Fprintln(stderr, "usage: nxy bench hook [-n 20] [-command \"git status\"] [-shim <path to bin/nxy.cmd>] [-bin <nxy binary>]")
		return 1
	}
	fs := flag.NewFlagSet("bench hook", flag.ContinueOnError)
	fs.SetOutput(stderr)
	n := fs.Int("n", 20, "timed runs per row")
	command := fs.String("command", "git status", "Bash command in the payload")
	shim := fs.String("shim", "", "path to bin/nxy.cmd, adds the `via shim` row")
	bin := fs.String("bin", "", "nxy binary (default: this one)")
	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}
	if *n < 1 {
		*n = 1
	}
	if *bin == "" {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintf(stderr, "nxy bench: %v\n", err)
			return 1
		}
		*bin = exe
	}
	cwd, _ := os.Getwd()
	payload, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Bash",
		"session_id":      "bench",
		"cwd":             cwd,
		"tool_input":      map[string]any{"command": *command},
	})

	type row struct {
		name string
		make func() *exec.Cmd
	}
	direct := func(env ...string) func() *exec.Cmd {
		return func() *exec.Cmd {
			c := exec.Command(*bin, "hook")
			c.Env = append(os.Environ(), env...)
			return c
		}
	}
	// `filter on` repeats one command, so after the first run rtk's answer comes from the rewrite
	// cache; `no cache` is what a command seen for the first time pays.
	rows := []row{
		{"filter off", direct("NXY_FILTER=0")},
		{"filter on, no cache", direct("NXY_REWRITE_CACHE=0")},
		{"filter on", direct()},
	}
	if *shim != "" {
		sh, err := exec.LookPath("bash")
		if err != nil {
			sh, err = exec.LookPath("sh")
		}
		if err != nil {
			fmt.Fprintln(stderr, "nxy bench: no bash or sh on PATH, skipping `via shim`")
		} else {
			abs, _ := filepath.Abs(*shim)
			viaShim := func(env ...string) func() *exec.Cmd {
				return func() *exec.Cmd {
					c := exec.Command(sh, "-c", `. "$1"; exit $LASTEXITCODE`, "_", abs)
					c.Env = append(append(os.Environ(), "NXY_BIN="+*bin), env...)
					return c
				}
			}
			// `via shim, filter off` minus `filter off` is what the shell costs by itself.
			rows = append(rows, row{"via shim, filter off", viaShim("NXY_FILTER=0")}, row{"via shim", viaShim()})
		}
	}

	once := func(r row) (time.Duration, error) {
		c := r.make()
		c.Stdin = bytes.NewReader(payload)
		var out bytes.Buffer
		c.Stdout, c.Stderr = &out, &out
		t := time.Now()
		err := c.Run()
		return time.Since(t), err
	}

	fmt.Fprintf(stdout, "nxy bench hook: %q, %d timed runs per row (ms)\n\n", *command, *n)
	fmt.Fprintf(stdout, "| row | first run | min | median | p90 | max | threshold 50 ms (14.20) |\n")
	fmt.Fprintf(stdout, "|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		first, err := once(r)
		if err != nil {
			fmt.Fprintf(stdout, "| %s | failed: %v | | | | | |\n", r.name, err)
			continue
		}
		samples := make([]time.Duration, 0, *n)
		failed := 0
		for i := 0; i < *n; i++ {
			d, err := once(r)
			if err != nil {
				failed++
				continue
			}
			samples = append(samples, d)
		}
		fmt.Fprintln(stdout, formatRow(r.name, first, samples, failed, *n))
	}
	return 0
}

// formatRow renders one table row. Failed runs are counted in the row; with no
// successful samples there are no numbers and no verdict.
func formatRow(name string, first time.Duration, samples []time.Duration, failed, n int) string {
	if len(samples) == 0 {
		return fmt.Sprintf("| %s | %s | | | | | no successful runs (failed %d/%d), no verdict |", name, ms(first), failed, n)
	}
	st := summarize(samples)
	verdict := "under"
	if st.Median > benchThreshold {
		verdict = "over"
	}
	note := ""
	if failed > 0 {
		note = fmt.Sprintf(" [failed %d/%d]", failed, n)
	}
	return fmt.Sprintf("| %s | %s | %s | %s | %s | %s | threshold 50 ms (14.20): %s (median)%s |",
		name, ms(first), ms(st.Min), ms(st.Median), ms(st.P90), ms(st.Max), verdict, note)
}
