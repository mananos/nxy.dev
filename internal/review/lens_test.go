package review

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

func names(ls []Lens) []string {
	out := []string{}
	for _, l := range ls {
		out = append(out, l.Name)
	}
	return out
}

func TestMergeThreeLayers(t *testing.T) {
	mk := func(name, title string, enabled bool) Lens { return Lens{Name: name, Title: title, Enabled: enabled} }
	builtin := []Lens{mk("a", "built", true), mk("b", "built", true), mk("c", "built", true)}
	user := []Lens{mk("b", "user", true), mk("d", "user", true)}
	repo := []Lens{mk("a", "repo", false), mk("d", "repo", true), mk("e", "repo", true)}
	got := Merge(builtin, user, repo)
	if want := []string{"b", "c", "d", "e"}; !reflect.DeepEqual(names(got), want) {
		t.Fatalf("got %v, want %v", names(got), want)
	}
	titles := map[string]string{}
	for _, l := range got {
		titles[l.Name] = l.Title
	}
	if titles["b"] != "user" || titles["d"] != "repo" || titles["c"] != "built" {
		t.Fatalf("layers did not stack: %v", titles)
	}
}

func TestParseLens(t *testing.T) {
	if _, ok := ParseLens("- just a checklist\n", "x"); ok {
		t.Fatal("no frontmatter must not parse")
	}
	l, ok := ParseLens("---\r\nname: mine\r\npaths: **/x/**, *.go\r\nalways: true\r\n---\r\n- body\r\n", "fallback")
	if !ok || l.Name != "mine" || !l.Always || !l.Enabled || len(l.Paths) != 2 || l.Body != "- body" {
		t.Fatalf("got %+v ok=%v", l, ok)
	}
	l, _ = ParseLens("---\nenabled: false\n---\nbody", "fb")
	if l.Name != "fb" || l.Title != "fb" || l.Enabled {
		t.Fatalf("fallback/enabled: %+v", l)
	}
}

func TestBuiltin(t *testing.T) {
	got := names(Builtin())
	want := []string{"api", "base", "contract", "database", "frontend", "persistence", "security"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSelectCross(t *testing.T) {
	api := []File{{Path: "api/src/x.go"}}
	web := []File{{Path: "web/src/y.js"}}
	if sel := Select(Builtin(), []File{{Path: "internal/foo/x.go", Content: "x"}}); len(sel) != 1 || sel[0].Lens.Name != "base" {
		t.Fatalf("Select must skip the contract lens: %+v", sel)
	}
	if sel := SelectCross(Builtin(), [][]File{api}); len(sel) != 0 {
		t.Fatalf("one repo: %+v", sel)
	}
	if sel := SelectCross(Builtin(), [][]File{api, {{Path: "web/README.md"}}}); len(sel) != 0 {
		t.Fatalf("docs in the second repo: %+v", sel)
	}
	sel := SelectCross(Builtin(), [][]File{api, web})
	if len(sel) != 1 || sel[0].Lens.Name != "contract" || len(sel[0].Files) != 2 {
		t.Fatalf("two repos: %+v", sel)
	}
	if r, _ := Raise(domain.RiskLow, sel); r != domain.RiskMedium {
		t.Fatalf("contract must raise to medium, got %s", r)
	}
}

func TestSelect(t *testing.T) {
	sel := Select(Builtin(), []File{{Path: "README.md", Content: "password"}})
	if len(sel) != 1 || sel[0].Lens.Name != "base" {
		t.Fatalf("docs alone must select only base, got %+v", sel)
	}
	sel = Select(Builtin(), []File{{Path: "internal/auth/x.go", Content: "package auth"}})
	if len(sel) != 2 || sel[1].Lens.Name != "security" || sel[1].Files[0] != "internal/auth/x.go" {
		t.Fatalf("got %+v", sel)
	}
}

func TestRaiseNeverLowers(t *testing.T) {
	all := Builtin()
	pick := func(paths ...string) []Selected {
		var fs []File
		for _, p := range paths {
			fs = append(fs, File{Path: p, Content: "x"})
		}
		return Select(all, fs)
	}
	cases := []struct {
		name    string
		planned domain.Risk
		sel     []Selected
		want    domain.Risk
		why     string
	}{
		{"plain stays low", domain.RiskLow, pick("internal/foo/bar.go"), domain.RiskLow, ""},
		{"api alone is medium", domain.RiskLow, pick("src/UserController.java"), domain.RiskMedium, "lente api: src/UserController.java"},
		{"security is high", domain.RiskLow, pick("internal/auth/x.go"), domain.RiskHigh, "lente security: internal/auth/x.go"},
		{"migration is high", domain.RiskMedium, pick("db/migrations/1.sql"), domain.RiskHigh, "lente database"},
		{"never lowers high", domain.RiskHigh, pick("src/UserController.java"), domain.RiskHigh, ""},
		{"never lowers medium", domain.RiskMedium, pick("internal/foo/bar.go"), domain.RiskMedium, ""},
	}
	for _, c := range cases {
		got, why := Raise(c.planned, c.sel)
		if got != c.want || !strings.HasPrefix(why, c.why) {
			t.Errorf("%s: got %s %q, want %s %q", c.name, got, why, c.want, c.why)
		}
	}
	if Max(domain.RiskLow, domain.RiskHigh, domain.RiskMedium) != domain.RiskHigh || Max() != domain.RiskLow {
		t.Fatal("Max")
	}
}

// Until F6 the embedded lenses and the 1.x ones must stay identical.
func TestBuiltinMatchesLegacyLenses(t *testing.T) {
	legacy, err := filepath.Glob(filepath.Join("..", "..", "lenses", "*.md"))
	if err != nil || len(legacy) == 0 {
		t.Skip("no 1.x lenses directory")
	}
	for _, p := range legacy {
		want, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		got, err := builtinFS.ReadFile("lenses/" + filepath.Base(p))
		if err != nil {
			t.Fatalf("%s is not embedded", filepath.Base(p))
		}
		if string(got) != string(want) {
			t.Errorf("%s differs from lenses/%s", filepath.Base(p), filepath.Base(p))
		}
	}
}
