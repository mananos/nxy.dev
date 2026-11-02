package platform

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/internal/worktree"
)

// worktreeRepo makes <tmp>/repo with one commit on main plus the local branches dev and exist.
func worktreeRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	d := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = d
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.name", "t")
	run("config", "user.email", "t@example.com")
	run("config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(d, "a.txt"), "a")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	run("branch", "dev")
	run("branch", "exist")
	return d
}

func newName(s string) *string { return &s }

func TestWorktreeNewBaseListPauseClose(t *testing.T) {
	repo := worktreeRepo(t)
	ctx := context.Background()
	added, err := AddWorktree(ctx, repo, worktree.NamesInput{Name: newName("Login Google"), Base: "dev", Issue: " #14 "}, worktree.Posix)
	if err != nil {
		t.Fatal(err)
	}
	if added.Branch != "feature/#14_login_google" || !strings.HasSuffix(added.Path, "/repo.worktrees/14_login_google") {
		t.Fatalf("added %+v", added)
	}
	if !strings.HasPrefix(added.Command, "git worktree add -b ") || !strings.HasSuffix(added.Command, " dev") {
		t.Fatalf("command %q", added.Command)
	}
	if _, err := os.Stat(NativePath(added.Path)); err != nil {
		t.Fatalf("folder not created: %v", err)
	}

	l, err := ListWorktrees(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Rows) != 2 || !l.Rows[0].Main || !l.Rows[0].Current || l.Rows[1].Current || l.Rows[1].Branch != added.Branch {
		t.Fatalf("rows %+v", l.Rows)
	}
	if l.Rows[1].Dirty == nil || *l.Rows[1].Dirty != 0 || l.Rows[1].PausedAt != 0 {
		t.Fatalf("row 1 %+v", l.Rows[1])
	}
	if l.DefaultBranch != "main" || l.Bases[0] != "main" {
		t.Fatalf("bases %v default %q", l.Bases, l.DefaultBranch)
	}
	if len(l.Branches.Busy) != 2 || strings.Join(l.Branches.Free, ",") != "dev,exist" && strings.Join(l.Branches.Free, ",") != "exist,dev" {
		t.Fatalf("branches %+v", l.Branches)
	}

	now := time.UnixMilli(1700000000000)
	if err := SetPaused(added.Path, true, now); err != nil {
		t.Fatal(err)
	}
	m, ok := Paused(added.Path)
	if !ok || m.At != 1700000000000 || m.Branch != added.Branch {
		t.Fatalf("mark %+v %v", m, ok)
	}
	if l, _ = ListWorktrees(ctx, repo); l.Rows[1].PausedAt != 1700000000000 {
		t.Fatalf("list does not show the pause: %+v", l.Rows[1])
	}
	if l.Rows[1].Dirty == nil || *l.Rows[1].Dirty != 0 {
		t.Fatalf("the pause mark must not count as the user's changes: %+v", l.Rows[1])
	}
	if err := SetPaused(added.Path, false, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := Paused(added.Path); ok {
		t.Fatal("resume must delete the mark")
	}
	if err := SetPaused(added.Path, false, now); err != nil {
		t.Fatalf("resuming twice: %v", err)
	}

	// closing a paused worktree takes the mark with it
	if err := SetPaused(added.Path, true, now); err != nil {
		t.Fatal(err)
	}
	cmd, err := RemoveWorktree(ctx, repo, added.Path, false, worktree.Posix)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cmd, "git worktree remove ") || strings.Contains(cmd, "--force") {
		t.Fatalf("command %q", cmd)
	}
	if _, err := os.Stat(NativePath(added.Path)); !os.IsNotExist(err) {
		t.Fatalf("folder still there: %v", err)
	}
	if !hasBranch(ctx, repo, added.Branch) {
		t.Fatal("the branch must stay")
	}
}

func TestWorktreeExistingBranch(t *testing.T) {
	repo := worktreeRepo(t)
	ctx := context.Background()
	added, err := AddWorktree(ctx, repo, worktree.NamesInput{Branch: newName("exist")}, worktree.Posix)
	if err != nil {
		t.Fatal(err)
	}
	if added.Branch != "exist" || !strings.HasSuffix(added.Path, "/repo.worktrees/exist") || strings.Contains(added.Command, " -b ") {
		t.Fatalf("added %+v", added)
	}
	if _, err := AddWorktree(ctx, repo, worktree.NamesInput{Branch: newName("exist")}, worktree.Posix); err == nil {
		t.Fatal("opening the same branch twice must fail")
	}
}

