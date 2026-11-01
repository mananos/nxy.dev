package tools

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/internal/goldentest"
)

func TestRtkHistoryDBPathGolden(t *testing.T) {
	n := 0
	for _, c := range goldentest.Load(t, "report") {
		if !strings.HasPrefix(c.Name, "dbpath:") {
			continue
		}
		n++
		t.Run(c.Name, func(t *testing.T) {
			var in struct {
				Env       map[string]string `json:"env"`
				Platforms []string          `json:"platforms"`
			}
			var out struct {
				Paths []string `json:"paths"`
			}
			c.In(t, "env", &in.Env)
			c.In(t, "platforms", &in.Platforms)
			raw, _ := json.Marshal(c.Output)
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatal(err)
			}
			goos := map[string]string{"win32": "windows", "linux": "linux", "darwin": "darwin"}
			for i, p := range in.Platforms {
				got := RtkHistoryDBPath(goos[p], func(k string) string { return in.Env[k] })
				got = strings.ReplaceAll(got, `\`, "/")
				if got != out.Paths[i] {
					t.Errorf("%s: got %q, want %q", p, got, out.Paths[i])
				}
			}
		})
	}
	if n == 0 {
		t.Fatal("no dbpath cases")
	}
}

func makeLedger(t *testing.T, rows [][]any) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "history.db")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE commands (timestamp TEXT, project_path TEXT, input_tokens INTEGER, output_tokens INTEGER, saved_tokens INTEGER)`); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO commands VALUES (?,?,?,?,?)`, r...); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestReadLedger(t *testing.T) {
	p := makeLedger(t, [][]any{
		{"2026-03-01T10:00:00Z", `C:\Work\Proj\`, 100, 40, 60},
		{"2026-03-01T10:00:30.500Z", "/w/p", 10, 5, 5},
		{"2026-03-01T11:00:00Z", "/w/p", 1, 1, 0},
		{"garbage", "/w/p", 1, 1, 0},
		{"2026-03-01T10:01:00Z", nil, "x", nil, 7},
	})
	rows, ok := ReadLedger(p, nil, nil)
	if !ok || len(rows) != 4 {
		t.Fatalf("all: ok=%v rows=%v", ok, rows)
	}
	if rows[1].Ts != time.Date(2026, 3, 1, 10, 0, 30, 500e6, time.UTC).UnixMilli() || rows[1].Project != "/w/p" {
		t.Errorf("row 1: %+v", rows[1])
	}
	if rows[0].Input != 100 || rows[0].Saved != 60 {
		t.Errorf("row 0: %+v", rows[0])
	}
	if rows[2].Project != "" || rows[2].Input != 0 || rows[2].Saved != 7 {
		t.Errorf("row 2 (nulls): %+v", rows[2])
	}
	since := time.Date(2026, 3, 1, 10, 0, 30, 0, time.UTC).UnixMilli()
	until := time.Date(2026, 3, 1, 10, 0, 59, 0, time.UTC).UnixMilli()
	rows, ok = ReadLedger(p, &since, &until)
	if !ok || len(rows) != 1 || rows[0].Project != "/w/p" {
		t.Fatalf("window: ok=%v rows=%v", ok, rows)
	}
	empty := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	rows, ok = ReadLedger(p, &empty, nil)
	if !ok || rows == nil || len(rows) != 0 {
		t.Fatalf("empty window: ok=%v rows=%#v", ok, rows)
	}
	if got := LedgerUnavailable(p); got != "" {
		t.Errorf("unavailable = %q", got)
	}
}

func TestReadLedgerUnreadable(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "none.db")
	if rows, ok := ReadLedger(missing, nil, nil); ok || rows != nil {
		t.Errorf("missing: %v %v", rows, ok)
	}
	if LedgerUnavailable(missing) != "no-db" {
		t.Error("missing should be no-db")
	}
	bad := filepath.Join(dir, "bad.db")
	if err := os.WriteFile(bad, []byte(strings.Repeat("not a database ", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadLedger(bad, nil, nil); ok {
		t.Error("garbage file should be unreadable")
	}
	noTable := makeLedger(t, nil)
	db, _ := sql.Open("sqlite", noTable)
	db.Exec(`DROP TABLE commands`)
	db.Close()
	if _, ok := ReadLedger(noTable, nil, nil); ok {
		t.Error("missing table should fail")
	}
}

func TestGain(t *testing.T) {
	stub := buildStub(t)
	dir := t.TempDir()
	gain := filepath.Join(dir, "gain.json")
	if err := os.WriteFile(gain, []byte(`{"daily":[{"date":"2026-03-01","saved_tokens":5}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NXY_GOLDEN_RTK_GAIN", gain)
	v, ok := Gain(stub, []string{"--all"}, dir)
	want := map[string]any{"daily": []any{map[string]any{"date": "2026-03-01", "saved_tokens": 5.0}}}
	if !ok || !reflect.DeepEqual(v, want) {
		t.Fatalf("gain = %v, %v", v, ok)
	}
	if err := os.WriteFile(gain, []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := Gain(stub, []string{"--all"}, dir); ok {
		t.Error("bad JSON should be false")
	}
	if err := os.WriteFile(gain, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := Gain(stub, nil, dir); ok {
		t.Error("empty stdout should be false")
	}
	t.Setenv("NXY_GOLDEN_RTK_GAIN", "")
	if _, ok := Gain(stub, nil, dir); ok {
		t.Error("non-zero exit should be false")
	}
	if _, ok := Gain(filepath.Join(dir, "nope"), nil, dir); ok {
		t.Error("missing binary should be false")
	}
}
