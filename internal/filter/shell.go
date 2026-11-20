package filter

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// jsSpaceClass is the set JS `\s` and String.prototype.trim agree on. RE2's `\s` is ASCII only
// and strings.Fields/TrimSpace differ on U+FEFF, so the filter never uses them.
const jsSpaceClass = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

const (
	reS = `[` + jsSpaceClass + `]`
	reN = `[^` + jsSpaceClass + `]`
)

func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func jsTrim(s string) string { return strings.TrimFunc(s, isJSSpace) }

func jsTrimEnd(s string) string { return strings.TrimRightFunc(s, isJSSpace) }

// jsFields mimics `s.trim().split(/\s+/)` except that an empty string yields no fields.
func jsFields(s string) []string { return strings.FieldsFunc(s, isJSSpace) }

// scan walks the string once tracking quote state and calls visit for every byte outside single
// quotes. Every special char is ASCII, so scanning bytes is safe on UTF-8. visit returns false to stop.
func scan(cmd string, visit func(i int, inDouble bool) bool) {
	inSingle, inDouble := false, false
	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]
		if ch == '\\' && !inSingle {
			i++
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle {
			continue
		}
		if !visit(i, inDouble) {
			return
		}
	}
}

func at(cmd string, i int) byte {
	if i < 0 || i >= len(cmd) {
		return 0
	}
	return cmd[i]
}

// SplitTopLevel splits at top-level &&, ||, ; and | (outside quotes) and returns trimmed, non-empty segments.
func SplitTopLevel(cmd string) []string {
	var raw []string
	start := 0
	scan(cmd, func(i int, inDouble bool) bool {
		if inDouble {
			return true
		}
		opLen := 0
		ch := cmd[i]
		if strings.HasPrefix(cmd[i:], "&&") || strings.HasPrefix(cmd[i:], "||") {
			opLen = 2
		} else if ch == ';' || ch == '|' {
			opLen = 1
		}
		if opLen > 0 {
			if start <= i { // JS slice(start, i) is "" when start > i (`&&&`)
				raw = append(raw, cmd[start:i])
			}
			start = i + opLen
		}
		return true
	})
	raw = append(raw, cmd[start:])
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if t := jsTrim(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// HasHeredoc is true for << / <<- / <<< outside quotes.
func HasHeredoc(cmd string) bool {
	found := false
	scan(cmd, func(i int, inDouble bool) bool {
		if !inDouble && cmd[i] == '<' && at(cmd, i+1) == '<' {
			found = true
			return false
		}
		return true
	})
	return found
}

// HasSubstitution is true for $(, backticks or ${ outside single quotes.
func HasSubstitution(cmd string) bool {
	found := false
	scan(cmd, func(i int, _ bool) bool {
		ch := cmd[i]
		if ch == '`' || (ch == '$' && (at(cmd, i+1) == '(' || at(cmd, i+1) == '{')) {
			found = true
			return false
		}
		return true
	})
	return found
}

// devNullRedirect reports whether rest (the text after a '>') is `>?\s*/dev/null` followed by
// whitespace, end of text or one of ;&|).
func devNullRedirect(rest string) bool {
	rest = strings.TrimPrefix(rest, ">")
	rest = strings.TrimLeftFunc(rest, isJSSpace)
	if !strings.HasPrefix(rest, "/dev/null") {
		return false
	}
	rest = rest[len("/dev/null"):]
	if rest == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return isJSSpace(r) || strings.ContainsRune(";&|)", r)
}

// HasRedirect is true for top-level >, >>, 2>file, &>, < outside quotes; 2>&1 and any redirect into /dev/null do not count.
func HasRedirect(cmd string) bool {
	found := false
	scan(cmd, func(i int, inDouble bool) bool {
		if inDouble {
			return true
		}
		ch := cmd[i]
		if ch == '>' {
			if at(cmd, i-1) == '2' && strings.HasPrefix(cmd[i:], ">&1") {
				return true
			}
			if devNullRedirect(cmd[i+1:]) {
				return true
			}
			found = true
			return false
		}
		if ch == '<' && at(cmd, i+1) != '<' && at(cmd, i-1) != '<' {
			found = true
			return false
		}
		return true
	})
	return found
}

var trailingLimitRe = regexp.MustCompile(reS + `*\|` + reS + `*(?:tail|head)(?:` + reS + `+-n?` + reS + `*\d+|` + reS + `+-\d+|` + reS + `+--lines[= ]\d+)?` + reS + `*$`)

// StripTrailingLimit removes a trailing `| tail -n N` / `| head -n N` suffix.
func StripTrailingLimit(cmd string) string {
	return trailingLimitRe.ReplaceAllLiteralString(cmd, "")
}

// IsBackgrounded is true when the command ends with a background & (not &&).
func IsBackgrounded(cmd string) bool {
	t := jsTrimEnd(cmd)
	return strings.HasSuffix(t, "&") && !strings.HasSuffix(t, "&&")
}

// assignName returns the length of a leading `[A-Za-z_][A-Za-z0-9_]*` of t, or 0.
func assignName(t string) int {
	n := 0
	for n < len(t) {
		c := t[n]
		alpha := c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
		if !alpha && !(n > 0 && c >= '0' && c <= '9') {
			break
		}
		n++
	}
	return n
}

// isAssignment matches /^[A-Za-z_][A-Za-z0-9_]*=/.
func isAssignment(t string) bool {
	n := assignName(t)
	return n > 0 && at(t, n) == '='
}

// FirstToken is the first token of a segment, skipping VAR=value prefixes and a leading time/nice/command/exec.
func FirstToken(segment string) string {
	for _, t := range jsFields(segment) {
		if isAssignment(t) {
			continue
		}
		if t == "time" || t == "nice" || t == "command" || t == "exec" {
			continue
		}
		if len(t) >= 2 && t[0] == '.' && (t[1] == '\\' || t[1] == '/') {
			t = t[2:]
		}
		if i := len(t) - 4; i >= 0 && t[i] == '.' {
			switch strings.ToLower(t[i+1:]) {
			case "exe", "cmd", "bat":
				t = t[:i]
			}
		}
		return t
	}
	return ""
}

// LeadingAssignments returns the leading VAR=value assignments of a command.
func LeadingAssignments(cmd string) map[string]string {
	out := map[string]string{}
	for _, t := range jsFields(cmd) {
		if !isAssignment(t) {
			break
		}
		n := assignName(t)
		out[t[:n]] = t[n+1:]
	}
	return out
}