func TestWorktreeAddRefusals(t *testing.T) {
	repo := worktreeRepo(t)
	ctx := context.Background()
	for _, c := range []struct {
		in   worktree.NamesInput
		want string
	}{
		{worktree.NamesInput{Name: newName("x"), Base: "nope"}, "la base nope no existe como rama local"},
		{worktree.NamesInput{Name: newName("x")}, "elegí la rama base"},
		{worktree.NamesInput{Name: newName("x"), Base: "main", Issue: "abc"}, "solo dígitos"},
		{worktree.NamesInput{Branch: newName("ghost")}, "la rama ghost no existe como rama local"},
		{worktree.NamesInput{Branch: newName("main")}, "la rama main ya está abierta en"},
		{worktree.NamesInput{Name: newName("???"), Base: "main"}, "poné un nombre"},
	} {
		if _, err := AddWorktree(ctx, repo, c.in, worktree.Posix); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: got %v, want %q", c.in, err, c.want)
		}
	}
	if _, err := AddWorktree(ctx, repo, worktree.NamesInput{Name: newName("x"), Base: "main"}, worktree.Posix); err != nil {
		t.Fatal(err)
	}
	if _, err := AddWorktree(ctx, repo, worktree.NamesInput{Name: newName("x"), Base: "main"}, worktree.Posix); err == nil || !strings.Contains(err.Error(), "ya existe") {
		t.Errorf("same name twice: %v", err)
	}
	if _, err := AddWorktree(ctx, t.TempDir(), worktree.NamesInput{Name: newName("x"), Base: "main"}, worktree.Posix); err == nil {
		t.Error("outside a repo must fail")
	}
}

func TestWorktreeCloseRefusals(t *testing.T) {
	repo := worktreeRepo(t)
	ctx := context.Background()
	added, err := AddWorktree(ctx, repo, worktree.NamesInput{Name: newName("wip"), Base: "main"}, worktree.Posix)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(NativePath(added.Path), "one.txt"), "1")
	writeFile(t, filepath.Join(NativePath(added.Path), "two.txt"), "2")

	if _, err := RemoveWorktree(ctx, repo, added.Path, false, worktree.Posix); err == nil || !strings.Contains(err.Error(), "2 archivos sin commitear") {
		t.Fatalf("dirty without force: %v", err)
	}
	if _, err := os.Stat(NativePath(added.Path)); err != nil {
		t.Fatal("a refused close must leave the folder")
	}
	if _, err := RemoveWorktree(ctx, repo, repo, false, worktree.Posix); err == nil || !strings.Contains(err.Error(), "principal") {
		t.Fatalf("main: %v", err)
	}
	if _, err := RemoveWorktree(ctx, added.Path, added.Path, true, worktree.Posix); err == nil || !strings.Contains(err.Error(), "esta sesión") {
		t.Fatalf("own session: %v", err)
	}
	if _, err := RemoveWorktree(ctx, repo, filepath.Join(filepath.Dir(repo), "elsewhere"), false, worktree.Posix); err == nil || !strings.Contains(err.Error(), "no está en la lista") {
		t.Fatalf("not listed: %v", err)
	}
	cmd, err := RemoveWorktree(ctx, repo, added.Path, true, worktree.Posix)
	if err != nil || !strings.Contains(cmd, "--force") {
		t.Fatalf("force: %q %v", cmd, err)
	}
}

func TestGitMessage(t *testing.T) {
	if got := gitMessage(os.ErrPermission, "x"); got != os.ErrPermission.Error() {
		t.Errorf("plain error: %q", got)
	}
	if got := gitMessage(errString("git worktree: exit 128: fatal: boom\n"), "x"); got != "fatal: boom" {
		t.Errorf("got %q", got)
	}
	if got := gitMessage(errString("git worktree: exit 1: "), "fallback"); got != "fallback" {
		t.Errorf("got %q", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
