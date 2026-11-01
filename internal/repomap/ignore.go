package repomap

import (
	"regexp"
	"strings"

	"github.com/mananos/nxy.dev/internal/jscompat"
)

type ignorePattern struct {
	re      *regexp.Regexp
	dirOnly bool
	rooted  bool
}

// Matcher is the minimal .gitignore support of the 1.x: bare names, `dir/`, `/rooted` and `*.ext`.
// Anything fancier (negation, `**`) is skipped rather than guessed at: over-indexing is cheap,
// wrongly dropping real source is not. The returned func tells whether rel (slash-separated, from
// the root) with base name name is ignored; a pattern with a slash in the middle only ever
// matches a rooted path, as in the 1.x.
func Matcher(gitignoreText string) func(rel, name string, isDir bool) bool {
	var pats []ignorePattern
	for _, line := range strings.Split(gitignoreText, "\n") {
		line = jscompat.Trim(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		dirOnly := strings.HasSuffix(line, "/")
		if dirOnly {
			line = line[:len(line)-1]
		}
		rooted := strings.HasPrefix(line, "/")
		if rooted {
			line = line[1:]
		}
		if line == "" || strings.Contains(line, "**") {
			continue
		}
		parts := strings.Split(line, "*")
		for i, s := range parts {
			parts[i] = regexp.QuoteMeta(s)
		}
		re, err := regexp.Compile("^" + strings.Join(parts, "[^/]*") + "$")
		if err != nil {
			continue // a pattern we cannot compile is a pattern we do not enforce
		}
		pats = append(pats, ignorePattern{re: re, dirOnly: dirOnly, rooted: rooted})
	}
	return func(rel, name string, isDir bool) bool {
		for _, p := range pats {
			if p.dirOnly && !isDir {
				continue
			}
			target := name
			if p.rooted {
				target = rel
			}
			if p.re.MatchString(target) {
				return true
			}
		}
		return false
	}
}
