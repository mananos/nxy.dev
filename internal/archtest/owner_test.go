package archtest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func readRepoFile(t *testing.T, rel ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, rel...)...))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// The module path, the GoReleaser release target and the shim's default
// download URLs must all name the same GitHub owner/repo. The module path may
// carry a major-version suffix (/v2), which is not part of the repo name.
func TestReleaseOwnerConsistent(t *testing.T) {
	mod := regexp.MustCompile(`(?m)^module github\.com/([^/\s]+/[^/\s]+)(?:/v[0-9]+)?\s*$`).FindStringSubmatch(readRepoFile(t, "go.mod"))
	if mod == nil {
		t.Fatal("go.mod: module path not found")
	}
	want := mod[1]

	gr := readRepoFile(t, ".goreleaser.yaml")
	owner := regexp.MustCompile(`(?m)^\s*owner:\s*(\S+)`).FindStringSubmatch(gr)
	name := regexp.MustCompile(`(?m)^\s*name:\s*(\S+)\s*$`).FindAllStringSubmatch(gr, -1)
	if owner == nil || len(name) == 0 {
		t.Fatal(".goreleaser.yaml: release owner/name not found")
	}
	if got := owner[1] + "/" + name[len(name)-1][1]; got != want {
		t.Errorf(".goreleaser.yaml release is %q, go.mod says %q", got, want)
	}

	urls := regexp.MustCompile(`https://github\.com/([^/\s]+/[^/\s]+)/releases/download/`).FindAllStringSubmatch(readRepoFile(t, "bin", "nxy.cmd"), -1)
	if len(urls) != 2 {
		t.Fatalf("bin/nxy.cmd: want 2 default release URLs (sh and batch), found %d", len(urls))
	}
	for _, u := range urls {
		if u[1] != want {
			t.Errorf("bin/nxy.cmd release URL names %q, go.mod says %q", u[1], want)
		}
	}
}
