package searchscope

import (
	"regexp"
	"strings"

	"github.com/mananos/nxy.dev/internal/filter"
	"github.com/mananos/nxy.dev/internal/jscompat"
)

// LocateNudgeMark is the fixed first words of the planner nudge, so metrics can find it in transcripts.
const LocateNudgeMark = "nxy locate nudge:"

// ReadGateMark is the fixed first words of the read gate's refusal, so the transcript counters can
// skip stopped searches.
const ReadGateMark = "nxy read gate:"

// v2LocateRe is `nxy locate`, `nxy.exe locate` or `<root>/bin/nxy.cmd" locate` (the shim, quoted or not).
var v2LocateRe = regexp.MustCompile(`(?:^|[\s"'/\\])nxy(?:\.exe|\.cmd)?["']?\s+locate(?:[\s"';|&]|$)`)

// IsLocateCommand is true when the command runs locate: the v2 `nxy locate` (binary or shim) or the
// 1.x locate.mjs entry, which old transcripts still carry.
func IsLocateCommand(cmd string) bool {
	return strings.Contains(cmd, "locate.mjs") || v2LocateRe.MatchString(cmd)
}

// Class is the SearchClass of the 1.x. Kind is "grep", "glob", "find", "read" or nil. Target and
// Pattern keep the raw input values (a numeric pattern stays a number). HasPath is only set where
// the 1.x sets it (rg, git grep, grep -r, Select-String).
type Class struct {
	Kind    any
	Broad   bool
	Target  any
	Pattern any
	HasPath *bool
}

func none() Class { return Class{Target: ""} }

var (
	wild       = regexp.MustCompile(`[*?[]`)
	fileExt    = regexp.MustCompile(`\.[A-Za-z0-9]{1,8}$`)
	lettersRe  = regexp.MustCompile(`^-[A-Za-z]+$`)
	rRe        = regexp.MustCompile(`[rR]`)
	findNameRe = regexp.MustCompile(`^-(i?name|i?path|i?regex)$`)
	psRecurse  = regexp.MustCompile(`(?i)^-r(ec\w*)?$`)
	psPathRe   = regexp.MustCompile(`(?i)^-(literal)?path$`)
	psSkipArg  = regexp.MustCompile(`(?i)^-(filter|include|exclude|depth|path|literalpath)$`)
	psPattern  = regexp.MustCompile(`(?i)^-pattern$`)
	dashWord   = regexp.MustCompile(`^-\w+$`)
	assignRe   = regexp.MustCompile(`^[A-Za-z_]\w*=`)
	driveRe    = regexp.MustCompile(`[A-Za-z]:\\`)
)

// LooksLikeFile is the string-only file test of IsBroadMainSearch: the last path segment has an extension.
func LooksLikeFile(p string) bool {
	p = strings.TrimRight(p, `\/`)
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		p = p[i+1:]
	}
	return fileExt.MatchString(p)
}

// IsBroadMainSearch: string work only, is this main-thread call a broad grep/glob/find search?
func IsBroadMainSearch(tool string, input any) bool {
	c := Classify(tool, input, LooksLikeFile)
	k, _ := c.Kind.(string)
	return c.Broad && (k == "grep" || k == "glob" || k == "find")
}

func safeIsFile(isFile func(string) bool, p string) bool {
	return !wild.MatchString(p) && isFile != nil && isFile(p)
}

// pathsBroad: broad unless every path is a file. No paths = the whole working tree = broad.
func pathsBroad(paths []string, isFile func(string) bool) bool {
	if len(paths) == 0 {
		return true
	}
	for _, p := range paths {
		if !safeIsFile(isFile, p) {
			return true
		}
	}
	return false
}

func set(items ...string) map[string]bool {
	m := map[string]bool{}
	for _, i := range items {
		m[i] = true
	}
	return m
}

var (
	rgValue   = set("-g", "--glob", "-t", "--type", "-T", "--type-not", "-A", "-B", "-C", "-m", "--max-count", "-j", "--threads", "-M", "--max-columns", "--iglob", "-r", "--replace", "-d", "--max-depth", "--sort", "--sortr", "-f", "--file", "--type-add", "--context")
	grepValue = set("-m", "--max-count", "-A", "-B", "-C", "--include", "--exclude", "--exclude-dir", "--include-dir", "-f", "--file", "--context", "--label", "-d", "--directories", "-D", "--devices")
	fdValue   = set("-e", "--extension", "-t", "--type", "-d", "--max-depth", "-E", "--exclude", "-x", "--exec", "-X", "--exec-batch", "-j", "--threads", "-c", "--color", "--min-depth", "-S", "--size", "--changed-within", "--changed-before", "-o", "--owner", "-g", "--glob")
	skipPre   = set("rtk", "sudo", "command", "time")
)

