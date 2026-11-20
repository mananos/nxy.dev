package setup

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPathPlanLinux(t *testing.T) {
	home := t.TempDir()
	shim := filepath.ToSlash(filepath.Join(home, "it's", ".nxy", "bin", "nxy.cmd"))
	p := PathPlan(PathOpts{Platform: "linux", ShimPath: shim, Home: filepath.ToSlash(home)})
	if p.Tool != "path" || len(p.Steps) != 1 || p.Steps[0].File != "sh" || p.Steps[0].Display != p.Steps[0].Args[1] {
		t.Fatalf("plan = %+v", p)
	}
	if !strings.Contains(p.Steps[0].Display, "chmod 755") {
		t.Errorf("no chmod 755: %s", p.Steps[0].Display)
	}
	if runtime.GOOS == "windows" {
		return
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	if out, err := exec.Command(sh, "-c", p.Steps[0].Args[1]).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	target := filepath.Join(home, ".local", "bin", "nxy")
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	want := "#!/bin/sh\nexec sh '" + strings.ReplaceAll(shim, "'", `'\''`) + "' \"$@\"\n"
	if string(b) != want {
		t.Errorf("script = %q, want %q", b, want)
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
}

func TestPathPlanWindows(t *testing.T) {
	p := PathPlan(PathOpts{Platform: "win32", ShimPath: `C:\Users\test\.nxy\bin\nxy.cmd`, Home: `C:\Users\test`})
	if len(p.Steps) != 1 || p.Steps[0].File != "powershell" || len(p.Steps[0].Args) != 3 {
		t.Fatalf("plan = %+v", p)
	}
	s := p.Steps[0]
	for _, want := range []string{"DoNotExpandEnvironmentNames", "ExpandString", "SetEnvironmentVariable", `'C:\Users\test\.nxy\bin'`} {
		if !strings.Contains(s.Args[2], want) {
			t.Errorf("command lacks %q: %s", want, s.Args[2])
		}
	}
	// pasteable in PowerShell as is: single quotes, the inner ones doubled
	if s.Display != "powershell -NoProfile -Command "+psq(s.Args[2]) || strings.Contains(s.Display, `"`) {
		t.Errorf("display is not what runs: %s", s.Display)
	}
}
