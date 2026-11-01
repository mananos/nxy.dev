package platform

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/internal/goldentest"
	"github.com/mananos/nxy.dev/internal/repomap"
)

type fileSpec struct {
	Text  *string `json:"text"`
	Size  int     `json:"size"`
	Mtime int64   `json:"mtime"`
}

type buildStep struct {
	Name   string              `json:"name"`
	Write  map[string]fileSpec `json:"write"`
	Remove []string            `json:"remove"`
	Meta   *string             `json:"meta"`
	Force  bool                `json:"force"`
}

func realTemp(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func writeSpec(t *testing.T, root, rel string, s fileSpec) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("a", s.Size)
	if s.Text != nil {
		text = *s.Text
	}
	if err := os.WriteFile(abs, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	m := time.UnixMilli(s.Mtime)
	if err := os.Chtimes(abs, m, m); err != nil {
		t.Fatal(err)
	}
}

// shown is a build result or info as the golden prints it: map path relative to the root.
func shown(t *testing.T, root string, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(root, m["map"].(string))
	if err != nil {
		t.Fatal(err)
	}
	m["map"] = filepath.ToSlash(rel)
	return m
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRepoMapGolden(t *testing.T) {
	for _, c := range goldentest.Load(t, "repomap") {
		switch c.File {
		case "build.json", "info.json", "noindex.json":
		default:
			continue
		}
		t.Run(c.File+"/"+c.Name, func(t *testing.T) {
			root := realTemp(t)
			var now int64
			c.In(t, "now", &now)
			clock := func() time.Time { return time.UnixMilli(now) }
			switch c.File {
			case "noindex.json":
				var q string
				c.In(t, "query", &q)
				r := QueryRepoMap(root, q, repomap.Opts{})
				rows := []any{}
				goldentest.Check(t, c, map[string]any{"ok": r.OK, "reason": r.Reason, "rows": rows, "scanned": r.Scanned, "truncated": r.Truncated})
			case "info.json":
				checkInfo(t, c, root, clock)
			default:
				checkBuild(t, c, root, clock)
			}
		})
	}
}

func checkInfo(t *testing.T, c goldentest.Case, root string, clock func() time.Time) {
	paths := RepoMapPaths(root)
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var mapText, metaRaw *string
	var meta json.RawMessage
	c.In(t, "map", &mapText)
	c.In(t, "meta", &meta)
	c.In(t, "metaRaw", &metaRaw)
	if mapText != nil {
		if err := os.WriteFile(paths.Map, []byte(*mapText), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if string(meta) != "null" && meta != nil {
		if err := os.WriteFile(paths.Meta, meta, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if metaRaw != nil {
		if err := os.WriteFile(paths.Meta, []byte(*metaRaw), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var got any
	if info := RepoMapInfo(root, clock); info != nil {
		got = shown(t, root, info)
	}
	goldentest.Check(t, c, map[string]any{"info": got})
}

func checkBuild(t *testing.T, c goldentest.Case, root string, clock func() time.Time) {
	var gitignore *string
	var tree map[string]fileSpec
	var steps []buildStep
	c.In(t, "gitignore", &gitignore)
	c.In(t, "tree", &tree)
	c.In(t, "steps", &steps)
	if gitignore != nil {
		if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(*gitignore), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for rel, s := range tree {
		writeSpec(t, root, rel, s)
	}
	paths := RepoMapPaths(root)
	first, err := BuildRepoMap(root, false, clock)
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(readFile(t, paths.Meta)), &meta); err != nil {
		t.Fatal(err)
	}
	meta["root"] = "<root>" // written in path order here, in readdir order by the 1.x: compared as a value
	listed := []any{}
	files := ListSourceFiles(root)
	sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel }) // BMP names: byte order = code-unit order
	for _, f := range files {
		listed = append(listed, map[string]any{"rel": f.Rel, "size": f.Size, "mtimeMs": f.MtimeMs})
	}
	outSteps := []any{}
	for _, s := range steps {
		for rel, spec := range s.Write {
			writeSpec(t, root, rel, spec)
		}
		for _, rel := range s.Remove {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
				t.Fatal(err)
			}
		}
		if s.Meta != nil {
			if err := os.WriteFile(paths.Meta, []byte(*s.Meta), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		r, err := BuildRepoMap(root, s.Force, clock)
		if err != nil {
			t.Fatal(err)
		}
		outSteps = append(outSteps, map[string]any{"name": s.Name, "result": shown(t, root, r), "map": readFile(t, paths.Map)})
	}
	goldentest.Check(t, c, map[string]any{
		"first": shown(t, root, first), "map": readFile(t, paths.Map), "meta": meta, "listed": listed, "steps": outSteps,
	})
}
