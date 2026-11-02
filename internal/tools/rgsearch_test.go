package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mananos/nxy.dev/internal/goldentest"
)

func writeLocateTable(t *testing.T, table string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "table.json")
	if err := os.WriteFile(p, []byte(table), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NXY_GOLDEN_LOCATE_TABLE", p)
}

func stubBin(dir, name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, name)
}

func TestRgSearchParsesAndCaps(t *testing.T) {
	dir := goldentest.LocateStubs(t)
	writeLocateTable(t, `{"rg": {
	  "needle": {"code": 0, "stdout": "./a.go:3:   func needle() {  \n.\\b\\c.go:7:x\nnot a row\n./d.go:1:one\n./d.go:2:two\n"},
	  "-w:needle": {"code": 0, "stdout": "./w.go:1:word\n"},
	  "boom": {"code": 2, "stdout": ""}}}`)
	bin := stubBin(dir, "rg")
	cwd := t.TempDir()

	r := RgSearch(context.Background(), cwd, "needle", RgOpts{Bin: bin})
	if !r.OK || r.Truncated || len(r.Rows) != 4 {
		t.Fatalf("result = %+v", r)
	}
	if got := r.Rows[0]; got != (RgRow{Path: "a.go", Line: 3, Text: "func needle() {"}) {
		t.Errorf("row 0 = %+v", got)
	}
	if r.Rows[1].Path != "b/c.go" {
		t.Errorf("windows separators: %q", r.Rows[1].Path)
	}

	r = RgSearch(context.Background(), cwd, "needle", RgOpts{Bin: bin, MaxRows: 2})
	if !r.OK || !r.Truncated || len(r.Rows) != 2 {
		t.Errorf("capped = %+v", r)
	}
	if r = RgSearch(context.Background(), cwd, "needle", RgOpts{Bin: bin, Word: true}); len(r.Rows) != 1 || r.Rows[0].Path != "w.go" {
		t.Errorf("word = %+v", r)
	}
	// no match is an answer; an error is not
	if r = RgSearch(context.Background(), cwd, "absent", RgOpts{Bin: bin}); !r.OK || len(r.Rows) != 0 {
		t.Errorf("no match = %+v", r)
	}
	if r = RgSearch(context.Background(), cwd, "boom", RgOpts{Bin: bin}); r.OK || r.Reason != "exit-2" {
		t.Errorf("error = %+v", r)
	}
	if r = RgSearch(context.Background(), cwd, "x", RgOpts{Bin: filepath.Join(cwd, "nope")}); r.OK || r.Reason != "spawn-failed" {
		t.Errorf("missing bin = %+v", r)
	}
}

func TestRgSearchWithoutRg(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	get := func(k string) string {
		switch k {
		case "HOME", "USERPROFILE", "LOCALAPPDATA":
			return home
		}
		return ""
	}
	if runtime.GOOS != "windows" {
		if _, ok := FindRg(get); ok {
			t.Skip("a real rg sits in a fixed location of this machine")
		}
	}
	if r := RgSearch(context.Background(), home, "x", RgOpts{Getenv: get}); r.OK || r.Reason != "rg-missing" {
		t.Errorf("result = %+v", r)
	}
}

func TestGraphExploreAndRelations(t *testing.T) {
	root := t.TempDir()
	var asked [][]string
	g := &Graph{Argv: []string{"codegraph"}, Getenv: func(string) string { return "" },
		Exec: func(_ context.Context, dir string, argv []string, _ string) (int, string, error) {
			asked = append(asked, argv)
			switch argv[1] {
			case "explore":
				return 0, "  graph text \n", nil
			case "callers":
				return 3, "", nil
			case "callees":
				return 0, " \n", nil
			}
			return 0, repeat("é", 24_001), nil
		}}

	if r := g.Explore(context.Background(), root, "q"); r.OK || r.Reason != "no-index" {
		t.Fatalf("no index = %+v", r)
	}
	if err := os.Mkdir(filepath.Join(root, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if r := g.Explore(context.Background(), root, "where is x"); !r.OK || r.Text != "graph text" || r.Truncated {
		t.Errorf("explore = %+v", r)
	}
	if len(asked) != 1 || asked[0][1] != "explore" || asked[0][2] != "where is x" {
		t.Errorf("argv = %v", asked)
	}
	if r := g.Relations(context.Background(), root, "callers", "x"); r.OK || r.Reason != "exit-3" {
		t.Errorf("callers = %+v", r)
	}
	if r := g.Relations(context.Background(), root, "callees", "x"); r.OK || r.Reason != "empty" {
		t.Errorf("callees = %+v", r)
	}
	r := g.Relations(context.Background(), root, "impact", "x")
	if !r.OK || !r.Truncated || len([]rune(r.Text)) != 24_000 {
		t.Errorf("impact capped = ok:%v truncated:%v runes:%d", r.OK, r.Truncated, len([]rune(r.Text)))
	}
	if r := (&Graph{}).Explore(context.Background(), root, "q"); r.Reason != "not-installed" {
		t.Errorf("not installed = %+v", r)
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
