package worktree

import "strings"

// Branch names for a feature that starts in the session's own folder (or in a worktree of a given
// branch). Pure: git runs in internal/platform.

// SuggestBranch is up to two names for the feature's branch: the prefixes shared by two or more of the
// repo's local branches, most used first (a prefix used by a single branch is ignored), then feature/ and
// fix/ as fallback, each plus the slug of the title, skipping names that already exist.
func SuggestBranch(title string, branches []string) []string {
	slug := SlugOf(title)
	if slug == "" {
		return nil
	}
	counts := map[string]int{}
	var order []string
	for _, b := range branches {
		i := strings.Index(b, "/")
		if i <= 0 {
			continue
		}
		p := b[:i+1]
		if counts[p] == 0 {
			order = append(order, p)
		}
		counts[p]++
	}
	// most used first; ties keep the order of the list (newest commit first)
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && counts[order[j]] > counts[order[j-1]]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	var prefixes []string
	for _, p := range order {
		if counts[p] >= 2 {
			prefixes = append(prefixes, p)
		}
	}
	fallback := []string{"fix/", "feature/"}
	if len(prefixes) == 0 {
		fallback = []string{"feature/", "fix/"}
	}
	for _, p := range fallback {
		seen := false
		for _, o := range prefixes {
			seen = seen || o == p
		}
		if !seen {
			prefixes = append(prefixes, p)
		}
	}
	exists := map[string]bool{}
	for _, b := range branches {
		exists[b] = true
	}
	var out []string
	for _, p := range prefixes {
		if name := p + slug; !exists[name] {
			out = append(out, name)
		}
		if len(out) == 2 {
			break
		}
	}
	return out
}

// BaseOptions is the bases to offer, at most max: the first one (the default branch) stays first.
func BaseOptions(bases []string, max int) []string {
	if max <= 0 {
		return nil
	}
	out := []string{}
	for _, b := range bases {
		if b == "" {
			continue
		}
		dup := false
		for _, o := range out {
			dup = dup || o == b
		}
		if !dup && len(out) < max {
			out = append(out, b)
		}
	}
	return out
}

// SwitchArgv is the `git switch` argv: a new branch from an explicit base, or an existing one.
func SwitchArgv(branch, base string, create bool) []string {
	if create {
		return []string{"git", "switch", "-c", branch, base}
	}
	return []string{"git", "switch", branch}
}
