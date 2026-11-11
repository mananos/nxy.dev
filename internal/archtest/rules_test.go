package archtest

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"testing"
)

func m(s string) string { return Module + "/" + s }

func TestCheck(t *testing.T) {
	cases := []struct {
		name string
		pkg  Pkg
		bad  bool
	}{
		{"pure imports database/sql", Pkg{ImportPath: m("internal/domain"), Imports: []string{"database/sql"}, Deps: []string{"database/sql"}}, true},
		{"pure imports os/exec transitively", Pkg{ImportPath: m("internal/flow"), Imports: []string{m("internal/plan")}, Deps: []string{m("internal/plan"), "os/exec"}}, true},
		{"pure imports adapter", Pkg{ImportPath: m("internal/gate"), Imports: []string{m("internal/tools")}, Deps: []string{m("internal/tools")}}, true},
		{"pure imports third party", Pkg{ImportPath: m("internal/filter"), Imports: []string{"example.com/x"}, Deps: []string{"example.com/x"}}, true},
		{"pure imports pure and stdlib", Pkg{ImportPath: m("internal/flow"), Imports: []string{m("internal/domain"), "strings"}, Deps: []string{m("internal/domain"), "strings"}}, false},
		{"memory imports host/claudecode/transcript", Pkg{ImportPath: m("internal/memory"), Imports: []string{m("internal/host/claudecode/transcript")}, Deps: []string{m("internal/host/claudecode/transcript")}}, true},
		{"memory imports domain and strings", Pkg{ImportPath: m("internal/memory"), Imports: []string{m("internal/domain"), "strings"}, Deps: []string{m("internal/domain"), "strings"}}, false},
		{"app imports store/sqlite", Pkg{ImportPath: m("internal/app"), Imports: []string{m("internal/store/sqlite")}}, true},
		{"app imports platform", Pkg{ImportPath: m("internal/app"), Imports: []string{m("internal/platform")}}, true},
		{"app depends on net", Pkg{ImportPath: m("internal/app"), Deps: []string{"net/http"}}, true},
		{"app imports domain", Pkg{ImportPath: m("internal/app"), Imports: []string{m("internal/domain"), "context"}, Deps: []string{m("internal/domain"), "context"}}, false},
		{"host imports runner", Pkg{ImportPath: m("internal/host/claudecode"), Imports: []string{m("internal/runner")}}, true},
		{"host imports app and platform", Pkg{ImportPath: m("internal/host/claudecode"), Imports: []string{m("internal/app"), m("internal/platform")}}, false},
		{"platform imports tui", Pkg{ImportPath: m("internal/platform"), Imports: []string{m("internal/tui")}}, true},
		{"tui imports platform", Pkg{ImportPath: m("internal/tui"), Imports: []string{m("internal/platform")}}, true},
		{"tui imports host/claudecode/transcript", Pkg{ImportPath: m("internal/tui"), Imports: []string{m("internal/host/claudecode/transcript")}}, true},
		{"views imports ask", Pkg{ImportPath: m("internal/views"), Imports: []string{m("internal/ask")}}, false},
		{"ask imports an adapter", Pkg{ImportPath: m("internal/ask"), Deps: []string{m("internal/store/sqlite")}}, true},
		{"tui imports views and format", Pkg{ImportPath: m("internal/tui"), Imports: []string{m("internal/views"), m("internal/format"), "strings"}}, false},
		{"cmd/nxy imports everything", Pkg{ImportPath: m("cmd/nxy"), Imports: []string{
			m("internal/store/sqlite"), m("internal/host/claudecode"), m("internal/runner"),
			m("internal/tools"), m("internal/update"), m("internal/tui"), m("internal/app"), m("internal/platform"),
		}}, false},
	}
	for _, c := range cases {
		got := Check([]Pkg{c.pkg})
		if (len(got) > 0) != c.bad {
			t.Errorf("%s: violations=%v, want bad=%v", c.name, got, c.bad)
		}
	}
}

func TestRepoImports(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "-json", "./...")
	cmd.Dir = "../.."
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, stderr.String())
	}
	var pkgs []Pkg
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var p Pkg
		if err := dec.Decode(&p); err != nil {
			t.Fatal(err)
		}
		pkgs = append(pkgs, p)
	}
	for _, v := range Check(pkgs) {
		t.Error(v.String())
	}
}