// parseArgs splits args into positionals and flags seen. -e/--regexp consume the pattern.
func parseArgs(args []string, valueFlags map[string]bool) (pos, flags []string, pattern *string) {
	at := func(i int) string {
		if i >= 0 && i < len(args) {
			return args[i]
		}
		return ""
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			pos = append(pos, args[i+1:]...)
			return
		case a == "-e" || a == "--regexp":
			if pattern == nil {
				s := at(i + 1)
				pattern = &s
			}
			i++
			flags = append(flags, "-e")
		case strings.HasPrefix(a, "--regexp="):
			if pattern == nil {
				s := a[len("--regexp="):]
				pattern = &s
			}
			flags = append(flags, "-e")
		case strings.HasPrefix(a, "--"):
			name, _, _ := strings.Cut(a, "=")
			flags = append(flags, name)
			if !strings.Contains(a, "=") && valueFlags[a] {
				i++
			}
		case len(a) > 1 && a[0] == '-':
			flags = append(flags, a)
			// A value flag may sit at the end of a cluster (`-nA 2`), or be attached (`-A2`: no skip).
			last := "-" + a[len(a)-1:]
			var skip bool
			if len(a) == 2 {
				skip = valueFlags[a]
			} else {
				skip = valueFlags[last] && lettersRe.MatchString(a)
			}
			if skip {
				i++
			}
		default:
			pos = append(pos, a)
		}
	}
	return
}

func strPtr(s string) *string { return &s }

// shift is `pattern ?? pos.shift() ?? null`.
func shift(pattern *string, pos []string) (*string, []string) {
	if pattern != nil {
		return pattern, pos
	}
	if len(pos) > 0 {
		return &pos[0], pos[1:]
	}
	return nil, pos
}

