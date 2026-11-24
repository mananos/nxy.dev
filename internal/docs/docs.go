// Package docs decides which docs a finished change concerns and words what the documenter reads
// and is asked. It ports core/docs.mjs of the 1.x: the `.md` files that name what changed are
// found by code (no tokens), and no documenter is offered when there are none.
//
// "Names what changed" = the name of a changed code file without extension (`ClienteService`,
// `cliente.component`) as a whole word. Names too generic to point at anything (`index`, `utils`)
// and very short ones are left out: they would match every doc in the repo.
//
// Differences with the 1.x: the question and its options are in Spanish ("Actualizar docs" /
// "Dejar"), and there is no `docs.paths` setting (it is not in the v2 config schema).
//
// Pure.
package docs

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/jscompat"
	"github.com/mananos/nxy.dev/v2/internal/review"
)

var generic = map[string]bool{
	"index": true, "main": true, "app": true, "utils": true, "util": true, "types": true, "type": true,
	"config": true, "constants": true, "routes": true, "module": true, "helpers": true, "helper": true,
	"common": true, "shared": true, "service": true, "model": true, "models": true, "test": true,
	"spec": true, "readme": true, "package": true, "build": true, "settings": true, "setup": true,
}

var extRE = regexp.MustCompile(`\.[A-Za-z0-9]+$`)

// DocTerms are the words to look for in docs: one per changed code file (repo-relative paths).
func DocTerms(paths []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, p := range paths {
		if review.IsDocPath(p) || review.IsTestPath(p) {
			continue
		}
		base := p
		if i := strings.LastIndex(p, "/"); i >= 0 {
			base = p[i+1:]
		}
		base = extRE.ReplaceAllString(base, "")
		if jscompat.UTF16Len(base) < 5 || generic[strings.ToLower(base)] {
			continue
		}
		if !seen[base] {
			seen[base] = true
			out = append(out, base)
		}
	}
	return out
}

var (
	notDocs = []string{"agents", "skills", "commands", "lenses", "gentle-ai"}
	docDirs = []string{"docs", "doc", "documentation", "wiki"}
	mdRE    = regexp.MustCompile(`(?i)\.mdx?$`)
	readmRE = regexp.MustCompile(`(?i)^README`)
)

func slashes(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	return strings.TrimPrefix(p, "./")
}

