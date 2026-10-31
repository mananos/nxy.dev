package filter

import (
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode/utf8"
)

// Port of core/shellwrites.mjs: best-effort list of the files a console command is about to
// write (redirections, `sed -i`, `tee`, `cp`/`mv`, ...). A parser, not a guarantee: scripts,
// `xargs`, variables and globs are not recognised. Never fails.

// WriteOptions tunes WriteTargets. The zero value is the 1.x default.
type WriteOptions struct {
	// Tool is "Bash" (default) or "PowerShell".
	Tool string
	// ContentOnly keeps only writes that rewrite a file's content (not cp/mv).
	ContentOnly bool
	// Native turns a Git Bash path (/c/x) into a native one; nil is the identity.
	Native func(string) string
	// IsDir tells whether an absolute path is a directory; nil means never.
	IsDir func(string) bool
}

type writeTok struct {
	text string
	op   bool
	dup  bool
}

var (
	unresolvable = regexp.MustCompile("[$`*?{]|^~")
	assignWord   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	psPathFlag   = regexp.MustCompile(`(?i)^-(path|filepath|literalpath)$`)
	sedExprFlag  = regexp.MustCompile(`^--(expression|file)=`)
	allDigits    = regexp.MustCompile(`^\d+$`)
)

func isIdentStart(c byte) bool { return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') }
func isIdentChar(c byte) bool  { return isIdentStart(c) || (c >= '0' && c <= '9') }

// heredocWord finds `<<WORD`, `<<-'WORD'` (not `<<<`) in a line and returns WORD.
func heredocWord(line string) (string, bool) {
	for i := 0; i+1 < len(line); i++ {
		if line[i] != '<' || line[i+1] != '<' || (i > 0 && line[i-1] == '<') {
			continue
		}
		j := i + 2
		if j < len(line) && line[j] == '-' {
			j++
		}
		for j < len(line) {
			r, size := utf8.DecodeRuneInString(line[j:])
			if !isJSSpace(r) {
				break
			}
			j += size
		}
		var q byte
		if j < len(line) && (line[j] == '\'' || line[j] == '"') {
			q = line[j]
			j++
		}
		if j >= len(line) || !isIdentStart(line[j]) {
			continue
		}
		k := j
		for k < len(line) && isIdentChar(line[k]) {
			k++
		}
		if q != 0 && (k >= len(line) || line[k] != q) {
			continue
		}
		return line[j:k], true
	}
	return "", false
}

// cutHeredocs removes heredoc bodies: from the line after `<<WORD` to the line equal to WORD.
func cutHeredocs(command string) []string {
	var out []string
	lines := strings.Split(command, "\n")
	for i := 0; i < len(lines); i++ {
		out = append(out, lines[i])
		word, ok := heredocWord(lines[i])
		if !ok {
			continue
		}
		for i+1 < len(lines) {
			i++
			if jsTrim(lines[i]) == word {
				break
			}
		}
	}
	return out
}

// splitQuotedLines splits on newlines that are outside quotes.
func splitQuotedLines(text string) []string {
	var out []string
	start := 0
	var q byte
	for i := 0; i < len(text); i++ {
		ch := text[i]
		switch {
		case q == '\'':
			if ch == '\'' {
				q = 0
			}
		case ch == '\\':
			i++
		case q == '"':
			if ch == '"' {
				q = 0
			}
		case ch == '\'' || ch == '"':
			q = ch
		case ch == '\n':
			out = append(out, text[start:i])
			start = i + 1
		}
	}
	if start > len(text) {
		start = len(text)
	}
	return append(out, text[start:])
}

// tokenizeWrites splits a segment into words and redirection operators, honouring quotes.
func tokenizeWrites(seg string, posix bool) []writeTok {
	var toks []writeTok
	var cur []byte
	has := false
	flush := func() {
		if has {
			toks = append(toks, writeTok{text: string(cur)})
		}
		cur = cur[:0]
		has = false
	}
	for i := 0; i < len(seg); i++ {
		ch := seg[i]
		switch {
		case ch == '\'':
			has = true
			end := len(seg)
			if j := strings.IndexByte(seg[i+1:], '\''); j >= 0 {
				end = i + 1 + j
			}
			cur = append(cur, seg[i+1:end]...)
			i = end
		case ch == '"':
			has = true
			i++
			for ; i < len(seg) && seg[i] != '"'; i++ {
				if posix && seg[i] == '\\' && i+1 < len(seg) {
					i++
				}
				cur = append(cur, seg[i])
			}
		case posix && ch == '\\' && i+1 < len(seg):
			has = true
			i++
			cur = append(cur, seg[i])
		case ch == '>' || ch == '<':
			if has && (allDigits.Match(cur) || string(cur) == "&") {
				cur = cur[:0]
				has = false
			} else {
				flush()
			}
			op := []byte{ch}
			for i+1 < len(seg) && (seg[i+1] == ch || (ch == '>' && seg[i+1] == '|')) {
				i++
				op = append(op, seg[i])
			}
			if ch == '>' && i+1 < len(seg) && seg[i+1] == '&' {
				i++
				for i+1 < len(seg) {
					r, size := utf8.DecodeRuneInString(seg[i+1:])
					if isJSSpace(r) {
						break
					}
					i += size
				}
				toks = append(toks, writeTok{text: string(op), op: true, dup: true})
			} else {
				toks = append(toks, writeTok{text: string(op), op: true, dup: ch == '<'})
			}
		default:
			r, size := utf8.DecodeRuneInString(seg[i:])
			if isJSSpace(r) {
				flush()
			} else {
				has = true
				cur = append(cur, seg[i:i+size]...)
			}
			i += size - 1
		}
	}
	flush()
	return toks
}

func isAbsPath(p string) bool {
	if filepath.IsAbs(p) {
		return true
	}
	return runtime.GOOS == "windows" && (strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`))
}

// resolvePath is node's path.resolve(base, p): a leading / is rooted on the drive of base on Windows.
func resolvePath(base, p string) string {
	if isAbsPath(p) {
		if filepath.VolumeName(p) == "" {
			p = filepath.VolumeName(base) + p
		}
	} else if base == "" {
		p, _ = filepath.Abs(p)
	} else {
		p = filepath.Join(base, p)
	}
	return filepath.Clean(p)
}

func lastPathElem(p string) string {
	parts := strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) == 0 {
		return p
	}
	return parts[len(parts)-1]
}

func nonDashOperands(a []string) []string {
	var out []string
	for _, x := range a {
		if !strings.HasPrefix(x, "-") || x == "-" {
			out = append(out, x)
		}
	}
	return out
}

func hasLetterRunWith(a string, want byte) bool {
	for i := 1; i < len(a); i++ {
		c := a[i]
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
			return false
		}
		if c == want {
			return true
		}
	}
	return false
}

// WriteTargets returns the absolute paths command is about to write, in order, de-duplicated.
func WriteTargets(command, cwd string, o WriteOptions) (res []string) {
	defer func() {
		if recover() != nil {
			res = nil
		}
	}()
	native := o.Native
	if native == nil {
		native = func(s string) string { return s }
	}
	ps := o.Tool == "PowerShell"
	seen := map[string]bool{}
	out := []string{}
	dir := resolvePath("", native(cwd))
	known := true

	add := func(t, at string, atKnown bool) {
		if t == "" || unresolvable.MatchString(t) || strings.HasPrefix(t, "/dev/") || t == "NUL" || t == "nul" || strings.HasPrefix(t, "&") {
			return
		}
		n := native(t)
		if !isAbsPath(n) && !atKnown {
			return
		}
		p := resolvePath(at, n)
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}

	var lines []string
	for _, l := range splitQuotedLines(strings.Join(cutHeredocs(command), "\n")) {
		lines = append(lines, SplitTopLevel(l)...)
	}
	for _, seg := range lines {
		toks := tokenizeWrites(seg, !ps)
		var words []string
		for i := 0; i < len(toks); i++ {
			t := toks[i]
			if t.op {
				var target *writeTok
				if i+1 < len(toks) {
					target = &toks[i+1]
				}
				if target != nil && !target.op {
					i++
				}
				if !t.dup && strings.HasPrefix(t.text, ">") && target != nil && !target.op {
					add(target.text, dir, known)
				}
			} else {
				words = append(words, t.text)
			}
		}
		k := 0
		for k < len(words) {
			w := words[k]
			if assignWord.MatchString(w) || w == "sudo" || w == "env" || w == "rtk" {
				k++
			} else {
				break
			}
		}
		name := ""
		if k < len(words) {
			name = words[k]
		}
		if len(name) >= 4 && strings.EqualFold(name[len(name)-4:], ".exe") {
			name = name[:len(name)-4]
		}
		var args []string
		if k+1 < len(words) {
			args = words[k+1:]
		}

		if name == "cd" || name == "Set-Location" {
			if len(args) == 0 || args[0] == "" || args[0] == "-" || unresolvable.MatchString(args[0]) {
				known = false
			} else {
				dir = resolvePath(dir, native(args[0]))
			}
			continue
		}
		lower := strings.ToLower(name)
		if ps {
			if lower == "set-content" || lower == "add-content" || lower == "out-file" {
				p := ""
				for i := 0; i < len(args); i++ {
					if psPathFlag.MatchString(args[i]) {
						if i+1 < len(args) {
							p = args[i+1]
						}
						break
					}
					if !strings.HasPrefix(args[i], "-") {
						p = args[i]
						break
					}
				}
				if p != "" {
					add(p, dir, known)
				}
			}
			continue
		}

		switch {
		case name == "sed":
			inplace, script := false, false
			var ops []string
			for i := 0; i < len(args); i++ {
				a := args[i]
				switch {
				case a == "-e" || a == "-f":
					script = true
					i++
				case sedExprFlag.MatchString(a):
					script = true
				case a == "--in-place" || strings.HasPrefix(a, "--in-place="):
					inplace = true
				case strings.HasPrefix(a, "--"):
				case hasLetterRunWith(a, 'i') && a[0] == '-':
					inplace = true
				case strings.HasPrefix(a, "-") && a != "-":
				default:
					ops = append(ops, a)
				}
			}
			if inplace {
				if !script && len(ops) > 0 {
					ops = ops[1:]
				} else if !script {
					ops = nil
				}
				for _, f := range ops {
					add(f, dir, known)
				}
			}
		case name == "perl":
			inplace, script := false, false
			var ops []string
			for i := 0; i < len(args); i++ {
				a := args[i]
				if len(a) >= 2 && a[0] == '-' && ((a[1] >= 'A' && a[1] <= 'Z') || (a[1] >= 'a' && a[1] <= 'z')) && !strings.HasPrefix(a, "--") {
				cluster:
					for _, c := range a[1:] {
						switch {
						case c == 'i':
							inplace = true
							break cluster
						case c == 'e' || c == 'E':
							script = true
							i++
							break cluster
						case strings.ContainsRune("MmIFxdDC", c):
							break cluster
						}
					}
				} else {
					ops = append(ops, a)
				}
			}
			if inplace {
				if !script && len(ops) > 0 {
					ops = ops[1:]
				} else if !script {
					ops = nil
				}
				for _, f := range ops {
					add(f, dir, known)
				}
			}
		case name == "tee":
			for _, f := range nonDashOperands(args) {
				add(f, dir, known)
			}
		case name == "truncate":
			var ops []string
			for i := 0; i < len(args); i++ {
				if args[i] == "-s" || args[i] == "-r" {
					i++
				} else if !strings.HasPrefix(args[i], "-") {
					ops = append(ops, args[i])
				}
			}
			for _, f := range ops {
				add(f, dir, known)
			}
		case name == "dd":
			for _, a := range args {
				if strings.HasPrefix(a, "of=") {
					add(a[3:], dir, known)
				}
			}
		case (name == "cp" || name == "mv") && !o.ContentOnly:
			var ops []string
			tdir, hasT := "", false
			for i := 0; i < len(args); i++ {
				if args[i] == "-t" {
					i++
					hasT = i < len(args)
					tdir = ""
					if hasT {
						tdir = args[i]
					}
				} else if !strings.HasPrefix(args[i], "-") {
					ops = append(ops, args[i])
				}
			}
			srcs := ops
			dest := tdir
			if !hasT {
				if len(ops) > 0 {
					srcs = ops[:len(ops)-1]
					dest = ops[len(ops)-1]
				} else {
					srcs, dest = nil, ""
				}
			}
			if dest != "" && len(srcs) > 0 {
				isDir := hasT || strings.HasSuffix(dest, "/") || strings.HasSuffix(dest, `\`)
				if !isDir && known && !unresolvable.MatchString(dest) && o.IsDir != nil {
					isDir = o.IsDir(resolvePath(dir, native(dest)))
				}
				if isDir || len(srcs) > 1 {
					for _, s := range srcs {
						if !unresolvable.MatchString(s) {
							add(filepath.Join(dest, lastPathElem(s)), dir, known)
						}
					}
				} else {
					add(dest, dir, known)
				}
				if name == "mv" {
					for _, s := range srcs {
						add(s, dir, known)
					}
				}
			}
		}
	}
	return out
}