func patAny(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func first(a []string, def string) string {
	if len(a) > 0 {
		return a[0]
	}
	return def
}

func grepClass(paths []string, pat *string, isFile func(string) bool) Class {
	hp := len(paths) > 0
	return Class{Kind: "grep", Broad: pathsBroad(paths, isFile), Target: first(paths, "."), Pattern: patAny(pat), HasPath: &hp}
}

func indexOf(a []string, s string) int {
	for i, x := range a {
		if x == s {
			return i
		}
	}
	return -1
}

func classifyCommand(name string, args []string, tool string, isFile func(string) bool) Class {
	ps := tool == "PowerShell"
	prev := func(i int) string {
		if i > 0 {
			return args[i-1]
		}
		return ""
	}
	switch {
	case name == "rg" || name == "ripgrep":
		pos, _, pattern := parseArgs(args, rgValue)
		pat, pos := shift(pattern, pos)
		return grepClass(pos, pat, isFile)
	case name == "git":
		var rest []string
		for i, a := range args {
			if !(a == "-C" || (i > 0 && args[i-1] == "-C")) {
				rest = append(rest, a)
			}
		}
		sub := ""
		hasSub := false
		for _, a := range rest {
			if !strings.HasPrefix(a, "-") {
				sub, hasSub = a, true
				break
			}
		}
		if !hasSub || sub != "grep" {
			return none()
		}
		after := args[indexOf(args, "grep")+1:]
		pos, _, pattern := parseArgs(after, grepValue)
		pat, pos := shift(pattern, pos)
		paths := pos
		if dd := indexOf(pos, "--"); dd >= 0 {
			paths = pos[dd+1:]
		}
		return grepClass(paths, pat, isFile)
	case name == "grep" || name == "egrep" || name == "fgrep":
		pos, flags, pattern := parseArgs(args, grepValue)
		recursive := false
		for _, f := range flags {
			if f == "--recursive" || f == "--include" || f == "--dereference-recursive" || (lettersRe.MatchString(f) && rRe.MatchString(f)) {
				recursive = true
				break
			}
		}
		if !recursive {
			return none()
		}
		pat, pos := shift(pattern, pos)
		return grepClass(pos, pat, isFile)
	case name == "find":
		var start []string
		for _, a := range args {
			if strings.HasPrefix(a, "-") || a == "(" || a == "!" {
				break
			}
			start = append(start, a)
		}
		var pat any
		for i, a := range args {
			if findNameRe.MatchString(a) {
				if i+1 < len(args) {
					pat = args[i+1]
				}
				break
			}
		}
		return Class{Kind: "find", Broad: pathsBroad(start, isFile), Target: first(start, "."), Pattern: pat}
	case name == "fd" || name == "fdfind":
		pos, _, _ := parseArgs(args, fdValue)
		var pat any
		if len(pos) > 0 {
			pat, pos = pos[0], pos[1:]
		}
		return Class{Kind: "find", Broad: pathsBroad(pos, isFile), Target: first(pos, "."), Pattern: pat}
	case name == "get-childitem" || name == "gci" || (ps && (name == "ls" || name == "dir")):
		rec := false
		for _, a := range args {
			if psRecurse.MatchString(a) {
				rec = true
				break
			}
		}
		if !rec {
			return none()
		}
		pi := -1
		for i, a := range args {
			if psPathRe.MatchString(a) {
				pi = i
				break
			}
		}
		var positional []string
		for i, a := range args {
			if !strings.HasPrefix(a, "-") && !psSkipArg.MatchString(prev(i)) {
				positional = append(positional, a)
			}
		}
		var target string
		if pi >= 0 {
			target = "."
			if pi+1 < len(args) {
				target = args[pi+1]
			}
		} else {
			target = first(positional, ".")
		}
		return Class{Kind: "find", Broad: pathsBroad([]string{target}, isFile), Target: target}
	case name == "select-string" || name == "sls":
		pi, pati := -1, -1
		for i, a := range args {
			if pi < 0 && psPathRe.MatchString(a) {
				pi = i
			}
			if pati < 0 && psPattern.MatchString(a) {
				pati = i
			}
		}
		var positional []string
		for i, a := range args {
			if !strings.HasPrefix(a, "-") && !dashWord.MatchString(prev(i)) {
				positional = append(positional, a)
			}
		}
		var pattern any
		if pati >= 0 {
			if pati+1 < len(args) {
				pattern = args[pati+1]
			}
		} else if len(positional) > 0 {
			pattern, positional = positional[0], positional[1:]
		}
		target := ""
		if pi >= 0 {
			if pi+1 < len(args) {
				target = args[pi+1]
			}
		} else {
			target = first(positional, "")
		}
		// No path means it reads the pipeline: not a tree search.
		broad := target != "" && pathsBroad([]string{target}, isFile)
		hp := target != ""
		t := target
		if t == "" {
			t = "."
		}
		return Class{Kind: "grep", Broad: broad, Target: t, Pattern: pattern, HasPath: &hp}
	}
	return none()
}

// baseName is name.replace(/^.*[\\/]/, ”): JS `.` stops at a line terminator, so only the first
// line of the token is searched for the last separator.
func baseName(s string) string {
	end := len(s)
	if i := strings.IndexAny(s, "\n\r  "); i >= 0 {
		end = i
	}
	if i := strings.LastIndexAny(s[:end], `\/`); i >= 0 {
		return s[i+1:]
	}
	return s
}

func classifyShell(command, tool string, isFile func(string) bool) Class {
	var firstTargeted *Class
	for _, line := range filter.SplitLines(command) {
		from := 0
		for _, seg := range filter.SplitTopLevel(line) {
			at := -1
			if i := strings.Index(line[from:], seg); i >= 0 {
				at = from + i
			}
			if at >= 0 {
				from = at + len(seg)
			}
			// A segment right after a single `|` reads the pipe, not the tree.
			before := ""
			if at > 0 {
				before = jscompat.TrimEnd(line[:at])
			}
			piped := strings.HasSuffix(before, "|") && !strings.HasSuffix(before, "||")
			posix := tool != "PowerShell" && !driveRe.MatchString(seg)
			toks := filter.Words(seg, posix)
			i := 0
			for i < len(toks) && (assignRe.MatchString(toks[i]) || skipPre[toks[i]]) {
				i++
			}
			if i >= len(toks) {
				continue
			}
			name := baseName(toks[i])
			if len(name) >= 4 && strings.EqualFold(name[len(name)-4:], ".exe") {
				name = name[:len(name)-4]
			}
			name = strings.ToLower(name)
			res := classifyCommand(name, toks[i+1:], tool, isFile)
			if res.Kind == nil {
				continue
			}
			// `x | rg foo` reads stdin unless a path was given.
			if piped && res.Kind == "grep" && (res.HasPath == nil || !*res.HasPath) && name != "select-string" && name != "sls" {
				continue
			}
			if res.Broad {
				return res
			}
			if firstTargeted == nil {
				r := res
				firstTargeted = &r
			}
		}
	}
	if firstTargeted != nil {
		return *firstTargeted
	}
	return none()
}

// Classify classifies a search-like tool call (Grep, Glob, Read, Bash, PowerShell). Never panics;
// isFile never sees a wildcard path.
func Classify(tool string, input any, isFile func(string) bool) (c Class) {
	defer func() {
		if recover() != nil {
			c = none()
		}
	}()
	inp, _ := input.(map[string]any)
	get := func(k string) any {
		v, ok := inp[k]
		if !ok {
			return nil
		}
		return v
	}
	switch tool {
	case "Grep":
		path, _ := get("path").(string)
		return Class{Kind: "grep", Broad: path == "" || !safeIsFile(isFile, path), Target: orDot(path), Pattern: get("pattern")}
	case "Glob":
		t := get("path")
		if !jscompat.Truthy(t) {
			t = "."
		}
		return Class{Kind: "glob", Broad: true, Target: t, Pattern: get("pattern")}
	case "Read":
		targeted := get("offset") != nil || get("limit") != nil || get("pages") != nil
		t := get("file_path")
		if t == nil {
			t = ""
		}
		return Class{Kind: "read", Broad: !targeted, Target: t}
	case "Bash", "PowerShell":
		if cmd, ok := get("command").(string); ok {
			return classifyShell(cmd, tool, isFile)
		}
	}
	return none()
}

func orDot(s string) string {
	if s == "" {
		return "."
	}
	return s
}
