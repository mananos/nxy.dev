package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, body := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFindDocsWalk(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"README.md":                  "intro\nUse ClienteService to fetch.\nand ClienteService again\n",
		"docs/guide.md":              "the ClienteService is described here\n",
		"docs/other.md":              "nothing\n",
		"src/notes.md":               "ClienteService (not a user doc)\n",
		"agents/planner.md":          "ClienteService in a prompt\n",
		"node_modules/pkg/README.md": "ClienteService vendored\n",
		"docs/big.md":                strings.Repeat("x", docsMaxBytes+1) + "\nClienteService\n",
		"CHANGELOG.md":               "ClienteService changed\n",
		"src/ClienteService.ts":      "export class ClienteService {}\n",
	})
	res := findDocs(context.Background(), root, []string{"src/ClienteService.ts", "CHANGELOG.md"}, "")
	if !reflect.DeepEqual(res.Terms, []string{"ClienteService"}) {
		t.Fatalf("terms = %v", res.Terms)
	}
	var got []string
	for _, d := range res.Docs {
		got = append(got, d.Path)
	}
	// README.md has two lines, so it goes first; CHANGELOG.md was changed by the plan itself.
	if want := []string{"README.md", "docs/guide.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("docs = %v, want %v", got, want)
	}
	if l := res.Docs[0].Lines; len(l) != 2 || l[0].Line != 2 || l[0].Text != "Use ClienteService to fetch." {
		t.Errorf("lines = %+v", l)
	}
}

func TestFindDocsNoTermsNoSearch(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"README.md": "index utils main\n"})
	res := findDocs(context.Background(), root, []string{"src/index.ts", "src/utils.ts", "x.go"}, "")
	if len(res.Terms) != 0 || len(res.Docs) != 0 {
		t.Errorf("res = %+v", res)
	}
}

func TestFindDocsWikiDirFromCI(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".github/workflows/wiki.yml": "name: wiki\nsteps:\n  - uses: x/wiki-action@v1\n    with:\n      path: handbook\n",
		"handbook/Home.md":           "the OrderController lives here\n",
		"misc/page.md":               "OrderController outside any docs folder\n",
	})
	res := findDocs(context.Background(), root, []string{"app/OrderController.java"}, "")
	if res.WikiDir != "handbook" {
		t.Fatalf("wikiDir = %q", res.WikiDir)
	}
	if len(res.Docs) != 1 || res.Docs[0].Path != "handbook/Home.md" {
		t.Errorf("docs = %+v", res.Docs)
	}
}

func TestFindDocsWithRgMatchesTheWalk(t *testing.T) {
	bin, ok := FindRg(os.Getenv)
	if !ok {
		t.Skip("rg is not installed")
	}
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"README.md":                  "Use ClienteService to fetch.\nand ClienteService again\n",
		"docs/guide.md":              "the ClienteService is described here\n",
		"src/notes.md":               "ClienteService (not a user doc)\n",
		"agents/planner.md":          "ClienteService in a prompt\n",
		"node_modules/pkg/README.md": "ClienteService vendored\n",
	})
	changed := []string{"src/ClienteService.ts"}
	withRg := findDocs(context.Background(), root, changed, bin)
	walk := findDocs(context.Background(), root, changed, "")
	if !reflect.DeepEqual(withRg, walk) {
		t.Errorf("rg: %+v\nwalk: %+v", withRg, walk)
	}
	if len(walk.Docs) != 2 {
		t.Errorf("docs = %+v", walk.Docs)
	}
}

func TestFindDocsSkipsGitignored(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	writeTree(t, root, map[string]string{
		".gitignore":            "docs/ignorado.md\n",
		"docs/ignorado.md":      "the ClienteService is ignored\n",
		"docs/guide.md":         "the ClienteService is described here\n",
		"src/ClienteService.ts": "export class ClienteService {}\n",
	})
	bins := []string{""}
	if bin, ok := FindRg(os.Getenv); ok {
		bins = append(bins, bin)
	}
	for _, bin := range bins {
		res := findDocs(context.Background(), root, []string{"src/ClienteService.ts"}, bin)
		var got []string
		for _, d := range res.Docs {
			got = append(got, d.Path)
		}
		if want := []string{"docs/guide.md"}; !reflect.DeepEqual(got, want) {
			t.Errorf("rg=%q docs = %v, want %v", bin, got, want)
		}
	}
}

func TestWalkMdStopsAtTheCap(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 3; i++ {
		writeTree(t, root, map[string]string{filepath.Join("d", string(rune('a'+i))+".md"): "x"})
	}
	var out []string
	walkMd(root, &out)
	if len(out) != 3 {
		t.Errorf("walked %d files", len(out))
	}
	out = make([]string, docsMaxFiles)
	walkMd(root, &out)
	if len(out) != docsMaxFiles {
		t.Errorf("the cap did not hold: %d", len(out))
	}
}
