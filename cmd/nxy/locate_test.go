package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/goldentest"
)

func writeTree(t *testing.T, root string, tree map[string]string) {
	t.Helper()
	for rel, text := range tree {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

var msInText = regexp.MustCompile(`_\d+ ms`)

// TestGoldenLocateReport runs `nxy locate` end to end over the trees of the 1.x golden, with rg and
// codegraph replaced by internal/goldentest/locatestub, and compares text, hits, terms and degraded.
func TestGoldenLocateReport(t *testing.T) {
	isolate(t)
	stubs := goldentest.LocateStubs(t)
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	t.Setenv("PATH", stubs)
	t.Setenv("NXY_CODEGRAPH_PATH", filepath.Join(stubs, "codegraph"+ext))
	t.Setenv("CODEGRAPH_DIR", "")

	for _, c := range goldentest.Load(t, "locate") {
		if c.File != "report.json" {
			continue
		}
		t.Run(c.Name, func(t *testing.T) {
			var in struct {
				Mode      string
				Question  *string
				Symbol    *string
				Codegraph bool
				Index     bool
				Rebuild   bool
				Tree      map[string]string
				Rg        map[string]struct {
					Code int
					Rows []string
				}
				Cg map[string]struct {
					Code   int    `json:"code"`
					Stdout string `json:"stdout"`
				}
			}
			c.In(t, "mode", &in.Mode)
			c.In(t, "question", &in.Question)
			c.In(t, "symbol", &in.Symbol)
			c.In(t, "codegraph", &in.Codegraph)
			c.In(t, "index", &in.Index)
			c.In(t, "rebuild", &in.Rebuild)
			c.In(t, "tree", &in.Tree)
			c.In(t, "rg", &in.Rg)
			c.In(t, "cg", &in.Cg)

			root := t.TempDir()
			writeTree(t, root, in.Tree)
			if in.Index {
				writeTree(t, root, map[string]string{".codegraph/codegraph.db": "x"})
			}
			if !in.Codegraph {
				writeTree(t, root, map[string]string{".nxy/config.json": `{"scout":{"codegraph":false}}`})
			}
			rg := map[string]map[string]any{}
			for k, v := range in.Rg {
				out := ""
				if len(v.Rows) > 0 {
					out = strings.Join(v.Rows, "\n") + "\n"
				}
				rg[k] = map[string]any{"code": v.Code, "stdout": out}
			}
			table, _ := json.Marshal(map[string]any{"rg": rg, "codegraph": in.Cg})
			tablePath := filepath.Join(t.TempDir(), "table.json")
			if err := os.WriteFile(tablePath, table, 0o644); err != nil {
				t.Fatal(err)
			}
			t.Setenv("NXY_GOLDEN_LOCATE_TABLE", tablePath)

			args := []string{"locate", "--cwd", root}
			if in.Rebuild {
				args = append(args, "--rebuild")
			}
			if in.Mode != "search" {
				args = append(args, "--"+in.Mode, *in.Symbol)
			} else {
				args = append(args, *in.Question)
			}
			code, text, _ := runCmd(t, "", args...)
			if code != 0 {
				t.Fatalf("exit %d: %s", code, text)
			}
			text = msInText.ReplaceAllString(strings.TrimSuffix(text, "\n"), "_<ms> ms")

			code, raw, _ := runCmd(t, "", append(args, "--json")...)
			if code != 0 {
				t.Fatalf("json exit %d: %s", code, raw)
			}
			var j struct {
				OK        bool           `json:"ok"`
				Mode      string         `json:"mode"`
				Hits      []any          `json:"hits"`
				Terms     []string       `json:"terms"`
				Degraded  []string       `json:"degraded"`
				Codegraph string         `json:"codegraph"`
				Extra     map[string]any `json:"-"`
			}
			if err := json.Unmarshal([]byte(raw), &j); err != nil {
				t.Fatalf("json: %v\n%s", err, raw)
			}
			if !j.OK || j.Mode != in.Mode {
				t.Errorf("json = %s", raw)
			}
			goldentest.Check(t, c, map[string]any{"text": text, "hits": j.Hits, "terms": j.Terms, "degraded": j.Degraded, "codegraph": j.Codegraph})
		})
	}
}

func TestLocateStatusIndexAndJSON(t *testing.T) {
	isolate(t)
	t.Setenv("PATH", t.TempDir()) // no rg, no codegraph
	t.Setenv("CODEGRAPH_DIR", "")
	// a path that is not there: codegraph reads as not installed whatever this machine has
	t.Setenv("NXY_CODEGRAPH_PATH", filepath.Join(t.TempDir(), "no-codegraph"))
	t.Setenv("LOCALAPPDATA", t.TempDir())
	repo := t.TempDir()
	writeTree(t, repo, map[string]string{
		"src/a.js":         "export function cancelBooking(id) {\n  return id;\n}\n",
		".nxy/config.json": `{"scout":{"codegraph":false}}`,
	})

	code, out, _ := runCmd(t, "", "locate", "--status", "--cwd", repo)
	if code != 0 || !strings.Contains(out, "repo map:     not built") || !strings.Contains(out, "codegraph:    not installed") {
		t.Fatalf("status (%d):\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(repo, ".nxy", "local", "index")); err == nil {
		t.Fatal("--status must not build the index")
	}

	code, out, _ = runCmd(t, "", "locate", "--index", "--cwd", repo)
	if code != 0 || !strings.HasPrefix(out, "repo map: 1 symbols from 1 files in ") || !strings.Contains(out, "repomap.tsv") {
		t.Fatalf("index (%d):\n%s", code, out)
	}
	for _, f := range []string{"repomap.tsv", "repomap.meta.json"} {
		if _, err := os.Stat(filepath.Join(repo, ".nxy", "local", "index", f)); err != nil {
			t.Errorf("shared index file %s: %v", f, err)
		}
	}
	_, out, _ = runCmd(t, "", "locate", "--status", "--cwd", repo)
	if !strings.Contains(out, "repo map:     1 files, 1 symbols (built ") {
		t.Fatalf("status after index:\n%s", out)
	}

	// --json "q": the word after the flag is the question
	_, out, _ = runCmd(t, "", "locate", "--json", "cancelBooking", "--cwd", repo)
	var j struct {
		OK        bool     `json:"ok"`
		Mode      string   `json:"mode"`
		Question  string   `json:"question"`
		Terms     []string `json:"terms"`
		Degraded  []string `json:"degraded"`
		Codegraph string   `json:"codegraph"`
		Hits      []struct {
			Path   string `json:"path"`
			Line   int    `json:"line"`
			Source string `json:"source"`
		} `json:"hits"`
	}
	if err := json.Unmarshal([]byte(out), &j); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if !j.OK || j.Mode != "search" || j.Question != "cancelBooking" || j.Codegraph != "disabled" ||
		len(j.Hits) == 0 || j.Hits[0].Path != "src/a.js" || j.Hits[0].Line != 1 || j.Hits[0].Source != "index" {
		t.Fatalf("json = %s", out)
	}

	// relations with nothing installed still answer, and say what degraded
	_, out, _ = runCmd(t, "", "locate", "--callees", "cancelBooking", "--cwd", repo)
	if !strings.Contains(out, "## definition (repo map)") || !strings.Contains(out, "src/a.js:1\tfunction\tcancelBooking") {
		t.Fatalf("callees:\n%s", out)
	}

	// no question
	code, out, _ = runCmd(t, "", "locate", "--cwd", repo)
	if code != 0 || !strings.HasPrefix(out, "Usage: nxy locate") {
		t.Fatalf("usage (%d): %s", code, out)
	}
	_, out, _ = runCmd(t, "", "locate", "--json", "--cwd", repo)
	if strings.TrimSpace(out) != `{"ok":false,"reason":"no question"}` {
		t.Fatalf("no question json: %s", out)
	}
}
