// Package repos names the repos one feature touches and tells which repo a file belongs to. Pure:
// paths are compared as text (forward slashes, no trailing separator), never against the disk.
// Port of hosts/claude-code/repos.mjs (repoNames, repoLabel, repoOf).
package repos

import "strings"

// clean is a path with forward slashes and no trailing separator.
func clean(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	for len(p) > 1 && strings.HasSuffix(p, "/") {
		p = p[:len(p)-1]
	}
	return p
}

func fold(s string, foldCase bool) string {
	if foldCase {
		return strings.ToLower(s)
	}
	return s
}

func segments(p string) []string {
	var out []string
	for _, s := range strings.Split(clean(p), "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// folder is the last segment of a root (the root itself when it has none).
func folder(root string) string {
	if s := segments(root); len(s) > 0 {
		return s[len(s)-1]
	}
	return root
}

// Names is the display name of each root: its folder name; roots that share one take parent folders
// until unique within their group (clientX/api, clientY/api); the full path when the parents run out.
// The map is keyed by the root as given.
func Names(roots []string, foldCase bool) map[string]string {
	var list []string
	seen := map[string]bool{}
	for _, r := range roots {
		if !seen[r] {
			seen[r] = true
			list = append(list, r)
		}
	}
	groups := map[string][]string{}
	var order []string
	for _, r := range list {
		k := fold(folder(r), foldCase)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}
	names := map[string]string{}
	for _, k := range order {
		members := groups[k]
		if len(members) == 1 {
			names[members[0]] = folder(members[0])
			continue
		}
		max := 0
		for _, m := range members {
			if n := len(segments(m)); n > max {
				max = n
			}
		}
		done := false
		for d := 2; d <= max && !done; d++ {
			cand := make([]string, len(members))
			uniq := map[string]bool{}
			for i, m := range members {
				s := segments(m)
				if len(s) > d {
					s = s[len(s)-d:]
				}
				cand[i] = strings.Join(s, "/")
				uniq[fold(cand[i], foldCase)] = true
			}
			if len(uniq) == len(members) {
				for i, m := range members {
					names[m] = cand[i]
				}
				done = true
			}
		}
		if !done {
			for _, m := range members {
				names[m] = clean(m)
			}
		}
	}
	return names
}

// Label is how a file is named in the review: the relative path in the primary repo, name/rel
// elsewhere, the absolute path (forward slashes) when root is empty (the file belongs to no repo).
func Label(root, abs string, names map[string]string, primary bool) string {
	a := clean(abs)
	if root == "" {
		return a
	}
	r := clean(root)
	rel := ""
	if len(a) >= len(r) {
		rel = strings.TrimLeft(a[len(r):], "/")
	}
	if primary {
		return rel
	}
	name, ok := names[root]
	if !ok {
		name = folder(root)
	}
	if rel == "" {
		return name
	}
	return name + "/" + rel
}

// Of is the root a file belongs to: the longest root that contains it (the first wins a tie).
func Of(file string, roots []string, foldCase bool) (string, bool) {
	a := fold(clean(file), foldCase)
	best, bestLen, found := "", -1, false
	for _, r := range roots {
		c := fold(clean(r), foldCase)
		if (a == c || strings.HasPrefix(a, c+"/")) && len(c) > bestLen {
			best, bestLen, found = r, len(c), true
		}
	}
	return best, found
}

// Split reads "api/src/X.java:12" against the repo names: the name that starts the path (the longest
// wins, names may hold slashes) and the rest. ok is false when no name matches.
func Split(at string, names []string) (name, rel string, ok bool) {
	p := strings.ReplaceAll(at, "\\", "/")
	for _, n := range names {
		if n == "" {
			continue
		}
		if (p == n || strings.HasPrefix(p, n+"/")) && len(n) > len(name) {
			name, rel, ok = n, strings.TrimPrefix(strings.TrimPrefix(p, n), "/"), true
		}
	}
	return name, rel, ok
}
