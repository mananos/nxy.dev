package platform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GitHead returns the commit HEAD points to, read from the .git files without
// running git (the approval hook must stay fast). "" when dir is not in a repo or
// HEAD is unborn.
func GitHead(dir string) string {
	dir = NativePath(dir)
	root, gitDir := findGit(dir)
	if root == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(b))
	ref, ok := strings.CutPrefix(head, "ref:")
	if !ok {
		if isSHA(head) {
			return head
		}
		return ""
	}
	ref = strings.TrimSpace(ref)
	common := commonDir(gitDir)
	for _, d := range []string{common, gitDir} {
		if rb, err := os.ReadFile(filepath.Join(d, filepath.FromSlash(ref))); err == nil {
			if s := strings.TrimSpace(string(rb)); isSHA(s) {
				return s
			}
		}
	}
	pb, err := os.ReadFile(filepath.Join(common, "packed-refs"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(pb), "\n") {
		sha, name, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && name == ref && isSHA(sha) {
			return sha
		}
	}
	return ""
}

func isSHA(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// gitOut runs git in dir and returns its stdout.
func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	var out, errw tailWriter
	out.keep, errw.keep = -1, 4096
	exit, err := runProc(ctx, NativePath(dir), append([]string{"git"}, args...), "", "", &out, &errw)
	if err != nil {
		return "", err
	}
	if exit != 0 {
		return "", fmt.Errorf("git %s: exit %d: %s", args[0], exit, strings.TrimSpace(errw.String()))
	}
	return out.String(), nil
}

func splitNUL(s string) []string {
	var res []string
	for _, p := range strings.Split(s, "\x00") {
		if p != "" {
			res = append(res, filepath.ToSlash(p))
		}
	}
	return res
}

func skipInternal(p string) bool {
	for _, pre := range []string{".nxy/", ".codegraph/"} {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

// GitChanged lists the files (relative to dir, slash-separated) that differ from
// base in the working tree plus the untracked ones, skipping .nxy/ and .codegraph/.
func GitChanged(ctx context.Context, dir, base string) ([]string, error) {
	diff, err := gitOut(ctx, dir, "diff", "--name-only", "--diff-filter=d", "-z", "--relative", base, "--")
	if err != nil {
		return nil, err
	}
	others, err := gitOut(ctx, dir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var res []string
	for _, p := range append(splitNUL(diff), splitNUL(others)...) {
		if skipInternal(p) || seen[p] {
			continue
		}
		seen[p] = true
		res = append(res, p)
	}
	return res, nil
}

// GitFiles lists the tracked and untracked-not-ignored files under dir (relative
// to dir, slash-separated).
func GitFiles(ctx context.Context, dir string) ([]string, error) {
	out, err := gitOut(ctx, dir, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	var res []string
	seen := map[string]bool{}
	for _, p := range splitNUL(out) {
		if !seen[p] {
			seen[p] = true
			res = append(res, p)
		}
	}
	return res, nil
}

// GitExcludeLocal makes git ignore pattern through .git/info/exclude (local, never
// committed). It does nothing when .codegraph/ is already ignored.
func GitExcludeLocal(ctx context.Context, dir, pattern string) error {
	probe := strings.Trim(pattern, "/")
	if strings.HasSuffix(pattern, "/") {
		probe += "/"
	}
	if exit, err := runProc(ctx, NativePath(dir), []string{"git", "check-ignore", "-q", probe}, "", "", &tailWriter{keep: 0}, &tailWriter{keep: 0}); err == nil && exit == 0 {
		return nil
	}
	out, err := gitOut(ctx, dir, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return err
	}
	p := strings.TrimSpace(out)
	if p == "" {
		return fmt.Errorf("git rev-parse --git-path: empty")
	}
	p = filepath.FromSlash(p)
	if !filepath.IsAbs(p) {
		p = filepath.Join(NativePath(dir), p)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	prev, _ := os.ReadFile(p)
	add := pattern + "\n"
	if len(prev) > 0 && !strings.HasSuffix(string(prev), "\n") {
		add = "\n" + add
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(add)
	return err
}
