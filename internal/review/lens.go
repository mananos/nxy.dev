package review

import (
	"embed"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed lenses/*.md
var builtinFS embed.FS

// Lens is one checklist the reviewer applies, with what makes it apply.
type Lens struct {
	Name    string
	Title   string
	Paths   []*regexp.Regexp
	Content []string
	Always  bool
	Enabled bool
	Body    string
	// Repos is the least number of repos with code in the diff for a cross-repo lens (0 or 1: a
	// normal lens, chosen per repo by Select).
	Repos int
}

// File is one changed file; Content is what it holds now (empty when Deleted).
type File struct {
	Path    string
	Content string
	Deleted bool
}

// Selected is a lens that applies, with the files that triggered it (none for an always-on lens).
type Selected struct {
	Lens  Lens
	Files []string
}

var frontmatterRE = regexp.MustCompile(`^---\r?\n((?s:.*?))\r?\n---\r?\n?((?s:.*))$`)
var metaRE = regexp.MustCompile(`^([a-zA-Z_]+):\s*(.*)$`)

func list(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// GlobToRegExp turns a glob (`**/entity/**`, `*Controller.java`) into a path matcher. Without a
// slash it matches the file name anywhere; with one, a path suffix. Case-insensitive.
func GlobToRegExp(glob string) *regexp.Regexp {
	g := strings.ReplaceAll(strings.TrimSpace(glob), `\`, "/")
	if strings.HasPrefix(g, "./") {
		g = g[2:]
	} else if strings.HasPrefix(g, "/") {
		g = g[1:]
	}
	var re strings.Builder
	for i := 0; i < len(g); i++ {
		c := g[i]
		switch {
		case c == '*' && i+1 < len(g) && g[i+1] == '*':
			if i+2 < len(g) && g[i+2] == '/' {
				re.WriteString("(?:.*/)?")
				i += 2
			} else {
				re.WriteString(".*")
				i++
			}
		case c == '*':
			re.WriteString("[^/]*")
		case c == '?':
			re.WriteString("[^/]")
		case strings.IndexByte(".+^${}()|[]\\", c) >= 0:
			re.WriteByte('\\')
			re.WriteByte(c)
		default:
			re.WriteByte(c)
		}
	}
	return regexp.MustCompile(`(?i)(?:^|/)` + re.String() + `$`)
}

// ParseLens reads a lens file: frontmatter (name, title, paths and content as comma-separated
// lists, always: true, enabled: false) and the checklist. ok is false without frontmatter.
func ParseLens(text, fallback string) (Lens, bool) {
	m := frontmatterRE.FindStringSubmatch(text)
	if m == nil {
		return Lens{}, false
	}
	meta := map[string]string{}
	for _, line := range strings.Split(m[1], "\n") {
		if kv := metaRE.FindStringSubmatch(strings.TrimSpace(line)); kv != nil {
			meta[kv[1]] = strings.TrimSpace(kv[2])
		}
	}
	name := meta["name"]
	if name == "" {
		name = fallback
	}
	title := meta["title"]
	if title == "" {
		title = name
	}
	l := Lens{
		Name:    name,
		Title:   title,
		Content: list(meta["content"]),
		Always:  meta["always"] == "true",
		Enabled: meta["enabled"] != "false",
		Body:    strings.TrimSpace(m[2]),
	}
	if n, err := strconv.Atoi(meta["repos"]); err == nil && n > 0 {
		l.Repos = n
	}
	for _, g := range list(meta["paths"]) {
		l.Paths = append(l.Paths, GlobToRegExp(g))
	}
	return l, true
}

// Builtin returns the lenses embedded in the binary, sorted by name.
func Builtin() []Lens {
	entries, err := builtinFS.ReadDir("lenses")
	if err != nil {
		return nil
	}
	var out []Lens
	for _, e := range entries {
		raw, err := builtinFS.ReadFile("lenses/" + e.Name())
		if err != nil {
			continue
		}
		if l, ok := ParseLens(string(raw), strings.TrimSuffix(e.Name(), ".md")); ok {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Merge stacks layers (embedded < user < repo): the same name replaces, enabled: false removes.
func Merge(layers ...[]Lens) []Lens {
	var order []string
	byName := map[string]Lens{}
	for _, layer := range layers {
		for _, l := range layer {
			if _, ok := byName[l.Name]; !ok {
				order = append(order, l.Name)
			}
			byName[l.Name] = l
		}
	}
	var out []Lens
	for _, n := range order {
		if l := byName[n]; l.Enabled {
			out = append(out, l)
		}
	}
	return out
}

var docRE = regexp.MustCompile(`(?i)\.(md|mdx|markdown|txt|adoc|asciidoc|rst|png|jpe?g|gif|webp|ico)$`)
var testRE = regexp.MustCompile(`(^|/)(tests?|__tests__|spec|specs|test-?data)/|\.(test|spec)\.[cm]?[jt]sx?$|(Test|Tests|Spec|IT)\.(java|kt|scala|groovy)$|_test\.(go|py)$|(^|/)test_[^/]*\.py$`)

// IsDocPath reports documentation and image files.
func IsDocPath(p string) bool { return docRE.MatchString(p) }

// IsTestPath reports test files.
func IsTestPath(p string) bool { return testRE.MatchString(p) }

// Select returns the lenses that apply to what changed, each with the files that triggered it.
// Docs never trigger one; an always-on lens (base) is always in.
func Select(lenses []Lens, files []File) []Selected {
	var code []File
	for _, f := range files {
		if !IsDocPath(f.Path) {
			code = append(code, f)
		}
	}
	var out []Selected
	for _, l := range lenses {
		if l.Repos > 1 {
			continue
		}
		var hit []string
		if !l.Always {
			for _, f := range code {
				if matches(l, f) {
					hit = append(hit, f.Path)
				}
			}
			if len(hit) == 0 {
				continue
			}
		}
		out = append(out, Selected{Lens: l, Files: hit})
	}
	return out
}

// SelectCross returns the cross-repo lenses (Repos > 1) that apply: the ones whose number of repos
// with code (not docs) in the diff is reached. perRepo has the files of each repo, already labelled
// "<repo>/<path>"; Files lists the code files of all of them.
func SelectCross(lenses []Lens, perRepo [][]File) []Selected {
	var out []Selected
	for _, l := range lenses {
		if l.Repos <= 1 {
			continue
		}
		repos := 0
		var hit []string
		for _, files := range perRepo {
			n := 0
			for _, f := range files {
				if !IsDocPath(f.Path) {
					hit = append(hit, f.Path)
					n++
				}
			}
			if n > 0 {
				repos++
			}
		}
		if repos >= l.Repos {
			out = append(out, Selected{Lens: l, Files: hit})
		}
	}
	return out
}

func matches(l Lens, f File) bool {
	p := path.Clean(strings.ReplaceAll(f.Path, `\`, "/"))
	for _, re := range l.Paths {
		if re.MatchString(p) {
			return true
		}
	}
	if f.Deleted || f.Content == "" {
		return false
	}
	for _, c := range l.Content {
		if strings.Contains(f.Content, c) {
			return true
		}
	}
	return false
}
