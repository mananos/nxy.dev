package platform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mananos/nxy.dev/internal/jscompat"
)

var (
	gitdirLine = regexp.MustCompile(`(?m)^gitdir:\s*(.+?)\s*$`)
	headRef    = regexp.MustCompile(`^ref:\s*refs/heads/(.+)$`)
)

// GitBranch is the current branch of the checkout containing dir, or the first 7 units of a detached
// HEAD (paths.mjs gitBranch). It reads files only and never spawns git. Not a repo or unreadable: "", false.
func GitBranch(dir string) (string, bool) {
	root, ok := GitRoot(dir)
	if !ok {
		return "", false
	}
	gitDir := filepath.Join(root, ".git")
	st, err := os.Stat(gitDir)
	if err != nil {
		return "", false
	}
	if !st.IsDir() {
		data, err := os.ReadFile(gitDir)
		if err != nil {
			return "", false
		}
		m := gitdirLine.FindStringSubmatch(string(data))
		if m == nil {
			return "", false
		}
		p := jscompat.Trim(m[1])
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		gitDir = p
	}
	raw, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", false
	}
	head := jscompat.Trim(string(raw))
	if m := headRef.FindStringSubmatch(head); m != nil {
		return m[1], true
	}
	return jscompat.UTF16Prefix(head, 7), true
}

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

// GitFileAt reads path (relative to the repo root, forward slashes) as it is at rev. exists is false
// when the revision has no such file; err is for a bad revision or a failing git.
func GitFileAt(ctx context.Context, dir, rev, path string) (content string, exists bool, err error) {
	if _, err := gitOut(ctx, dir, "rev-parse", "--verify", "--quiet", rev+"^{commit}"); err != nil {
		return "", false, fmt.Errorf("git: revision %q not found", rev)
	}
	spec := rev + ":" + filepath.ToSlash(path)
	if _, err := gitOut(ctx, dir, "cat-file", "-e", spec); err != nil {
		return "", false, nil
	}
	out, err := gitOut(ctx, dir, "cat-file", "blob", spec)
	if err != nil {
		return "", false, err
	}
	return out, true, nil
}

// maxUntrackedDiff is the size above which an untracked file gets a header only.
const maxUntrackedDiff = 256 * 1024

// GitFileDiff is the unified diff of one file. Status is "M", "A" or "D".
type GitFileDiff struct {
	Path   string
	Status string
	Text   string
}

// GitDiff returns the per-file diff of the working tree against base, plus the
// untracked files as new-file diffs, skipping .nxy/ and .codegraph/. Paths are
// relative to dir. Binary files carry only a header.
func GitDiff(ctx context.Context, dir, base string) ([]GitFileDiff, error) {
	names, err := gitOut(ctx, dir, "diff", "--name-status", "--no-renames", "-z", "--relative", base, "--")
	if err != nil {
		return nil, err
	}
	text, err := gitOut(ctx, dir, "diff", "--no-color", "--no-ext-diff", "--no-renames", "--relative", "-U3", base, "--")
	if err != nil {
		return nil, err
	}
	var chunks []string
	for _, line := range strings.SplitAfter(text, "\n") {
		if strings.HasPrefix(line, "diff --git ") || len(chunks) == 0 {
			if len(chunks) == 0 && !strings.HasPrefix(line, "diff --git ") {
				continue
			}
			chunks = append(chunks, "")
		}
		chunks[len(chunks)-1] += line
	}
	fields := strings.Split(names, "\x00")
	var res []GitFileDiff
	ci := 0
	for i := 0; i+1 < len(fields); i += 2 {
		status, p := fields[i], filepath.ToSlash(fields[i+1])
		if status == "" {
			break
		}
		chunk := ""
		if ci < len(chunks) {
			chunk = chunks[ci]
		}
		ci++
		if skipInternal(p) {
			continue
		}
		res = append(res, GitFileDiff{Path: p, Status: status[:1], Text: chunk})
	}
	others, err := gitOut(ctx, dir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, p := range splitNUL(others) {
		if skipInternal(p) {
			continue
		}
		full := filepath.Join(NativePath(dir), filepath.FromSlash(p))
		if st, err := os.Stat(full); err == nil && st.Size() > maxUntrackedDiff {
			res = append(res, GitFileDiff{Path: p, Status: "A", Text: "diff --git a/" + p + " b/" + p + "\nnew file mode 100644\nFile too large to diff (" + strconv.FormatInt(st.Size(), 10) + " bytes)\n"})
			continue
		}
		b, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		res = append(res, GitFileDiff{Path: p, Status: "A", Text: newFileDiff(p, b)})
	}
	return res, nil
}

func newFileDiff(p string, b []byte) string {
	head := "diff --git a/" + p + " b/" + p + "\nnew file mode 100644\n"
	probe := b
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	if strings.IndexByte(string(probe), 0) >= 0 {
		return head + "Binary files /dev/null and b/" + p + " differ\n"
	}
	if len(b) == 0 {
		return head
	}
	s := string(b)
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	var sb strings.Builder
	sb.WriteString(head)
	fmt.Fprintf(&sb, "--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n", p, len(lines))
	for _, l := range lines {
		sb.WriteString("+" + l + "\n")
	}
	if !strings.HasSuffix(s, "\n") {
		sb.WriteString("\\ No newline at end of file\n")
	}
	return sb.String()
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
// committed). It does nothing when the pattern is already ignored by the repo's own rules. For a
// directory pattern the probe is its .gitignore: a tool's nested `*` + `!.gitignore` makes the
// directory look ignored while that file still shows up as untracked.
func GitExcludeLocal(ctx context.Context, dir, pattern string) error {
	probe := strings.Trim(pattern, "/")
	if strings.HasSuffix(pattern, "/") {
		probe += "/.gitignore"
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
