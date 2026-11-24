package tui

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/views"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func noPath(string) (string, error) { return "", errors.New("no") }

func onlyPath(names ...string) func(string) (string, error) {
	return func(n string) (string, error) {
		for _, x := range names {
			if x == n {
				return n, nil
			}
		}
		return "", errors.New("no")
	}
}

func TestEditorVisualWins(t *testing.T) {
	c, susp, ok := editorCmd("linux", envOf(map[string]string{"VISUAL": "nano", "EDITOR": "vim"}), noPath, "a.go", 7)
	if !ok || !susp || !reflect.DeepEqual(c.Args, []string{"nano", "+7", "a.go"}) {
		t.Fatalf("got %v %v", ok, c)
	}
}

func TestEditorCodeUsesG(t *testing.T) {
	c, _, ok := editorCmd("linux", envOf(map[string]string{"EDITOR": "code --wait"}), noPath, "a.go", 3)
	if !ok || !reflect.DeepEqual(c.Args, []string{"code", "--wait", "-g", "a.go:3"}) {
		t.Fatalf("got %v %v", ok, c)
	}
}

func TestEditorVimPlusN(t *testing.T) {
	c, _, ok := editorCmd("linux", envOf(map[string]string{"EDITOR": "vim"}), noPath, "a.go", 0)
	if !ok || !reflect.DeepEqual(c.Args, []string{"vim", "+1", "a.go"}) {
		t.Fatalf("got %v %v", ok, c)
	}
}

func TestEditorFallback(t *testing.T) {
	c, susp, ok := editorCmd("linux", envOf(nil), onlyPath("code", "xdg-open"), "a.go", 4)
	if !ok || susp || !reflect.DeepEqual(c.Args, []string{"code", "-g", "a.go:4"}) {
		t.Fatalf("code in PATH: %v %v %v", ok, susp, c)
	}
	c, susp, ok = editorCmd("windows", envOf(nil), noPath, "a.go", 4)
	if !ok || susp || !reflect.DeepEqual(c.Args, []string{"rundll32", "url.dll,FileProtocolHandler", "a.go"}) {
		t.Fatalf("windows: %v %v %v", ok, susp, c)
	}
	c, _, ok = editorCmd("linux", envOf(nil), onlyPath("xdg-open"), "a.go", 4)
	if !ok || !reflect.DeepEqual(c.Args, []string{"xdg-open", "a.go"}) {
		t.Fatalf("linux: %v %v", ok, c)
	}
	st := newState(Options{Getenv: envOf(nil), GOOS: "linux", LookPath: noPath})
	got := st.openEditor("a.go", 2)
	if !strings.Contains(got, "a.go:2") || !strings.Contains(got, "EDITOR") || st.cmd != nil {
		t.Fatalf("toast %q cmd %v", got, st.cmd)
	}
	st = newState(Options{Getenv: envOf(nil), GOOS: "linux", LookPath: onlyPath("code")})
	if got := st.openEditor("/x/a.go", 2); !strings.Contains(got, "abriendo a.go con code") || st.cmd == nil {
		t.Fatalf("toast %q cmd %v", got, st.cmd)
	}
}

func TestEditorAbsFile(t *testing.T) {
	st := newState(Options{Getenv: envOf(nil)})
	root := filepath.Join(t.TempDir(), "api")
	st.data.Cockpit = &views.Cockpit{Features: []views.CockpitFeature{{ID: "F1", Repos: []views.RepoRef{{Name: "api", Path: root}}}}}
	got := st.absFile("F1", "api", "src/a.go")
	if want := filepath.Join(root, "src", "a.go"); got != want || !strings.HasPrefix(got, root) {
		t.Fatalf("got %q want %q", got, want)
	}
	if got := st.absFile("nope", "api", "src/a.go"); got != "src/a.go" {
		t.Fatalf("unknown feature: %q", got)
	}
}
