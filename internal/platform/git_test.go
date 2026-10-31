package platform

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const sha1 = "0123456789abcdef0123456789abcdef01234567"
const sha2 = "89abcdef0123456789abcdef0123456789abcdef"

func TestGitHeadLooseRef(t *testing.T) {
	d := t.TempDir()
	writeFile(t, filepath.Join(d, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(d, ".git", "refs", "heads", "main"), sha1+"\n")
	if got := GitHead(d); got != sha1 {
		t.Fatalf("got %q", got)
	}
}

func TestGitHeadPackedRefs(t *testing.T) {
	d := t.TempDir()
	writeFile(t, filepath.Join(d, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(d, ".git", "packed-refs"), "# pack-refs\n"+sha2+" refs/heads/other\n"+sha1+" refs/heads/main\n")
	if got := GitHead(d); got != sha1 {
		t.Fatalf("got %q", got)
	}
}

func TestGitHeadDetached(t *testing.T) {
	d := t.TempDir()
	writeFile(t, filepath.Join(d, ".git", "HEAD"), sha2+"\n")
	if got := GitHead(d); got != sha2 {
		t.Fatalf("got %q", got)
	}
}

func TestGitHeadWorktree(t *testing.T) {
	d := t.TempDir()
	main := filepath.Join(d, "main", ".git")
	wt := filepath.Join(main, "worktrees", "w")
	writeFile(t, filepath.Join(wt, "HEAD"), "ref: refs/heads/feat\n")
	writeFile(t, filepath.Join(wt, "commondir"), "../..\n")
	writeFile(t, filepath.Join(main, "refs", "heads", "feat"), sha1+"\n")
	work := filepath.Join(d, "work")
	writeFile(t, filepath.Join(work, ".git"), "gitdir: "+filepath.ToSlash(wt)+"\n")
	if got := GitHead(work); got != sha1 {
		t.Fatalf("got %q", got)
	}
}

func TestGitHeadUnbornAndNoRepo(t *testing.T) {
	d := t.TempDir()
	if got := GitHead(d); got != "" {
		t.Fatalf("no repo: got %q", got)
	}
	writeFile(t, filepath.Join(d, ".git", "HEAD"), "ref: refs/heads/main\n")
	if got := GitHead(d); got != "" {
		t.Fatalf("unborn: got %q", got)
	}
}

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	d := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = d
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.name", "t")
	run("config", "user.email", "t@example.com")
	run("config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(d, "a.txt"), "a")
	writeFile(t, filepath.Join(d, "sub", "b.txt"), "b")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	return d
}

func TestGitChangedAndFiles(t *testing.T) {
	d := gitRepo(t)
	base := GitHead(d)
	if base == "" {
		t.Fatal("empty head")
	}
	writeFile(t, filepath.Join(d, "a.txt"), "changed")
	writeFile(t, filepath.Join(d, "new.txt"), "n")
	writeFile(t, filepath.Join(d, ".nxy", "x.json"), "{}")
	writeFile(t, filepath.Join(d, ".codegraph", "db"), "x")
	got, err := GitChanged(context.Background(), d, base)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	if want := []string{"a.txt", "new.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("changed %v want %v", got, want)
	}
	// from a subdir, paths are relative to it
	sub, err := GitChanged(context.Background(), filepath.Join(d, "sub"), base)
	if err != nil {
		t.Fatal(err)
	}
	if len(sub) != 0 {
		t.Fatalf("sub changed %v", sub)
	}
	files, err := GitFiles(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	found := strings.Join(files, ",")
	for _, w := range []string{"a.txt", "new.txt", "sub/b.txt"} {
		if !strings.Contains(found, w) {
			t.Fatalf("files %v missing %s", files, w)
		}
	}
}

func TestGitChangedSkipsDeleted(t *testing.T) {
	d := gitRepo(t)
	base := GitHead(d)
	if err := os.Remove(filepath.Join(d, "sub", "b.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(d, "a.txt"), "changed")
	got, err := GitChanged(context.Background(), d, base)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("changed %v want %v", got, want)
	}
}

func TestGitExcludeLocalOtherPattern(t *testing.T) {
	d := gitRepo(t)
	ctx := context.Background()
	if err := GitExcludeLocal(ctx, d, "/.codegraph/"); err != nil {
		t.Fatal(err)
	}
	if err := GitExcludeLocal(ctx, d, "/.other/"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(d, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "/.other/") {
		t.Fatalf("other pattern not added:\n%s", b)
	}
}

func TestGitExcludeLocal(t *testing.T) {
	d := gitRepo(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ { // second call is a no-op
		if err := GitExcludeLocal(ctx, d, "/.codegraph/"); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join(d, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "/.codegraph/"); n != 1 {
		t.Fatalf("pattern appears %d times:\n%s", n, b)
	}
}
