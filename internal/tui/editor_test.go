package tui

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/views"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestEditorVisualWins(t *testing.T) {
	c, ok := editorCmd(envOf(map[string]string{"VISUAL": "nano", "EDITOR": "vim"}), "a.go", 7)
	if !ok || !reflect.DeepEqual(c.Args, []string{"nano", "+7", "a.go"}) {
		t.Fatalf("got %v %v", ok, c)
	}
}

func TestEditorCodeUsesG(t *testing.T) {
	c, ok := editorCmd(envOf(map[string]string{"EDITOR": "code --wait"}), "a.go", 3)
	if !ok || !reflect.DeepEqual(c.Args, []string{"code", "--wait", "-g", "a.go:3"}) {
		t.Fatalf("got %v %v", ok, c)
	}
}

func TestEditorVimPlusN(t *testing.T) {
	c, ok := editorCmd(envOf(map[string]string{"EDITOR": "vim"}), "a.go", 0)
	if !ok || !reflect.DeepEqual(c.Args, []string{"vim", "+1", "a.go"}) {
		t.Fatalf("got %v %v", ok, c)
	}
}

func TestEditorNoneToast(t *testing.T) {
	if _, ok := editorCmd(envOf(nil), "a.go", 2); ok {
		t.Fatal("no editor must not build a command")
	}
	st := newState(Options{Getenv: envOf(nil)})
	if got := st.openEditor("a.go", 2); got != "a.go:2" || st.cmd != nil {
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
