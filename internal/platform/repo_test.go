package platform

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func originConfig(url string) string {
	return "[core]\n\tbare = false\n[remote \"origin\"]\n\turl = " + url + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
}

func TestRepoKeyRemotes(t *testing.T) {
	tests := []struct{ name, config, want string }{
		{"https with user and .git", originConfig("https://user@GitHub.com/Owner/repo.git"), "github.com/Owner/repo"},
		{"https plain", originConfig("https://github.com/owner/repo"), "github.com/owner/repo"},
		{"ssh scp-like", originConfig("git@github.com:owner/repo.git"), "github.com/owner/repo"},
		{"ssh url with port", originConfig("ssh://git@host.example:22/owner/repo.git"), "host.example/owner/repo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, ".git", "config"), tt.config)
			sub := filepath.Join(root, "a", "b")
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			if got := RepoKey(sub); got != tt.want {
				t.Fatalf("RepoKey = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRepoKeyNoRemote(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".git", "config"), "[core]\n\tbare = false\n")
	got := RepoKey(filepath.Join(root))
	if strings.Contains(got, `\`) || got != filepath.ToSlash(root) {
		t.Fatalf("RepoKey = %q, want %q", got, filepath.ToSlash(root))
	}
}

func TestRepoKeyNoRepo(t *testing.T) {
	dir := t.TempDir()
	got := RepoKey(dir)
	if strings.Contains(got, `\`) || got != filepath.ToSlash(dir) {
		t.Fatalf("RepoKey = %q, want %q", got, filepath.ToSlash(dir))
	}
}

func TestRepoKeyWorktree(t *testing.T) {
	main := t.TempDir()
	writeFile(t, filepath.Join(main, ".git", "config"), originConfig("git@github.com:owner/repo.git"))
	wtGit := filepath.Join(main, ".git", "worktrees", "wt")
	writeFile(t, filepath.Join(wtGit, "commondir"), "../..\n")
	wt := t.TempDir()
	writeFile(t, filepath.Join(wt, ".git"), "gitdir: "+filepath.ToSlash(wtGit)+"\n")
	if got, want := RepoKey(wt), "github.com/owner/repo"; got != want {
		t.Fatalf("RepoKey = %q, want %q", got, want)
	}
}

func TestOpenCommand(t *testing.T) {
	tests := []struct {
		goos string
		want []string
	}{
		{"windows", []string{"rundll32", "url.dll,FileProtocolHandler", "p.md"}},
		{"darwin", []string{"open", "p.md"}},
		{"linux", []string{"xdg-open", "p.md"}},
		{"freebsd", []string{"xdg-open", "p.md"}},
	}
	for _, tt := range tests {
		if got := openCommand(tt.goos, "p.md"); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("openCommand(%s) = %v, want %v", tt.goos, got, tt.want)
		}
	}
}