func normWiki(w string) string {
	if w == "" {
		return ""
	}
	return strings.TrimRight(slashes(w), "/")
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// IsUserDoc says whether a `.md`/`.mdx` is something a user reads as documentation: the root's
// files, any README outside dot-folders, anything under a docs-like folder or the CI wiki folder.
// Prompts and plugin files (`agents/`, `skills/`, ...) name code all the time but are not docs.
func IsUserDoc(path, wikiDir string) bool {
	p := slashes(path)
	if !mdRE.MatchString(p) {
		return false
	}
	w := normWiki(wikiDir)
	if w != "" && strings.HasPrefix(p, w+"/") {
		return true
	}
	segs := strings.Split(p, "/")
	base := segs[len(segs)-1]
	segs = segs[:len(segs)-1]
	for _, s := range segs {
		if strings.HasPrefix(s, ".") {
			return false
		}
	}
	if len(segs) > 0 && contains(notDocs, segs[0]) {
		return false
	}
	if len(segs) == 0 {
		return true
	}
	if readmRE.MatchString(base) {
		return true
	}
	for _, s := range segs {
		if contains(docDirs, strings.ToLower(s)) {
			return true
		}
	}
	return false
}

// UserDocGlobs are the rg globs for the same rule as IsUserDoc (IsUserDoc still filters what comes back).
func UserDocGlobs(wikiDir string) []string {
	w := normWiki(wikiDir)
	out := []string{"/*.md", "/*.mdx", "**/README*.md", "**/README*.mdx"}
	for _, d := range docDirs {
		out = append(out, "**/"+d+"/**/*.md", "**/"+d+"/**/*.mdx")
	}
	if w != "" {
		out = append(out, w+"/**/*.md", w+"/**/*.mdx")
	}
	for _, d := range notDocs {
		out = append(out, "!/"+d+"/**")
	}
	return out
}

// TermsPattern is a regex source matching any term as a whole word (dots inside a term are literal).
func TermsPattern(terms []string) string {
	parts := make([]string, len(terms))
	for i, t := range terms {
		var b strings.Builder
		for _, r := range t {
			if strings.ContainsRune(`.*+?^${}()|[]\`, r) {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		}
		parts[i] = `\b` + b.String() + `\b`
	}
	return strings.Join(parts, "|")
}

var (
	wikiRE    = regexp.MustCompile(`(?i)wiki`)
	skipRelRE = regexp.MustCompile(`(?i)^(https?|wiki|true|false|main|master)$`)
	letterRE  = regexp.MustCompile(`(?i)[a-z]`)
	lineSplit = regexp.MustCompile(`\r?\n`)
)

func isWordish(r rune) bool { // [\w.-]
	return r == '.' || r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isDelim(r rune) bool { // [\s"',]
	return jscompat.IsSpace(r) || r == '"' || r == '\'' || r == ','
}

func isPrefix(r rune) bool { // [\s"'=:]
	return jscompat.IsSpace(r) || r == '"' || r == '\'' || r == '=' || r == ':'
}

// pathAt matches `\.?\/?((?:[\w.-]+\/)*[\w.-]+)\/?(?=[\s"',]|$)` at rs[p:], trying the options in
// the order the regex backtracks (greedy first). It returns the captured group and where the
// match ends (the lookahead consumes nothing).
func pathAt(rs []rune, p int) (string, int, bool) {
	opts := func(at int, r rune) []int { // greedy optional: take it, then skip it
		if at < len(rs) && rs[at] == r {
			return []int{at + 1, at}
		}
		return []int{at}
	}
	for _, afterDot := range opts(p, '.') {
		for _, start := range opts(afterDot, '/') {
			// runs of [\w.-] separated by single slashes
			var runs [][2]int
			q := start
			for {
				e := q
				for e < len(rs) && isWordish(rs[e]) {
					e++
				}
				if e == q {
					break
				}
				runs = append(runs, [2]int{q, e})
				if e < len(rs) && rs[e] == '/' {
					q = e + 1
					continue
				}
				break
			}
			k := len(runs)
			if k == 0 {
				continue
			}
			m := k - 1 // runs followed by a slash that can repeat
			if last := runs[k-1][1]; last < len(rs) && rs[last] == '/' {
				m = k
			}
			for j := m; j >= 0; j-- {
				if j+1 > k {
					continue
				}
				end := runs[j][1]
				group := string(rs[start:end])
				for _, e := range opts(end, '/') {
					if e == len(rs) || isDelim(rs[e]) {
						return group, e, true
					}
				}
			}
		}
	}
	return "", 0, false
}

// matchPaths is the matchAll of the path regex over s.
func matchPaths(s string) []string {
	rs := []rune(s)
	var out []string
	for i := 0; i <= len(rs); {
		matched := false
		if i == 0 {
			if g, e, ok := pathAt(rs, 0); ok {
				out = append(out, g)
				i, matched = e, true
			}
		}
		if !matched && i < len(rs) && isPrefix(rs[i]) {
			if g, e, ok := pathAt(rs, i+1); ok {
				out = append(out, g)
				i, matched = e, true
			}
		}
		if !matched {
			i++
		}
	}
	return out
}

// WikiDirFromCi is the folder a CI pipeline publishes to a wiki, from the pipeline files' text:
// a line that talks about a wiki and names a path that exists as a directory in the repo. ""
// when there is none.
func WikiDirFromCi(pipelineTexts []string, isDir func(rel string) bool) string {
	for _, text := range pipelineTexts {
		lines := lineSplit.Split(text, -1)
		for i := range lines {
			if !wikiRE.MatchString(lines[i]) {
				continue
			}
			// The path is usually on the same line or the step's next few lines (`path:`, `source:`, `-d docs`).
			end := i + 6
			if end > len(lines) {
				end = len(lines)
			}
			near := strings.Join(lines[i:end], " ")
			for _, rel := range matchPaths(near) {
				if rel == "" || strings.Contains(rel, "..") || skipRelRE.MatchString(rel) || !letterRE.MatchString(rel) {
					continue
				}
				if isDir(rel) {
					return rel
				}
			}
		}
	}
	return ""
}

// File is one changed file of the packet.
type File struct {
	Path    string
	Status  string
	Added   int
	Removed int
	Diff    string
}

// PacketIn is what the documenter reads.
type PacketIn struct {
	ID      string
	Plan    string
	Files   []File
	Docs    []domain.DocFile
	WikiDir string
	// MaxDiffLines caps the diff lines; 0 means 1500.
	MaxDiffLines int
}

// Packet is what the documenter reads: the plan, what changed, and the docs that name it with
// the lines where they do (docsPacket of the 1.x, same texts).
func Packet(o PacketIn) string {
	max := o.MaxDiffLines
	if max == 0 {
		max = 1500
	}
	out := []string{
		"# nxy docs packet · review " + o.ID,
		"",
		"## The plan",
		jscompat.Trim(o.Plan),
		"",
		"## Docs that name what changed (edit these)",
	}
	for _, d := range o.Docs {
		out = append(out, "- "+d.Path)
		for _, l := range d.Lines {
			out = append(out, fmt.Sprintf("    %s:%d: %s", d.Path, l.Line, l.Text))
		}
	}
	out = append(out, "")
	if o.WikiDir != "" {
		out = append(out, "The repo's CI publishes `"+o.WikiDir+"/` to its wiki: that is where its docs live.", "")
	}
	out = append(out, "## What changed")
	for _, f := range o.Files {
		out = append(out, fmt.Sprintf("- %s (%s, +%d −%d)", f.Path, f.Status, f.Added, f.Removed))
	}
	out = append(out, "")
	used := 0
	for _, f := range o.Files {
		if f.Diff == "" {
			continue
		}
		n := strings.Count(f.Diff, "\n") + 1
		if used+n > max {
			out = append(out, "(diff of "+f.Path+" left out: read the file if a doc depends on it)")
			continue
		}
		used += n
		out = append(out, f.Diff, "")
	}
	return strings.Join(out, "\n")
}

// Shown is the short list of docs in the question: the first three, then ", +N more".
func Shown(docs []string) string {
	n := len(docs)
	if n > 3 {
		n = 3
	}
	s := strings.Join(docs[:n], ", ")
	if len(docs) > 3 {
		s += fmt.Sprintf(", +%d more", len(docs)-3)
	}
	return s
}

// Labels of the options of the docs question.
const (
	LabelUpdate = "Actualizar docs"
	LabelLeave  = "Dejar"
)

// QuestionText is the text of the docs question of a feature.
func QuestionText(id string) string {
	return "nxy feature " + id + ": ¿actualizar los docs que nombran este cambio?"
}

// Option is one answer of the docs question.
type Option struct {
	Label       string
	Description string
}

// Ask is the docs question the main thread puts to the user.
type Ask struct {
	Question string
	Header   string
	Options  []Option
}

// Question is the docs question of feature id for the docs found.
func Question(id string, docs []string) Ask {
	return Ask{
		Question: QuestionText(id),
		Header:   "Docs",
		Options: []Option{
			{LabelUpdate, Shown(docs)},
			{LabelLeave, "los docs quedan como están"},
		},
	}
}

// DocumenterPrompt is the prompt of the documenter: the request, the docs that name the change
// with their lines, the command that prints the full packet and the rules it works by.
func DocumenterPrompt(f domain.Feature, exe string) string {
	exe = strings.ReplaceAll(exe, `\`, "/")
	var b strings.Builder
	fmt.Fprintf(&b, "You are the nxy documenter for feature %s. A change was made and some docs in the repo name what it changed. You bring those docs up to date, nothing else.\n\n", f.ID)
	b.WriteString("## Request (verbatim, the user's own words)\n\n" + f.Request + "\n\n")
	b.WriteString("## Docs that name what changed (edit these)\n\n")
	if f.Docs != nil {
		for _, d := range f.Docs.Docs {
			b.WriteString("- " + d.Path + "\n")
			for _, l := range d.Lines {
				fmt.Fprintf(&b, "    %s:%d: %s\n", d.Path, l.Line, l.Text)
			}
		}
		if f.Docs.WikiDir != "" {
			b.WriteString("\nThe repo's CI publishes `" + f.Docs.WikiDir + "/` to its wiki: that is where its docs live.\n")
		}
	}
	fmt.Fprintf(&b, "\n## Packet\n\nBefore you edit, read the plan, the changed files and the diff with exactly this command (its own Bash call):\n\n\"%s\" review docs --feature %s\n\nRun it alone, without `| head`, `| tail` or a redirect, and read its whole output: it already comes cut to 25,000 bytes and the diff is at the end.\n\n", exe, f.ID)
	b.WriteString("## Rules\n\n")
	b.WriteString("- Edit only the docs listed above. For each one, read it around those lines and change only what the diff made wrong or incomplete: a renamed class, a new field or endpoint, a changed flow, a new step.\n")
	b.WriteString("- Keep the doc's language, tone, headings and format: a doc in Spanish stays in Spanish.\n")
	if f.Docs != nil && f.Docs.WikiDir != "" {
		b.WriteString("- If the change adds something users need that no doc covers, you may add one page in `" + f.Docs.WikiDir + "/`, shaped like its neighbours. Otherwise do not create files.\n")
	} else {
		b.WriteString("- Do not create files.\n")
	}
	b.WriteString("- Do not document internals the docs did not already cover, do not rewrite sections that are still right, and do not touch code.\n")
	b.WriteString("- Edit files only with Edit and Write, never through the shell. Bash is for the packet command only: a shell write to the repo is denied.\n\n")
	b.WriteString("## Response format (required)\n\nUnder 200 tokens:\n\n```\n## Docs updated\n- `path.md` — <what changed, one line>\n\n## Left as is\n- `path.md` — <why, one line>   (omit the section when empty)\n```\n")
	return b.String()
}
