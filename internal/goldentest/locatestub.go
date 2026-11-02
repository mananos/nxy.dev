package goldentest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// LocateStubs builds scripts/golden/locatestub and returns a directory (removed with the test) that
// holds it as `rg` and as `codegraph`. Answers come from the JSON table in $NXY_GOLDEN_LOCATE_TABLE.
func LocateStubs(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	rg := filepath.Join(dir, "rg"+ext)
	if out, err := exec.Command("go", "build", "-o", rg, "github.com/mananos/nxy.dev/scripts/golden/locatestub").CombinedOutput(); err != nil {
		t.Fatalf("go build locatestub: %v: %s", err, out)
	}
	data, err := os.ReadFile(rg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "codegraph"+ext), data, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}
