// Package goldentest loads and compares the golden files (testdata/golden/<area>/*.json). Go
// writes them now: with NXY_WRITE_GOLDEN=1 Check does not compare, it keeps what the code computes
// and Load rewrites the `output` of the cases that changed. It is a test helper: only _test.go
// files import it.
package goldentest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
)

// Writing reports whether the golden files are being rewritten (NXY_WRITE_GOLDEN=1).
func Writing() bool { return os.Getenv("NXY_WRITE_GOLDEN") == "1" }

// saved holds, in write mode, the computed outputs: file path -> case name -> output.
var (
	savedMu sync.Mutex
	saved   = map[string]map[string]any{}
)

// Case is one golden case: {name, input, output}.
type Case struct {
	Area   string
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
	if Writing() {
		t.Cleanup(func() {
			savedMu.Lock()
			defer savedMu.Unlock()
			for _, f := range files {
				if outs, ok := saved[f]; ok {
					RewriteOutputs(t, f, outs)
					delete(saved, f)
				}
			}
		})
	}
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
			cases[i].Area = area
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
	if Writing() {
		path := filepath.Join(Root(t), "testdata", "golden", c.Area, c.File)
		out := map[string]any{}
		for k, v := range got {
			out[k] = Norm(t, v)
		}
		savedMu.Lock()
		if saved[path] == nil {
			saved[path] = map[string]any{}
		}
		saved[path][c.Name] = out
		savedMu.Unlock()
		return
	}
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

// RewriteOutputs rewrites the golden file at path, which is `[\n{case},\n...\n]` with one case per
// line. For each case named in outputs whose decoded `output` differs from the new value, only the
// `output` is replaced (compact JSON, no HTML escaping); every other line stays byte for byte and
// `name` and `input` never change.
func RewriteOutputs(t testing.TB, path string, outputs map[string]any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	changed := false
	for i, line := range lines {
		body := strings.TrimSuffix(line, ",")
		if !strings.HasPrefix(body, "{") {
			continue
		}
		var c struct {
			Name   json.RawMessage `json:"name"`
			Input  json.RawMessage `json:"input"`
			Output json.RawMessage `json:"output"`
		}
		if err := json.Unmarshal([]byte(body), &c); err != nil {
			t.Fatalf("%s line %d: %v", path, i+1, err)
		}
		var name string
		if err := json.Unmarshal(c.Name, &name); err != nil {
			t.Fatalf("%s line %d: %v", path, i+1, err)
		}
		newOut, ok := outputs[name]
		if !ok {
			continue
		}
		var old any
		if err := json.Unmarshal(c.Output, &old); err != nil {
			t.Fatalf("%s/%s: %v", path, name, err)
		}
		if reflect.DeepEqual(old, Norm(t, newOut)) {
			continue
		}
		enc, err := compact(newOut)
		if err != nil {
			t.Fatal(err)
		}
		in := string(c.Input)
		if c.Input == nil {
			in = "null"
		}
		lines[i] = fmt.Sprintf(`{"name":%s,"input":%s,"output":%s}`, c.Name, in, enc)
		if strings.HasSuffix(line, ",") {
			lines[i] += ","
		}
		changed = true
	}
	if !changed {
		return
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

func compact(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// NormRootText is normRootText of scripts/golden/fixtures.mjs: the root, raw or as embedded in a
// JSON string, becomes "<root>" followed by the rest of the path with forward slashes, and the
// root's project slug becomes "<rootslug>".
func NormRootText(text, root string) string {
	out := text
	esc, _ := json.Marshal(root)
	jsonRoot := string(esc[1 : len(esc)-1])
	if jsonRoot != root {
		re := regexp.MustCompile(regexp.QuoteMeta(jsonRoot) + `((?:\\\\|[^\s"\\),;])*)`)
		out = re.ReplaceAllStringFunc(out, func(m string) string {
			return "<root>" + strings.ReplaceAll(m[len(jsonRoot):], `\\`, "/")
		})
	}
	re := regexp.MustCompile(regexp.QuoteMeta(root) + `([^\s"'),;]*)`)
	out = re.ReplaceAllStringFunc(out, func(m string) string {
		return "<root>" + strings.ReplaceAll(m[len(root):], `\`, "/")
	})
	slug := []rune(root)
	for i, r := range slug {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			slug[i] = '-'
		}
	}
	return strings.ReplaceAll(out, string(slug), "<rootslug>")
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
