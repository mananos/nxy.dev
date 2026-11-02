package repomap

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/mananos/nxy.dev/internal/jscompat"
)

// Row is one matching symbol of a query.
type Row struct {
	Path string
	Line int
	Kind string
	Name string
	Sig  string
}

// Opts narrows a query: Limit nil means 40.
type Opts struct {
	Limit *int
	Kind  string
	Path  string
}

// Result is what a query found. Reason is "ok" (or "no-index", set by whoever has no map).
type Result struct {
	OK        bool
	Reason    string
	Rows      []Row
	Scanned   int
	Truncated bool
}

// regexQuery is /^\/(.*)\/([gimsuy]*)$/ with JS's `.` (no line terminators).
var regexQuery = regexp.MustCompile(`^/([^\n\r\x{2028}\x{2029}]*)/([gimsuy]*)$`)

// compileQuery is the 1.x `new RegExp(m[1], flags.replace(/g/g, ”) + 'i')` with one wanted
// difference: the 1.x added "i" even when the query already had it, and a flag given twice is
// invalid in JS, so `/x/i` fell back to substring there. Here an explicit `i` is not doubled and
// `/x/i` is a regex. A flag repeated by the user, or a pattern RE2 cannot compile, still means
// substring (nil).
func compileQuery(query string) *regexp.Regexp {
	m := regexQuery.FindStringSubmatch(jscompat.Trim(query))
	if m == nil {
		return nil
	}
	flags := strings.ReplaceAll(m[2], "g", "")
	if !strings.Contains(flags, "i") {
		flags += "i"
	}
	seen := map[rune]bool{}
	prefix := "(?i"
	for _, f := range flags {
		if seen[f] {
			return nil
		}
		seen[f] = true
		if f == 's' || f == 'm' {
			prefix += string(f)
		}
	}
	re, err := regexp.Compile(prefix + ")" + m[1])
	if err != nil {
		return nil
	}
	return re
}

// Query searches the map text without a model: substring, or /regex/, over name, path and (for a
// regex) signature, capped by Limit.
func Query(mapText, query string, opts Opts) Result {
	limit := 40
	if opts.Limit != nil {
		limit = *opts.Limit
	}
	re := compileQuery(query)
	needle := strings.ToLower(query)
	rows := []Row{}
	scanned := 0
	for _, line := range strings.Split(mapText, "\n") {
		if line == "" {
			continue
		}
		scanned++
		f := strings.Split(line, "\t") // fields past the fifth (a tab inside a signature) are dropped
		field := func(i int) string {
			if i < len(f) {
				return f[i]
			}
			return ""
		}
		path, kind, name, sig := field(0), field(2), field(3), field(4)
		if opts.Kind != "" && kind != opts.Kind {
			continue
		}
		if opts.Path != "" && !strings.Contains(strings.ToLower(path), strings.ToLower(opts.Path)) {
			continue
		}
		var hit bool
		if re != nil {
			hit = re.MatchString(name) || re.MatchString(path) || re.MatchString(sig)
		} else {
			hit = strings.Contains(strings.ToLower(name), needle) || strings.Contains(strings.ToLower(path), needle)
		}
		if !hit {
			continue
		}
		ln, _ := strconv.Atoi(field(1))
		rows = append(rows, Row{Path: path, Line: ln, Kind: kind, Name: name, Sig: sig})
		if len(rows) >= limit {
			break
		}
	}
	return Result{OK: true, Reason: "ok", Rows: rows, Scanned: scanned, Truncated: len(rows) >= limit}
}
