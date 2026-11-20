package affected

import (
	"path"
	"sort"
	"strings"
)

// Tree reads repo files by slash-separated, repo-relative path.
type Tree interface {
	Read(path string) ([]byte, bool)
}

// Command is one test command; Dir is repo-relative ("." is the root).
type Command struct {
	Dir  string
	Line string
	// Row is the name of the runner row the command came from; Repo says it is a repo row.
	Row  string
	Repo bool
}

func norm(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	return path.Clean(p)
}

func stemOf(p string) string {
	b := path.Base(p)
	return strings.TrimSuffix(b, path.Ext(b))
}

// ByName picks tests by naming: the changed test files themselves, plus the
// files of all that are tests whose stem matches a stem pattern of a row for
// the name of a changed source (in the same dir when the row says so).
func ByName(rows []Runner, changed, all []string) []string {
	set := map[string]bool{}
	for _, c := range changed {
		c = norm(c)
		if IsTest(rows, c) {
			set[c] = true
			continue
		}
		name := stemOf(c)
		dir := path.Dir(c)
		ext := path.Ext(c)
		for _, a := range all {
			a = norm(a)
			for _, r := range rows {
				if !extOf(r, ext) || !isTestRow(r, a) || (r.SameDir && path.Dir(a) != dir) {
					continue
				}
				for _, s := range r.Stems {
					if strings.ReplaceAll(s, "{name}", name) == stemOf(a) {
						set[a] = true
					}
				}
			}
		}
	}
	return sorted(set)
}

func sorted(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func detected(r Runner, dir string, tree Tree) bool {
	for _, d := range r.Detect {
		data, ok := tree.Read(join(dir, d.File))
		if !ok {
			continue
		}
		if d.Contains == "" || strings.Contains(string(data), d.Contains) {
			return true
		}
	}
	return false
}

func join(dir, file string) string {
	if dir == "." || dir == "" {
		return file
	}
	return dir + "/" + file
}

type groupKey struct {
	row    int
	dir    string // dir the command runs from
	module string // path of the matched dir under dir, for {module}
}

// wrapperUp looks for a wrapper row of rows[i] detected in a strict ancestor
// of dir; it returns the row index and the ancestor dir.
func wrapperUp(rows []Runner, i int, dir string, tree Tree) (int, string, bool) {
	for d := dir; d != "." && d != "/"; {
		d = path.Dir(d)
		for j, w := range rows {
			if w.WrapperOf == rows[i].Name && detected(w, d, tree) {
				return j, d, true
			}
		}
	}
	return 0, "", false
}

// safeItem reports whether s can go unquoted-or-double-quoted into a line for
// both sh and cmd: only letters, digits, space and ._/@+=:,- are allowed.
func safeItem(s string) bool {
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.ContainsRune(" ._/@+=:,-", c):
		default:
			return false
		}
	}
	return true
}

// Commands groups tests into commands. Each test goes to the nearest ancestor
// dir that has the marker of a row whose test globs match it; tests with no
// such dir come back as unmatched. Output order is fixed (row, dir).
func Commands(rows []Runner, tests []string, tree Tree) ([]Command, []string) {
	groups := map[groupKey]map[string]bool{}
	unmatched := map[string]bool{}
	for _, t := range tests {
		t = norm(t)
		placed := false
		dir := path.Dir(t)
		for {
			for i, r := range rows {
				if isTestRow(r, t) && detected(r, dir, tree) {
					it := item(r, dir, t)
					k := groupKey{row: i, dir: dir}
					if j, wd, ok := wrapperUp(rows, i, dir, tree); ok && r.WrapperOf == "" {
						k = groupKey{row: j, dir: wd}
						if strings.Contains(rows[j].Command, "{module}") {
							rel := dir
							if wd != "." {
								rel = strings.TrimPrefix(dir, wd+"/")
							}
							k.module = ":" + strings.ReplaceAll(rel, "/", ":") + ":"
						}
						it = item(rows[j], dir, t)
					}
					placed = true
					if !safeItem(it) || !safeItem(k.module) {
						unmatched[t] = true
						break
					}
					if groups[k] == nil {
						groups[k] = map[string]bool{}
					}
					groups[k][it] = true
					break
				}
			}
			if placed || dir == "." || dir == "/" {
				break
			}
			dir = path.Dir(dir)
		}
		if !placed {
			unmatched[t] = true
		}
	}
	keys := make([]groupKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a].row != keys[b].row {
			return keys[a].row < keys[b].row
		}
		if keys[a].dir != keys[b].dir {
			return keys[a].dir < keys[b].dir
		}
		return keys[a].module < keys[b].module
	})
	var cmds []Command
	for _, k := range keys {
		r := rows[k.row]
		sep := r.Sep
		if sep == "" {
			sep = " "
		}
		items := sorted(groups[k])
		parts := make([]string, len(items))
		for i, it := range items {
			parts[i] = r.Prefix + quote(it)
		}
		line := strings.ReplaceAll(r.Command, "{module}", k.module)
		cmds = append(cmds, Command{Dir: k.dir, Line: strings.ReplaceAll(line, "{tests}", strings.Join(parts, sep)), Row: r.Name, Repo: r.Repo})
	}
	return cmds, sorted(unmatched)
}

// quote wraps an item in double quotes when it has spaces.
func quote(s string) string {
	if strings.ContainsAny(s, " \t") {
		return "\"" + s + "\""
	}
	return s
}

// item renders one test as the argument of its row, relative to dir.
func item(r Runner, dir, test string) string {
	rel := test
	if dir != "." && dir != "" {
		rel = strings.TrimPrefix(test, dir+"/")
	}
	switch r.Item {
	case "dir":
		d := path.Dir(rel)
		if d == "." {
			return "./"
		}
		return "./" + d + "/"
	case "class":
		return className(rel)
	}
	return rel
}

var testRoots = []string{"src/test/java/", "src/test/scala/", "src/test/kotlin/", "src/test/groovy/"}

func className(rel string) string {
	for _, root := range testRoots {
		if strings.HasPrefix(rel, root) {
			rel = strings.TrimPrefix(rel, root)
			break
		}
		if i := strings.Index(rel, "/"+root); i >= 0 {
			rel = rel[i+1+len(root):]
			break
		}
	}
	if strings.HasPrefix(rel, "test/") {
		rel = strings.TrimPrefix(rel, "test/")
	}
	rel = strings.TrimSuffix(rel, path.Ext(rel))
	return strings.ReplaceAll(rel, "/", ".")
}
