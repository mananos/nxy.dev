package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/worktree"
)

// The git side of `nxy feature list|new|pause|resume|close` (hosts/claude-code/worktrees.mjs). Nothing
// here commits or pushes, and a new worktree never gets a base from the session's HEAD: the base is
// always explicit. Progress, handoff and plan state are not read: the v2 keeps them elsewhere.

const (
	maxDirty    = 5000
	maxBranches = 200
)

// realPath compares paths by their real location: symlinks resolved, slashes, no trailing slash, and
// lower case on Windows.
func realPath(p string) string {
	r := p
	if e, err := filepath.EvalSymlinks(NativePath(p)); err == nil {
		r = e
	}
	r = strings.TrimRight(strings.ReplaceAll(r, `\`, "/"), "/")
	if runtime.GOOS == "windows" {
		r = strings.ToLower(r)
	}
	return r
}

// SamePath reports whether a and b are the same location.
func SamePath(a, b string) bool { return realPath(a) == realPath(b) }

// gitRetry runs git and retries a few times when it fails with "Permission denied" (Windows holds a
// directory open for a moment after a process that used it exits).
func gitRetry(ctx context.Context, dir string, args ...string) (string, error) {
	for i := 0; ; i++ {
		out, err := gitOut(ctx, dir, args...)
		if err == nil || i >= 3 || !strings.Contains(err.Error(), "Permission denied") {
			return out, err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// gitMessage is git's own words for an error, without the "git <verb>: exit N:" prefix.
func gitMessage(err error, fallback string) string {
	msg := err.Error()
	if i := strings.Index(msg, ": exit "); i >= 0 {
		if j := strings.Index(msg[i+2:], ": "); j >= 0 {
			msg = msg[i+2+j+2:]
		}
	}
	if msg = strings.TrimSpace(msg); msg != "" {
		return msg
	}
	return fallback
}

func gitWorktrees(ctx context.Context, root string) []worktree.Worktree {
	out, err := gitOut(ctx, root, "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	return worktree.ParseWorktrees(out)
}

// dirtyCount is the number of files with changes in one worktree, as git sees them (nothing excluded),
// capped. ok is false when git could not read the status.
func dirtyCount(ctx context.Context, path string) (n int, ok bool) {
	out, err := gitOut(ctx, path, "status", "--porcelain", "-z", "-uall")
	if err != nil {
		return 0, false
	}
	parts := strings.Split(out, "\x00")
	for i := 0; i < len(parts) && n < maxDirty; i++ {
		e := parts[i]
		if len(e) < 4 {
			continue
		}
		if strings.ContainsAny(e[:2], "RC") {
			i++
		}
		if e[3:] == ".nxy/local/features/paused.json" {
			continue // nxy's own pause mark is not the user's work
		}
		n++
	}
	return n, true
}

// ListBranches is the repo's local branches, newest commit first, capped.
func ListBranches(ctx context.Context, root string) []string {
	out, err := gitOut(ctx, root, "for-each-ref", "refs/heads", "--sort=-committerdate", "--format=%(refname:short)")
	if err != nil {
		return nil
	}
	var res []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" && len(res) < maxBranches {
			res = append(res, l)
		}
	}
	return res
}

func hasBranch(ctx context.Context, root, branch string) bool {
	_, err := gitOut(ctx, root, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// PauseMark is the content of <runtime>/features/paused.json (~100 bytes).
type PauseMark struct {
	At     int64  `json:"at"`
	Branch string `json:"branch,omitempty"`
}

func pausedFile(path string) string {
	return filepath.Join(RuntimeDir(path), "features", "paused.json")
}

// Paused reads the pause mark of a worktree.
func Paused(path string) (PauseMark, bool) {
	b, err := os.ReadFile(pausedFile(NativePath(path)))
	if err != nil {
		return PauseMark{}, false
	}
	var m struct {
		At     *float64 `json:"at"`
		Branch *string  `json:"branch"`
	}
	if json.Unmarshal(b, &m) != nil || m.At == nil {
		return PauseMark{}, false
	}
	mark := PauseMark{At: int64(*m.At)}
	if m.Branch != nil {
		mark.Branch = *m.Branch
	}
	return mark, true
}

// SetPaused writes (on) or deletes (off) the pause mark of a worktree. The handoff itself is not
// generated here.
func SetPaused(path string, on bool, now time.Time) error {
	path = NativePath(path)
	file := pausedFile(path)
	if !on {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	branch, _ := GitBranch(path)
	b, err := json.Marshal(PauseMark{At: now.UnixMilli(), Branch: branch})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return WriteAtomic(file, b)
}

// WorktreeRow is what the list shows about one worktree.
type WorktreeRow struct {
	Path        string
	Branch      string
	Main        bool
	Locked      bool
	Current     bool
	Dirty       *int // nil: git could not read the status
	Ahead       int
	PausedAt    int64
	MissingDeps bool
}

// WorktreeList is the worktrees of a repo plus the branch lists `new` needs.
type WorktreeList struct {
	Main          string
	RepoName      string
	Rows          []WorktreeRow
	Branches      worktree.Choices
	DefaultBranch string
	Bases         []string
}

// ListWorktrees lists the worktrees of the repo that holds cwd.
func ListWorktrees(ctx context.Context, cwd string) (WorktreeList, error) {
	root, ok := GitRoot(NativePath(cwd))
	if !ok {
		return WorktreeList{}, errors.New("no es un repo git")
	}
	list := gitWorktrees(ctx, root)
	if len(list) == 0 {
		return WorktreeList{}, errors.New("git no pudo listar los worktrees")
	}
	// Without remotes every commit would count as unpushed: one check per list, not per worktree.
	remotes, _ := gitOut(ctx, root, "for-each-ref", "--count=1", "refs/remotes")
	hasRemotes := strings.TrimSpace(remotes) != ""
	res := WorktreeList{}
	for _, wt := range list {
		row := WorktreeRow{Path: wt.Path, Branch: wt.Branch, Main: wt.Main, Locked: wt.Locked, Current: SamePath(wt.Path, root)}
		if n, ok := dirtyCount(ctx, wt.Path); ok {
			row.Dirty = &n
		}
		if hasRemotes {
			if out, err := gitOut(ctx, wt.Path, "rev-list", "--count", "HEAD", "--not", "--remotes"); err == nil {
				row.Ahead, _ = strconv.Atoi(strings.TrimSpace(out))
			}
		}
		if m, ok := Paused(wt.Path); ok {
			row.PausedAt = m.At
		}
		native := NativePath(wt.Path)
		_, pkgErr := os.Stat(filepath.Join(native, "package.json"))
		_, modErr := os.Stat(filepath.Join(native, "node_modules"))
		row.MissingDeps = pkgErr == nil && modErr != nil
		res.Rows = append(res.Rows, row)
	}
	main := list[0]
	branches := ListBranches(ctx, main.Path)
	if main.Branch != "" {
		found := false
		for _, b := range branches {
			found = found || b == main.Branch
		}
		if !found {
			branches = append([]string{main.Branch}, branches...)
		}
	}
	originHead, _ := gitOut(ctx, main.Path, "symbolic-ref", "--short", "-q", "refs/remotes/origin/HEAD")
	res.Main = main.Path
	res.RepoName = main.Path[strings.LastIndex(main.Path, "/")+1:]
	res.DefaultBranch = worktree.DefaultBranchOf(strings.TrimSpace(originHead), main.Branch, branches)
	res.Branches = worktree.BranchChoices(branches, list)
	res.Bases = worktree.BaseChoices(branches, res.DefaultBranch)
	return res, nil
}

// Added is what AddWorktree created.
type Added struct {
	Command string
	Path    string
	Branch  string
}

// AddWorktree creates a worktree: {Name, Base, Issue} is a new branch from the local Base, {Branch} an
// existing one. Every check runs before `git worktree add`.
func AddWorktree(ctx context.Context, cwd string, in worktree.NamesInput, shell worktree.Shell) (Added, error) {
	root, ok := GitRoot(NativePath(cwd))
	var list []worktree.Worktree
	if ok {
		list = gitWorktrees(ctx, root)
	}
	if len(list) == 0 {
		return Added{}, errors.New("no es un repo git")
	}
	main := list[0].Path
	if in.Issue != "" {
		iss := worktree.ParseIssue(in.Issue)
		if !iss.OK {
			return Added{}, errors.New(iss.Error)
		}
		in.Issue = iss.Issue // "#14" and " 14 " both name the branch feature/#14_...
	}
	names := worktree.FeatureNamesOf(in, main)
	if !names.OK {
		return Added{}, errors.New(names.Error)
	}
	if _, err := os.Stat(NativePath(names.Path)); err == nil {
		return Added{}, fmt.Errorf("la carpeta %s ya existe", names.Path)
	}
	if names.Create {
		if hasBranch(ctx, main, names.Branch) {
			return Added{}, fmt.Errorf("la rama %s ya existe: usá --branch si querés abrirla", names.Branch)
		}
		if !hasBranch(ctx, main, names.Base) {
			return Added{}, fmt.Errorf("la base %s no existe como rama local", names.Base)
		}
	} else {
		if !hasBranch(ctx, main, names.Branch) {
			return Added{}, fmt.Errorf("la rama %s no existe como rama local", names.Branch)
		}
		for _, w := range list {
			if w.Branch == names.Branch {
				return Added{}, fmt.Errorf("la rama %s ya está abierta en %s", names.Branch, w.Path)
			}
		}
	}
	argv, err := worktree.AddArgv(names.Branch, names.Path, names.Create, names.Base)
	if err != nil {
		return Added{}, err
	}
	if _, err := gitRetry(ctx, main, argv[1:]...); err != nil {
		return Added{}, errors.New(gitMessage(err, "git worktree add falló"))
	}
	return Added{Command: worktree.CommandText(argv, shell), Path: names.Path, Branch: names.Branch}, nil
}

// FindWorktree returns the worktree of the repo that holds cwd whose real path is path, and the root of
// cwd's checkout.
func FindWorktree(ctx context.Context, cwd, path string) (wt worktree.Worktree, list []worktree.Worktree, root string, ok bool) {
	root, found := GitRoot(NativePath(cwd))
	if !found {
		return wt, nil, "", false
	}
	list = gitWorktrees(ctx, root)
	for _, w := range list {
		if SamePath(w.Path, path) {
			return w, list, root, true
		}
	}
	return wt, list, root, false
}

// RemoveWorktree removes a worktree. Never `--force` unless asked; the branch stays. The main worktree
// and the one of this session are refused; without force a worktree with changes is refused too, saying
// how many files would be lost.
func RemoveWorktree(ctx context.Context, cwd, path string, force bool, shell worktree.Shell) (string, error) {
	wt, list, root, ok := FindWorktree(ctx, cwd, path)
	if !ok {
		return "", errors.New("ese worktree no está en la lista")
	}
	if wt.Main {
		return "", errors.New("el worktree principal no se cierra")
	}
	if SamePath(wt.Path, root) {
		return "", errors.New("no se cierra el worktree de esta sesión")
	}
	if !force {
		if n, ok := dirtyCount(ctx, wt.Path); ok && n > 0 {
			word := "archivos"
			if n == 1 {
				word = "archivo"
			}
			return "", fmt.Errorf("tiene %d %s sin commitear: cerrarlo los PIERDE. Guardalos o usá --force para cerrar igual", n, word)
		}
	}
	// The pause mark dies with the worktree; git would refuse to remove it as an untracked file.
	_ = SetPaused(wt.Path, false, time.Time{})
	argv := worktree.RemoveArgv(wt.Path, force)
	if _, err := gitRetry(ctx, list[0].Path, argv[1:]...); err != nil {
		return "", errors.New(gitMessage(err, "git worktree remove falló"))
	}
	return worktree.CommandText(argv, shell), nil
}
