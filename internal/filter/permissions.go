package filter

import (
	"regexp"
	"strings"
)

// Rules are the permissions.allow / permissions.deny entries of Claude Code settings.
type Rules struct {
	Allow []string
	Deny  []string
}

// ruleRegex mirrors ruleToRegex of hosts/claude-code/permissions.mjs; nil when the rule is not a Bash rule.
func ruleRegex(rule string) *regexp.Regexp {
	t := jsTrim(rule)
	body := ""
	switch {
	case t == "Bash":
	case strings.HasPrefix(t, "Bash(") && strings.HasSuffix(t, ")") && len(t) >= 6:
		body = t[len("Bash(") : len(t)-1]
		if strings.ContainsAny(body, "\n\r  ") {
			return nil
		}
	default:
		return nil
	}
	var pat string
	switch {
	case body == "" || body == "*":
		pat = `^(?s:.*)$`
	case strings.HasSuffix(body, ":*"):
		pat = `^` + wildcard(body[:len(body)-2]) + `(` + reS + `(?s:.*))?$`
	default:
		pat = `^` + wildcard(body) + `$`
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil
	}
	return re
}

func wildcard(s string) string {
	parts := strings.Split(s, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return strings.Join(parts, `(?s:.*)`)
}

// MatchesAnyRule is true when the (trimmed) command matches any of the rules.
func MatchesAnyRule(cmd string, rules []string) bool {
	c := jsTrim(cmd)
	for _, r := range rules {
		if re := ruleRegex(r); re != nil && re.MatchString(c) {
			return true
		}
	}
	return false
}

// splitLoneAmp splits at a lone & (not part of &&, &>, >&, |&).
func splitLoneAmp(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '&' {
			continue
		}
		if i > 0 && strings.IndexByte("&>|", s[i-1]) >= 0 {
			continue
		}
		if i+1 < len(s) && (s[i+1] == '&' || s[i+1] == '>') {
			continue
		}
		out = append(out, s[start:i])
		start = i + 1
	}
	return append(out, s[start:])
}

// CommandSegments are the commands a line runs: split at &&, ||, ;, |, newlines and a lone &.
func CommandSegments(cmd string) []string {
	lines := strings.Split(cmd, "\n")
	out := []string{}
	for i, line := range lines {
		if i < len(lines)-1 {
			line = strings.TrimSuffix(line, "\r")
		}
		for _, seg := range SplitTopLevel(line) {
			for _, s := range splitLoneAmp(seg) {
				if t := jsTrim(s); t != "" {
					out = append(out, t)
				}
			}
		}
	}
	return out
}

// OriginalVerdict is the verdict Claude Code's rules give the original command: "deny", "allow" or "none".
func OriginalVerdict(cmd string, rules Rules) string {
	segments := CommandSegments(cmd)
	deny := MatchesAnyRule(cmd, rules.Deny)
	for _, s := range segments {
		if deny {
			break
		}
		deny = MatchesAnyRule(s, rules.Deny)
	}
	if deny {
		return "deny"
	}
	if len(segments) == 0 || HasSubstitution(cmd) {
		return "none"
	}
	for _, s := range segments {
		if !MatchesAnyRule(s, rules.Allow) {
			return "none"
		}
	}
	return "allow"
}
