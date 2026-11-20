package tools

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/setup"
)

func TestKnownRgLocations(t *testing.T) {
	get := func(string) string { return "" }
	got := KnownRgLocations("linux", "/home/dev", get)
	want := []string{"/usr/bin/rg", "/usr/local/bin/rg", "/opt/homebrew/bin/rg",
		filepath.Join("/home/dev", ".cargo", "bin", "rg"), filepath.Join("/home/dev", ".local", "bin", "rg")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("linux = %v", got)
	}

	home := t.TempDir()
	local := filepath.Join(home, "Local")
	pkg := filepath.Join(local, "Microsoft", "WinGet", "Packages", "BurntSushi.ripgrep.MSVC_x")
	if err := os.MkdirAll(filepath.Join(pkg, "ripgrep-14.1-x86_64"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(local, "Microsoft", "WinGet", "Packages", "Other.pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	win := KnownRgLocations("windows", home, func(k string) string {
		if k == "LOCALAPPDATA" {
			return local
		}
		return ""
	})
	wantWin := []string{
		filepath.Join(local, "Microsoft", "WinGet", "Links", "rg.exe"),
		filepath.Join(pkg, "rg.exe"),
		filepath.Join(pkg, "ripgrep-14.1-x86_64", "rg.exe"),
		filepath.Join(home, ".cargo", "bin", "rg.exe"),
		filepath.Join(home, "scoop", "shims", "rg.exe"),
	}
	if !reflect.DeepEqual(win, wantWin) {
		t.Errorf("windows = %v", win)
	}
}

func TestFindRgNoneAndKnownLocation(t *testing.T) {
	home, getenv := isolate(t)
	if p, ok := FindRg(getenv); ok && runtime.GOOS == "windows" {
		t.Fatalf("found %q with an empty PATH and a temp home", p)
	}
	stub := buildStub(t)
	dir := filepath.Join(home, ".cargo", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "rg"
	if runtime.GOOS == "windows" {
		name = "rg.exe"
	}
	data, _ := os.ReadFile(stub)
	dst := filepath.Join(dir, name)
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := FindRg(getenv)
	if !ok {
		t.Fatal("rg in ~/.cargo/bin not found")
	}
	// a machine that has rg in a system location finds that one first
	if got != dst && runtime.GOOS == "windows" {
		t.Errorf("FindRg = %q, want %q", got, dst)
	}
	onPath, at := DetectRg(getenv)
	if runtime.GOOS == "windows" && (onPath || at != dst) {
		t.Errorf("DetectRg = %v %q, want false %q", onPath, at, dst)
	}
}

func TestRunPlanStopsAtFirstFailure(t *testing.T) {
	plan := setup.Plan{
		Steps: []setup.Step{
			{Display: "go version", File: "go", Args: []string{"version"}},
			{Display: "go nosuchsubcommand", File: "go", Args: []string{"nosuchsubcommand"}},
			{Display: "never", File: "go", Args: []string{"version"}},
		},
		Manual: []string{},
	}
	r := RunPlan(plan)
	if r.OK || len(r.Ran) != 2 || r.FailedAt != "go nosuchsubcommand" || r.Tail == "" {
		t.Fatalf("result = %+v", r)
	}
	missing := RunPlan(setup.Plan{Steps: []setup.Step{{Display: "nope", File: "nxy-no-such-binary-xyz"}}})
	if missing.OK || missing.FailedAt != "nope" || missing.Tail == "" {
		t.Fatalf("missing binary = %+v", missing)
	}
	if !RunPlan(setup.Plan{Steps: []setup.Step{plan.Steps[0]}}).OK {
		t.Fatal("go version should pass")
	}
}
