package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mananos/nxy.dev/v2/internal/tui"
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

// the dump without --demo reads the real db: an empty one still draws every section
func TestTuiSelfArgv(t *testing.T) {
	got, err := selfArgv([]string{"nxy", "mem", "rm", "K3"})
	if err != nil || strings.Join(got, " ") != "mem rm K3" {
		t.Fatalf("selfArgv = %v, %v", got, err)
	}
	for _, bad := range [][]string{nil, {}, {"rm", "-rf", "x"}, {"/usr/bin/nxy", "mem"}} {
		if _, err := selfArgv(bad); err == nil {
			t.Errorf("selfArgv(%v) should fail", bad)
		}
	}
}

func TestTuiWhere(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NXY_HOME", home)
	old := nxyOnPath
	t.Cleanup(func() { nxyOnPath = old })

	nxyOnPath = func() string { return "" }
	var out, errb bytes.Buffer
	if code := run([]string{"tui", "where"}, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if _, err := os.Stat(filepath.Join(home, "bin", "nxy.cmd")); err != nil {
		t.Errorf("shim not ensured: %v", err)
	}
	shim := filepath.ToSlash(filepath.Join(home, "bin", "nxy.cmd"))
	for _, want := range []string{"& \"" + shim + "\" tui", "sh \"" + shim + "\" tui", "nxy en el PATH"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("without nxy in the PATH, lacks %q:\n%s", want, out.String())
		}
	}

	nxyOnPath = func() string { return "/x/nxy" }
	out.Reset()
	if code := run([]string{"tui", "where"}, strings.NewReader(""), &out, &errb); code != 0 || !strings.Contains(out.String(), "  nxy tui") || strings.Contains(out.String(), "PowerShell") {
		t.Fatalf("with nxy in the PATH (%d):\n%s", code, out.String())
	}
}

func TestNxyOnPathSkipsPluginBin(t *testing.T) {
	root := t.TempDir()
	goos, name := runtime.GOOS, "nxy"
	if goos == "windows" {
		name = "nxy.exe"
	}
	mk := func(dir string) string {
		d := filepath.Join(root, filepath.FromSlash(dir))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, name), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
		return d
	}
	plugin := mk(".claude/plugins/x/bin")
	other := mk("pluginroot/bin")
	user := mk("user/bin")
	sep := string(os.PathListSeparator)

	if got := lookNxy(plugin, "", "", goos); got != "" {
		t.Errorf("plugin bin only: got %q", got)
	}
	if got := lookNxy(other, "", filepath.Join(root, "pluginroot"), goos); got != "" {
		t.Errorf("CLAUDE_PLUGIN_ROOT/bin only: got %q", got)
	}
	want := filepath.Join(user, name)
	if got := lookNxy(plugin+sep+other+sep+user, "", filepath.Join(root, "pluginroot"), goos); got != want {
		t.Errorf("with a user dir: got %q, want %q", got, want)
	}
}

func TestTuiRealDump(t *testing.T) {
	t.Setenv("NXY_HOME", t.TempDir())
	for _, target := range []string{"inicio", "memoria", "gasto", "config"} {
		var out, errb bytes.Buffer
		args := []string{"tui", "--dump", target, "--plain", "--icons", "unicode", "--cwd", t.TempDir()}
		if code := run(args, strings.NewReader(""), &out, &errb); code != 0 {
			t.Fatalf("%s: exit %d: %s", target, code, errb.String())
		}
		if strings.TrimSpace(out.String()) == "" {
			t.Errorf("%s: empty dump", target)
		}
	}
}
