package platform

import (
	"os"
	"path/filepath"
	"strings"
)

// RepoKey identifies the repository that contains dir, without running git.
// With an origin remote it is the normalized url (github.com/owner/repo); without
// one, the repo root; outside a repo, dir itself. Paths use forward slashes.
func RepoKey(dir string) string {
	dir = NativePath(dir)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	root, gitDir := findGit(dir)
	if root == "" {
		return filepath.ToSlash(dir)
	}
	if url := originURL(filepath.Join(commonDir(gitDir), "config")); url != "" {
		return normalizeRemote(url)
	}
	return filepath.ToSlash(root)
}

// findGit returns the repo root (GitRoot) and its git dir; a .git file (worktree) is followed to its gitdir.
func findGit(dir string) (root, gitDir string) {
	root, ok := GitRoot(dir)
	if !ok {
		return "", ""
	}
	g := filepath.Join(root, ".git")
	if st, err := os.Stat(g); err != nil || st.IsDir() {
		return root, g
	}
	if b, err := os.ReadFile(g); err == nil {
		if line := strings.TrimSpace(string(b)); strings.HasPrefix(line, "gitdir:") {
			p := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
			if !filepath.IsAbs(p) {
				p = filepath.Join(root, p)
			}
			return root, filepath.Clean(p)
		}
	}
	return root, g
}

// commonDir resolves a worktree git dir to the shared one through its commondir file.
func commonDir(gitDir string) string {
	b, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return gitDir
	}
	p := strings.TrimSpace(string(b))
	if p == "" {
		return gitDir
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(gitDir, p)
	}
	return filepath.Clean(p)
}

// originURL reads the url of [remote "origin"] from a git config file.
func originURL(config string) string {
	b, err := os.ReadFile(config)
	if err != nil {
		return ""
	}
	in := false
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			sec := strings.Join(strings.Fields(strings.Trim(line, "[]")), " ")
			in = strings.EqualFold(sec, `remote "origin"`)
			continue
		}
		if !in {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.EqualFold(strings.TrimSpace(k), "url") {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}

// normalizeRemote turns any git url into host/owner/repo.
func normalizeRemote(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	if i := strings.Index(u, "@"); i >= 0 && i < strings.IndexAny(u+"/", "/") {
		u = u[i+1:]
	}
	// scp-like: host:owner/repo
	if i := strings.Index(u, ":"); i >= 0 && !strings.Contains(u[:i], "/") {
		rest := u[i+1:]
		if j := strings.IndexAny(rest, "/"); !(j > 0 && isPort(rest[:j])) {
			u = u[:i] + "/" + strings.TrimLeft(rest, "/")
		} else {
			u = u[:i] + "/" + rest[j+1:]
		}
	}
	u = strings.TrimRight(strings.ReplaceAll(u, `\`, "/"), "/")
	u = strings.TrimSuffix(u, ".git")
	host, rest, _ := strings.Cut(u, "/")
	return strings.ToLower(host) + "/" + rest
}

func isPort(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
