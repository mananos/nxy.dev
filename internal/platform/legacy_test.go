package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/goldentest"
)

// planSnapshot lists every file under root with its bytes and mtime, and every directory.
func planSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			out[p] = "dir"
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[p] = string(b) + "|" + info.ModTime().String()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestLegacyPlanFiles(t *testing.T) {
	root := realTemp(t)
	local := filepath.Join(root, ".nxy", "local")
	write := func(rel, body string) {
		p := filepath.Join(local, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	empty, err := ReadLegacyPlanFiles(root)
	if err != nil || len(empty.Approved) != 0 || len(empty.Verify) != 0 {
		t.Fatalf("missing .nxy/local: %+v %v", empty, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".nxy")); err == nil {
		t.Fatal("the read created .nxy")
	}

	write("plan-approvals.json", `["aaaa1111","bbbb2222"]`)
	write("verify/aaaa1111/batch-1.json", `{"status":"pass","branch":"main"}`)
	write("verify/aaaa1111/batch-2.json", `{"status":"fail"}`)
	write("verify/aaaa1111/batch-3.json", `not json`)
	write("verify/aaaa1111/notes.txt", `{"status":"pass"}`)
	write("verify/bbbb2222/batch-1.json", `{"status":"running","branch":"other"}`)
	write("verify/Bad_Name/batch-1.json", `{"status":"pass"}`)

	before := planSnapshot(t, root)
	got, err := ReadLegacyPlanFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Approved, ",") != "aaaa1111,bbbb2222" {
		t.Errorf("approved %v", got.Approved)
	}
	if len(got.Verify) != 2 || got.Verify["aaaa1111"][1] != "pass" || got.Verify["aaaa1111"][2] != "fail" ||
		len(got.Verify["aaaa1111"]) != 2 || got.Verify["bbbb2222"][1] != "running" {
		t.Errorf("verify %+v", got.Verify)
	}
	after := planSnapshot(t, root)
	if len(after) != len(before) {
		t.Fatalf("entries changed: %d -> %d", len(before), len(after))
	}
	for p, v := range before {
		if after[p] != v {
			t.Errorf("%s changed", p)
		}
	}

	write("plan-approvals.json", `{"not":"an array"}`)
	if bad, err := ReadLegacyPlanFiles(root); err != nil || len(bad.Approved) != 0 {
		t.Errorf("bad approvals: %+v %v", bad, err)
	}
}

func TestLegacyProjectKeyGolden(t *testing.T) {
	for _, c := range goldentest.Load(t, "legacykey") {
		c := c
		t.Run(c.File+"/"+c.Name, func(t *testing.T) {
			if strings.HasPrefix(c.Name, "remote:") {
				var text string
				c.In(t, "text", &text)
				var got any // null when the 1.x gives null
				if n := legacyNormalizeRemote(text); n != "" {
					got = n
				}
				goldentest.Check(t, c, map[string]any{"normalized": got})
				return
			}
			root := realTemp(t)
			rootText := filepath.ToSlash(root)
			var files map[string]string
			var cwd string
			c.In(t, "files", &files)
			c.In(t, "cwd", &cwd)
			for rel, text := range files {
				p := filepath.Join(root, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(strings.ReplaceAll(text, "@ROOT@", rootText)), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			dir := root
			if cwd != "" {
				dir = filepath.Join(root, filepath.FromSlash(cwd))
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			got := strings.ReplaceAll(LegacyProjectKey(dir), strings.ToLower(rootText), "@ROOT@")
			goldentest.Check(t, c, map[string]any{"key": got})
		})
	}
}

func TestLegacyReadJSONLFrom(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.jsonl")
	write := func(s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("{\"a\":1}\n{\"a\":2}\n{\"a\":")
	rows, cur, err := ReadJSONLFrom(p, 0)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%d err=%v, want 2 complete lines", len(rows), err)
	}
	if cur.Offset != 16 || cur.Size != 21 || cur.Head == "" {
		t.Fatalf("cursor = %+v", cur)
	}
	if FileHead(p, cur.Offset) != cur.Head {
		t.Fatal("head must hash the first bytes up to the offset")
	}
	// The line gets finished: only it is read.
	write("{\"a\":1}\n{\"a\":2}\n{\"a\":3}\nnot json\n")
	rows, next, err := ReadJSONLFrom(p, cur.Offset)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%d err=%v, want 1", len(rows), err)
	}
	if next.Offset != next.Size || next.Offset != int64(len("{\"a\":1}\n{\"a\":2}\n{\"a\":3}\nnot json\n")) {
		t.Fatalf("next = %+v", next)
	}
	// The head of the old cursor still matches the grown file.
	if FileHead(p, cur.Offset) != cur.Head {
		t.Fatal("head up to the old offset changed")
	}
	// Truncated: an offset past the end reads from the start.
	write("{\"b\":1}\n")
	rows, next, err = ReadJSONLFrom(p, next.Offset)
	if err != nil || len(rows) != 1 || next.Offset != 8 {
		t.Fatalf("truncated: rows=%d next=%+v err=%v", len(rows), next, err)
	}
	if FileHead(p, 16) == cur.Head {
		t.Fatal("head must change when the start of the file does")
	}
	// Empty and missing.
	write("")
	rows, next, err = ReadJSONLFrom(p, 0)
	if err != nil || len(rows) != 0 || next.Offset != 0 || next.Size != 0 {
		t.Fatalf("empty: rows=%d next=%+v err=%v", len(rows), next, err)
	}
	if _, _, err := ReadJSONLFrom(p+".nope", 0); err == nil {
		t.Fatal("missing file must be an error")
	}
	if _, ok := FileSize(p + ".nope"); ok {
		t.Fatal("missing file has no size")
	}
}

func TestLegacyPaths(t *testing.T) {
	got, err := LegacyMemoryDB(func(k string) string {
		if k == "NXY_HOME" {
			return filepath.Join("x", "home")
		}
		return ""
	})
	if err != nil || got != filepath.Join("x", "home", "memory", "memory.db") {
		t.Fatalf("LegacyMemoryDB = %q, %v", got, err)
	}
	dir := t.TempDir()
	if !strings.HasSuffix(filepath.ToSlash(LegacyFilterLog(dir)), ".nxy/local/metrics/filter.jsonl") ||
		!strings.HasSuffix(filepath.ToSlash(LegacyReviewLog(dir)), ".nxy/local/review.jsonl") ||
		!strings.HasSuffix(FilterCheckout(dir), "/.nxy/local") {
		t.Fatalf("paths: %q %q %q", LegacyFilterLog(dir), LegacyReviewLog(dir), FilterCheckout(dir))
	}
}
