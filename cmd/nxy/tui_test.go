package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mananos/nxy.dev/internal/tui"
)

func TestTuiDump(t *testing.T) {
	for _, tg := range tui.DumpTargets() {
		for _, w := range []int{80, 120, 200} {
			t.Run(fmt.Sprintf("%s/%d", tg.ID, w), func(t *testing.T) {
				var out, errb bytes.Buffer
				args := []string{"tui", "--demo", "--dump", tg.ID, "--width", fmt.Sprint(w), "--plain", "--icons", "unicode"}
				if tg.Key != "" {
					args = append(args, "--feature", tg.Key)
				}
				code := run(args, strings.NewReader(""), &out, &errb)
				if code != 0 {
					t.Fatalf("exit %d: %s", code, errb.String())
				}
				if strings.Contains(out.String(), "\x1b") {
					t.Fatalf("ESC in plain output")
				}
				lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
				for i, l := range lines {
					if n := ansi.StringWidth(l); n > w {
						t.Fatalf("line %d is %d cells, width %d: %q", i+1, n, w, l)
					}
				}
				if w == 200 {
					// the content takes the whole width, aligned with the bars: it starts
					// near the left edge, never centered
					near := false
					for _, l := range lines[1 : len(lines)-1] {
						if strings.TrimSpace(l) != "" && len(l)-len(strings.TrimLeft(l, " ")) <= 3 {
							near = true
						}
					}
					if !near && tg.ID != "decision" {
						t.Fatalf("no line of the body starts near the left edge")
					}
				}
			})
		}
	}
}

func TestTuiDumpUnknown(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"tui", "--demo", "--dump", "nope"}, strings.NewReader(""), &out, &errb)
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	for _, tg := range tui.DumpTargets() {
		if !strings.Contains(errb.String(), tg.ID) {
			t.Errorf("stderr does not list %q: %s", tg.ID, errb.String())
		}
	}
}

func TestTuiNoDemo(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"tui"}, strings.NewReader(""), &out, &errb); code != 1 {
		t.Fatalf("exit %d", code)
	}
}
