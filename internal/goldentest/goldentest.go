// Package goldentest loads and compares the golden files the 1.x JS generates
// (testdata/golden/<area>/*.json). It is a test helper: only _test.go files import it.
package goldentest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Case is one golden case: {name, input, output}.
type Case struct {
	File   string
	Name   string                     `json:"name"`
	Input  map[string]json.RawMessage `json:"input"`
	Output map[string]json.RawMessage `json:"output"`
}

// Root returns the module root (the nearest parent holding go.mod).
func Root(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// Load reads every testdata/golden/<area>/*.json (name order) and tags each case with its file.
func Load(t testing.TB, area string) []Case {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(Root(t), "testdata", "golden", area, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden files for %s (err=%v)", area, err)
	}
	sort.Strings(files)
	var all []Case
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var cases []Case
		if err := json.Unmarshal(raw, &cases); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for i := range cases {
			cases[i].File = filepath.Base(f)
		}
		all = append(all, cases...)
	}
	return all
}

// Has reports whether the case input carries key.
func (c Case) Has(key string) bool {
	_, ok := c.Input[key]
	return ok
}

// In decodes input[key] into v; a missing key leaves v untouched.
func (c Case) In(t testing.TB, key string, v any) {
	t.Helper()
	raw, ok := c.Input[key]
	if !ok {
		return
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s/%s: input.%s: %v", c.File, c.Name, key, err)
	}
}

// Norm round-trips a value through JSON so it compares with decoded golden output.
func Norm(t testing.TB, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Check compares got with the case output, exactly: every output key must be computed and every
// computed key must be in the output.
func Check(t testing.TB, c Case, got map[string]any) {
	t.Helper()
	for key, raw := range c.Output {
		var want any
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
		g, ok := got[key]
		if !ok {
			t.Errorf("%s/%s: golden has output %q the port does not compute", c.File, c.Name, key)
			continue
		}
		if !reflect.DeepEqual(want, Norm(t, g)) {
			in, _ := json.Marshal(c.Input)
			gj, _ := json.Marshal(g)
			t.Errorf("%s/%s: %s\n input: %s\n want: %s\n  got: %s", c.File, c.Name, key, in, raw, gj)
		}
	}
	for key := range got {
		if _, ok := c.Output[key]; !ok {
			t.Errorf("%s/%s: port computes %q the golden does not have", c.File, c.Name, key)
		}
	}
}

// NormRoot turns every string starting with root into "<root>" + rest, backslashes as slashes.
// It walks maps and slices produced by json.Unmarshal into any.
func NormRoot(v any, root string) any {
	switch x := v.(type) {
	case string:
		if strings.HasPrefix(x, root) {
			return "<root>" + strings.ReplaceAll(x[len(root):], `\`, "/")
		}
		return x
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = NormRoot(e, root)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = NormRoot(e, root)
		}
		return out
	}
	return v
}
