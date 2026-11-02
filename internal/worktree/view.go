// Package worktree is the pure side of `nxy feature list|new|close`: parsing `git worktree list
// --porcelain`, branch and folder names, the exact `git worktree add` / `remove` argv and how it is
// shown, the "open" command per shell, which branches are free or busy, and the texts of a row. It
// ports core/featuresview.mjs; git itself runs in internal/platform.
//
// Differences from the 1.x: slugOf strips accents with a table (Latin-1 and Latin Extended-A, the
// letters that have a canonical decomposition) instead of Unicode NFD; every other non-ASCII letter
// becomes "_" in both. newQuestion and closeQuestion (the panel's confirmations) are not ported.
package worktree

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/mananos/nxy.dev/internal/jscompat"
)

const (
	slugMax  = 50
	issueMax = 7
)

// Worktree is one block of `git worktree list --porcelain`. Branch is "" for a detached or bare one.
type Worktree struct {
	Path     string
	Branch   string
	Head     string
	Detached bool
	Locked   bool
	Main     bool
}

func slashes(p string) string { return strings.ReplaceAll(p, `\`, "/") }

var blockSep = regexp.MustCompile(`\r?\n\s*\r?\n`)

// ParseWorktrees parses `git worktree list --porcelain`. The first block is the main worktree.
func ParseWorktrees(porcelain string) []Worktree {
	var out []Worktree
	for _, block := range blockSep.Split(porcelain, -1) {
		var wt *Worktree
		for _, raw := range strings.Split(strings.ReplaceAll(block, "\r\n", "\n"), "\n") {
			line := jscompat.TrimEnd(raw)
			switch {
			case strings.HasPrefix(line, "worktree "):
				wt = &Worktree{Path: slashes(line[9:]), Main: len(out) == 0}
			case wt == nil:
			case strings.HasPrefix(line, "HEAD "):
				wt.Head = line[5:]
			case strings.HasPrefix(line, "branch "):
				wt.Branch = strings.TrimPrefix(line[7:], "refs/heads/")
			case line == "detached":
				wt.Detached = true
			case line == "locked" || strings.HasPrefix(line, "locked "):
				wt.Locked = true
			}
		}
		if wt != nil {
			out = append(out, *wt)
		}
	}
	return out
}

// accents maps the accented letters that have a canonical decomposition to their base letter.
var accents = func() map[rune]rune {
	m := map[rune]rune{}
	for base, set := range map[rune]string{
		'a': "àáâãäåāăą", 'c': "çćĉċč", 'd': "ď", 'e': "èéêëēĕėęě", 'g': "ĝğġģ", 'h': "ĥ",
		'i': "ìíîïĩīĭįİ", 'j': "ĵ", 'k': "ķ", 'l': "ĺļľ", 'n': "ñńņň", 'o': "òóôõöōŏő",
		'r': "ŕŗř", 's': "śŝşš", 't': "ţť", 'u': "ùúûüũūŭůűų", 'w': "ŵ", 'y': "ýÿŷ", 'z': "źżž",
	} {
		for _, r := range set {
			m[r] = base
			m[unicode.ToUpper(r)] = base
		}
	}
	m['Ÿ'] = 'y'
	m['İ'] = 'i'
	return m
}()

// SlugOf is slugOf: lower case a-z0-9 words joined by "_", at most 50 characters.
func SlugOf(text string) string {
	var b strings.Builder
	under := false
	for _, r := range text {
		if r >= 0x300 && r <= 0x36f {
			continue
		}
		if base, ok := accents[r]; ok {
			r = base
		}
		r = unicode.ToLower(r)
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if under {
				b.WriteByte('_')
				under = false
			}
			b.WriteRune(r)
		} else if b.Len() > 0 {
			under = true
		}
	}
	s := b.String()
	if len(s) > slugMax {
		s = s[:slugMax]
	}
	return strings.TrimRight(s, "_")
}

// Issue is the result of ParseIssue.
type Issue struct {
	OK    bool
	Issue string
	Error string
}

// ParseIssue accepts "", "14" or "#14" (digits only, up to 7).
func ParseIssue(text string) Issue {
	trimmed := jscompat.Trim(text)
	t := jscompat.Trim(strings.TrimPrefix(trimmed, "#"))
	if t == "" {
		if trimmed == "#" {
			return Issue{Error: "el n.º de issue son solo dígitos (14 o #14)"}
		}
		return Issue{OK: true}
	}
	digits := len(t) <= issueMax
	for i := 0; i < len(t); i++ {
		if t[i] < '0' || t[i] > '9' {
			digits = false
		}
	}
	if !digits {
		return Issue{Error: "el n.º de issue son solo dígitos, hasta " + strconv.Itoa(issueMax) + " (14 o #14)"}
	}
	return Issue{OK: true, Issue: t}
}

func dirOf(s string) string {
	i := strings.LastIndex(s, "/")
	switch {
	case i > 0:
		return s[:i]
	case i == 0:
		return "/"
	}
	return "."
}

// NamesInput is what the user typed: Name (with Base and Issue) for a new branch, Branch alone for an
// existing one. A nil pointer is "not given".
type NamesInput struct {
	Issue  string
	Name   *string
	Base   string
	Branch *string
}

// FeatureNames is the resolved branch, folder and base of a worktree.
type FeatureNames struct {
	OK     bool
	Create bool
	Slug   string
	Branch string
	Base   string // "" when the branch already exists
	Path   string
	Error  string
}

// FeatureNamesOf is featureNames: the folder lives in <main parent>/<repo>.worktrees/<slug>.
func FeatureNamesOf(in NamesInput, mainPath string) FeatureNames {
	main := strings.TrimRight(slashes(mainPath), "/")
	dir := dirOf(main)
	repo := main[strings.LastIndex(main, "/")+1:]
	prefix := dir
	if dir == "/" {
		prefix = ""
	}
	root := prefix + "/" + repo + ".worktrees"
	fail := func(msg string) FeatureNames { return FeatureNames{Error: msg} }
	if in.Branch != nil && in.Name == nil {
		branch := jscompat.Trim(*in.Branch)
		slug := SlugOf(strings.TrimPrefix(branch, "feature/"))
		if branch == "" || slug == "" {
			return fail("elegí una rama")
		}
		return FeatureNames{OK: true, Slug: slug, Branch: branch, Path: root + "/" + slug}
	}
	name := ""
	if in.Name != nil {
		name = *in.Name
	}
	if SlugOf(name) == "" {
		return fail("poné un nombre para el feature (letras o números)")
	}
	base := jscompat.Trim(in.Base)
	if base == "" {
		return fail("elegí la rama base")
	}
	slug := SlugOf(name)
	branch := "feature/" + slug
	if in.Issue != "" {
		branch = "feature/#" + in.Issue + "_" + slug
	}
	// the folder comes from the branch without `feature/`: reopening it as «existing» lands in the same folder
	folder := SlugOf(strings.TrimPrefix(branch, "feature/"))
	return FeatureNames{OK: true, Create: true, Slug: folder, Branch: branch, Base: base, Path: root + "/" + folder}
}

// DefaultBranchOf is the repo's main branch: origin/HEAD (if that branch exists locally), else the
// main worktree's branch, else main / master, else "".
func DefaultBranchOf(originHead, mainBranch string, branches []string) string {
	has := func(b string) bool {
		for _, x := range branches {
			if x == b {
				return true
			}
		}
		return false
	}
	if o := jscompat.Trim(originHead); o != "" {
		b := o
		if i := strings.Index(o, "/"); i >= 0 {
			b = o[i+1:]
		}
		if b != "" && has(b) {
			return b
		}
	}
	if mainBranch != "" {
		return mainBranch
	}
	if has("main") {
		return "main"
	}
	if has("master") {
		return "master"
	}
	return ""
}

// BaseChoices is the main branch first, then the other local branches (busy ones included).
func BaseChoices(branches []string, defaultBranch string) []string {
	out := []string{}
	if defaultBranch != "" {
		out = append(out, defaultBranch)
	}
	for _, b := range branches {
		dup := false
		for _, o := range out {
			if o == b {
				dup = true
			}
		}
		if !dup {
			out = append(out, b)
		}
	}
	return out
}

// ErrNoBase is what AddArgv returns for a new branch without an explicit base.
var ErrNoBase = errors.New("addArgv: create needs an explicit base")

// AddArgv is the `git worktree add` argv. A new branch needs an explicit base.
func AddArgv(branch, path string, create bool, base string) ([]string, error) {
	if create {
		if base == "" {
			return nil, ErrNoBase
		}
		return []string{"git", "worktree", "add", "-b", branch, path, base}, nil
	}
	return []string{"git", "worktree", "add", path, branch}, nil
}

// RemoveArgv is the `git worktree remove` argv.
func RemoveArgv(path string, force bool) []string {
	if force {
		return []string{"git", "worktree", "remove", "--force", path}
	}
	return []string{"git", "worktree", "remove", path}
}

// Shell is the syntax the commands are shown in.
type Shell string

const (
	PowerShell Shell = "powershell"
	Posix      Shell = "posix"
)

func quote(s string, shell Shell) string {
	esc := `'\''`
	if shell == PowerShell {
		esc = "''"
	}
	return "'" + strings.ReplaceAll(s, "'", esc) + "'"
}

var plainArg = regexp.MustCompile(`^[A-Za-z0-9_\-./:=@%+,]+$`)

// CommandText shows argv with single quotes only around an argument that needs them. The shell only
// matters for an argument that itself holds a single quote.
func CommandText(argv []string, shell Shell) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if plainArg.MatchString(a) {
			parts[i] = a
		} else {
			parts[i] = quote(a, shell)
		}
	}
	return strings.Join(parts, " ")
}

// ShellOf is powershell on Windows without a SHELL variable (Git Bash sets it), else posix.
func ShellOf(goos, shellEnv string) Shell {
	if goos == "windows" && shellEnv == "" {
		return PowerShell
	}
	return Posix
}

// OpenCommand is the line that opens Claude in a worktree.
func OpenCommand(path string, shell Shell) string {
	if shell == PowerShell {
		return "cd " + quote(path, shell) + "; claude"
	}
	return "cd " + quote(path, shell) + " && claude"
}

// Busy is a local branch that a worktree has open.
type Busy struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Choices splits the local branches in free ones and busy ones.
type Choices struct {
	Free []string `json:"free"`
	Busy []Busy   `json:"busy"`
}

// BranchChoices splits the local branches in those no worktree has open (free) and those that one has.
func BranchChoices(branches []string, worktrees []Worktree) Choices {
	held := map[string]string{}
	for _, w := range worktrees {
		if w.Branch == "" {
			continue
		}
		if _, ok := held[w.Branch]; !ok {
			held[w.Branch] = w.Path
		}
	}
	out := Choices{Free: []string{}, Busy: []Busy{}}
	for _, b := range branches {
		if p, ok := held[b]; ok {
			out.Busy = append(out.Busy, Busy{Name: b, Path: p})
		} else {
			out.Free = append(out.Free, b)
		}
	}
	return out
}

func round(x float64) float64 { return math.Floor(x + 0.5) }

func ago(ms int64) string {
	m := math.Max(0, round(float64(ms)/60000))
	if m < 1 {
		return "menos de 1 min"
	}
	if m < 60 {
		return strconv.FormatFloat(m, 'f', -1, 64) + " min"
	}
	h := round(m / 60)
	if h < 48 {
		return strconv.FormatFloat(h, 'f', -1, 64) + " h"
	}
	return strconv.FormatFloat(round(h/24), 'f', -1, 64) + " d"
}

// RowInput is what a row shows about one worktree. Dirty nil is "git could not read the status"; the
// time fields are Unix milliseconds, 0 for "none".
type RowInput struct {
	Path        string
	Branch      string
	Main        bool
	Current     bool
	Dirty       *int
	Ahead       int
	BatchDone   int
	BatchTotal  int
	ReviewDone  bool
	HandoffAt   int64
	PausedAt    int64
	Running     bool
	MissingDeps bool
}

// Row is the texts of one worktree row.
type Row struct {
	Name   string   `json:"name"`
	Branch string   `json:"branch"`
	Tags   []string `json:"tags"`
	Lines  []string `json:"lines"`
}

func plural(n int, one, many string) string {
	if n == 1 {
		return strconv.Itoa(n) + " " + one
	}
	return strconv.Itoa(n) + " " + many
}

// RowView is rowView: the tags and lines of one worktree, `now` in Unix milliseconds.
func RowView(wt RowInput, now int64) Row {
	path := slashes(wt.Path)
	r := Row{Name: path[strings.LastIndex(path, "/")+1:], Branch: wt.Branch, Tags: []string{}, Lines: []string{}}
	if r.Branch == "" {
		r.Branch = "(detached)"
	}
	if wt.Main {
		r.Tags = append(r.Tags, "principal")
	}
	if wt.Current {
		r.Tags = append(r.Tags, "esta sesión")
	}
	if wt.PausedAt != 0 {
		r.Tags = append(r.Tags, "pausado hace "+ago(now-wt.PausedAt))
	}
	if wt.Running {
		r.Tags = append(r.Tags, "plan corriendo")
	}
	switch {
	case wt.Dirty == nil:
		r.Lines = append(r.Lines, "no se pudo leer el estado")
	case *wt.Dirty > 0:
		r.Lines = append(r.Lines, plural(*wt.Dirty, "archivo", "archivos")+" sin commitear")
	default:
		r.Lines = append(r.Lines, "limpio")
	}
	if wt.BatchTotal > 0 {
		line := "lote " + strconv.Itoa(wt.BatchDone) + "/" + strconv.Itoa(wt.BatchTotal)
		if wt.ReviewDone {
			line += " · review hecha"
		}
		r.Lines = append(r.Lines, line)
	}
	if wt.HandoffAt != 0 {
		r.Lines = append(r.Lines, "handoff hace "+ago(now-wt.HandoffAt))
	} else {
		r.Lines = append(r.Lines, "sin handoff")
	}
	if wt.Ahead > 0 {
		r.Lines = append(r.Lines, plural(wt.Ahead, "commit", "commits")+" sin pushear")
	}
	if wt.MissingDeps {
		r.Lines = append(r.Lines, "falta node_modules: corré npm install en esa carpeta")
	}
	return r
}
