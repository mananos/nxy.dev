package goldentest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRewriteOutputs(t *testing.T) {
	same := `{"name":"same","input":{"a":1},"output":{"x":"<&>é"}},`
	diff := `{"name":"diff","input":{"b":[ 2 ]},"output":{"x":"old"}}`
	path := filepath.Join(t.TempDir(), "g.json")
	if err := os.WriteFile(path, []byte("[\n"+same+"\n"+diff+"\n]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	RewriteOutputs(t, path, map[string]any{
		"same": map[string]any{"x": "<&>é"},
		"diff": map[string]any{"x": "<new & é>"},
	})
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "[\n" + same + "\n" + `{"name":"diff","input":{"b":[ 2 ]},"output":{"x":"<new & é>"}}` + "\n]\n"
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}

	// nothing changed: the file is not touched
	RewriteOutputs(t, path, map[string]any{"diff": map[string]any{"x": "<new & é>"}})
	again, _ := os.ReadFile(path)
	if string(again) != want {
		t.Fatal("unchanged case rewrote the file")
	}
}
