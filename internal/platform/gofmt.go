package platform

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Gofmt lists the files (relative to dir) that `gofmt -l` would reformat. Files that
// no longer exist are skipped; without gofmt in the PATH it reports nothing.
func Gofmt(ctx context.Context, dir string, files []string) ([]string, error) {
	if _, err := exec.LookPath("gofmt"); err != nil {
		return nil, nil
	}
	argv := []string{"gofmt", "-l"}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err == nil {
			argv = append(argv, f)
		}
	}
	if len(argv) == 2 {
		return nil, nil
	}
	var out, errb bytes.Buffer
	exit, err := runProc(ctx, dir, argv, "", "", &out, &errb)
	if err != nil || exit != 0 {
		return nil, err
	}
	var bad []string
	for _, l := range strings.Split(out.String(), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			bad = append(bad, filepath.ToSlash(l))
		}
	}
	return bad, nil
}
