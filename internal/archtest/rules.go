// Package archtest checks the import rules of the module (roadmap 14.19).
package archtest

import (
	"fmt"
	"strings"
)

// Module is the module path all internal packages live under.
const Module = "github.com/mananos/nxy.dev"

// Pkg is the subset of `go list -deps -json` output the rules need.
type Pkg struct {
	ImportPath string
	Standard   bool
	Imports    []string
	Deps       []string
}

// Violation is one broken rule.
type Violation struct {
	Pkg    string
	Import string
	Rule   string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s imports %s: %s", v.Pkg, v.Import, v.Rule)
}

// Rules as data: paths are relative to the module.
var (
	pure = set("internal/domain", "internal/flow", "internal/plan", "internal/review", "internal/gate", "internal/filter", "internal/version", "internal/affected", "internal/design", "internal/pricing", "internal/jscompat", "internal/metrics", "internal/format", "internal/report", "internal/promptcache", "internal/searchscope", "internal/textdiff", "internal/repomap", "internal/setup", "internal/configedit", "internal/scout", "internal/worktree", "internal/repos", "internal/docs", "internal/ask", "internal/memory", "internal/legacy", "internal/host/claudecode/transcript")
	// agnostic packages: pure and also free of any host package.
	agnostic = set("internal/memory")
	adapters = set("internal/store/sqlite", "internal/host/claudecode", "internal/runner", "internal/tools", "internal/update", "internal/tui")
	// support packages: may not import adapters, may only be imported by adapters and cmd.
	support = set("internal/platform", "internal/views")
	// viewsOnly: inside the module these may import internal/views and pure non-host packages only.
	viewsOnly = set("internal/tui")
	// forbidden for pure packages and app, anywhere in their dependency tree.
	forbiddenStd = []string{"os/exec", "net", "net/http", "database/sql"}
)

func set(items ...string) map[string]bool {
	m := map[string]bool{}
	for _, i := range items {
		m[i] = true
	}
	return m
}

// rel returns the module-relative path and whether p is inside the module.
func rel(p string) (string, bool) {
	if p == Module {
		return "", true
	}
	if strings.HasPrefix(p, Module+"/") {
		return strings.TrimPrefix(p, Module+"/"), true
	}
	return p, false
}

func isForbiddenStd(p string) bool {
	for _, f := range forbiddenStd {
		if p == f {
			return true
		}
	}
	return false
}

// isStd: standard library paths have no dot in the first element.
func isStd(p string) bool {
	first, _, _ := strings.Cut(p, "/")
	return !strings.Contains(first, ".")
}

// Check applies the rules to the packages and returns every violation.
func Check(pkgs []Pkg) []Violation {
	var out []Violation
	for _, p := range pkgs {
		r, ok := rel(p.ImportPath)
		if !ok {
			continue
		}
		switch {
		case pure[r]:
			out = append(out, checkPure(p)...)
			if agnostic[r] {
				out = append(out, checkAgnostic(p)...)
			}
		case r == "internal/app":
			out = append(out, checkApp(p)...)
		case adapters[r] || support[r]:
			out = append(out, checkAdapter(p, r)...)
		}
		if viewsOnly[r] {
			out = append(out, checkViewsOnly(p)...)
		}
	}
	return out
}

// checkViewsOnly looks at direct imports: views, or a pure package that is not a host one.
func checkViewsOnly(p Pkg) []Violation {
	var out []Violation
	for _, d := range p.Imports {
		dr, in := rel(d)
		if !in || dr == "internal/views" {
			continue
		}
		if !pure[dr] || strings.HasPrefix(dr, "internal/host/") {
			out = append(out, Violation{p.ImportPath, d, "tui may only import views and pure non-host packages"})
		}
	}
	return out
}

func checkPure(p Pkg) []Violation {
	var out []Violation
	for _, d := range p.Deps {
		if d == p.ImportPath {
			continue
		}
		if isForbiddenStd(d) {
			out = append(out, Violation{p.ImportPath, d, "pure package depends on I/O"})
			continue
		}
		dr, in := rel(d)
		switch {
		case in && !pure[dr]:
			out = append(out, Violation{p.ImportPath, d, "pure package may only depend on pure packages"})
		case !in && !isStd(d):
			out = append(out, Violation{p.ImportPath, d, "pure package may only depend on the standard library"})
		}
	}
	return out
}

func checkAgnostic(p Pkg) []Violation {
	var out []Violation
	for _, d := range p.Deps {
		if dr, in := rel(d); in && strings.HasPrefix(dr, "internal/host/") {
			out = append(out, Violation{p.ImportPath, d, "host-agnostic package may not depend on a host"})
		}
	}
	return out
}

func checkApp(p Pkg) []Violation {
	var out []Violation
	for _, d := range p.Deps {
		if isForbiddenStd(d) {
			out = append(out, Violation{p.ImportPath, d, "app depends on I/O"})
		}
	}
	for _, d := range p.Imports {
		dr, in := rel(d)
		switch {
		case in && !pure[dr]:
			out = append(out, Violation{p.ImportPath, d, "app may only import pure packages"})
		case !in && !isStd(d):
			out = append(out, Violation{p.ImportPath, d, "app may only import the standard library"})
		}
	}
	return out
}

func checkAdapter(p Pkg, self string) []Violation {
	var out []Violation
	for _, d := range p.Imports {
		dr, in := rel(d)
		if in && adapters[dr] && dr != self {
			out = append(out, Violation{p.ImportPath, d, "only cmd/nxy may wire adapters"})
		}
	}
	return out
}
