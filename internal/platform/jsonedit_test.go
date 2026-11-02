package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJSONDocKeepsOrderAndSpelling(t *testing.T) {
	in := `{"z":1,"model":"opus","hooks":{"b":[1,2],"a":{"x":true}},"n":1e-7,"s":"<&>"}`
	doc, err := ParseJSONDoc([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Set([]string{"hooks", "a", "y"}, "new"); err != nil {
		t.Fatal(err)
	}
	if err := doc.Set([]string{"statusLine"}, map[string]any{"type": "command"}); err != nil {
		t.Fatal(err)
	}
	got, err := doc.Text()
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "z": 1,
  "model": "opus",
  "hooks": {
    "b": [
      1,
      2
    ],
    "a": {
      "x": true,
      "y": "new"
    }
  },
  "n": 1e-7,
  "s": "<&>",
  "statusLine": {
    "type": "command"
  }
}
`
	if string(got) != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestJSONDocDeletePrunes(t *testing.T) {
	doc, err := ParseJSONDoc([]byte(`{"roles":{"planner":{"effort":"high"}},"keep":1}`))
	if err != nil {
		t.Fatal(err)
	}
	doc.Delete("roles", "planner", "effort")
	got, _ := doc.Text()
	if string(got) != "{\n  \"keep\": 1\n}\n" {
		t.Fatalf("got %q", got)
	}
	if v, ok := doc.Get("keep"); !ok || v != float64(1) {
		t.Fatalf("Get keep = %v %v", v, ok)
	}
	doc.Delete("missing", "x") // no-op
}

func TestJSONDocRejectsNonObject(t *testing.T) {
	for _, in := range []string{`[1]`, `"x"`, `{"a":1} {}`, `{broken`} {
		if _, err := ParseJSONDoc([]byte(in)); err == nil {
			t.Errorf("%s: want error", in)
		}
	}
	if _, err := ParseJSONDoc([]byte(`[1]`)); !errors.Is(err, ErrNotObject) {
		t.Errorf("array: err = %v", err)
	}
}

func TestReadJSONDocMissing(t *testing.T) {
	doc, exists, err := ReadJSONDoc(filepath.Join(t.TempDir(), "none.json"))
	if err != nil || exists || doc == nil {
		t.Fatalf("doc=%v exists=%v err=%v", doc, exists, err)
	}
}

func TestBackupAndPruneKeepsThree(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// someone else's backup is never touched
	other := p + ".bak-123"
	os.WriteFile(other, []byte("x"), 0o644)
	// two backups in the same millisecond: the name moves forward
	if a, _, _ := BackupAndPrune(p, time.UnixMilli(500)); !strings.HasSuffix(a, ".bak-nxy-500") {
		t.Fatalf("first backup %s", a)
	}
	if b, _, _ := BackupAndPrune(p, time.UnixMilli(500)); !strings.HasSuffix(b, ".bak-nxy-501") {
		t.Fatalf("same-ms backup %s", b)
	}
	for _, e := range []string{".bak-nxy-500", ".bak-nxy-501"} {
		os.Remove(p + e)
	}
	var last string
	for i := 0; i < 5; i++ {
		b, kept, err := BackupAndPrune(p, time.UnixMilli(int64(1000+i*10)))
		if err != nil {
			t.Fatal(err)
		}
		if want := min(i+1, BackupKeep); kept != want {
			t.Fatalf("run %d: kept=%d want %d", i, kept, want)
		}
		last = b
	}
	entries, _ := os.ReadDir(dir)
	var mine []string
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bak-nxy-") {
			mine = append(mine, e.Name())
		}
	}
	if len(mine) != BackupKeep {
		t.Fatalf("backups = %v", mine)
	}
	if _, err := os.Stat(last); err != nil {
		t.Fatalf("newest backup gone: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("foreign backup removed: %v", err)
	}
}
