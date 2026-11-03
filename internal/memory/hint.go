package memory

import (
	"regexp"
	"strings"
)

var hintRe = regexp.MustCompile(`(?i)(^|[^\p{L}\p{N}_])(siempre|nunca|de ahora en adelante|always|never|from now on)($|[^\p{L}\p{N}_])`)

// RuleHint reports whether a prompt sounds like a rule for the repo.
func RuleHint(prompt string) bool {
	p := strings.TrimSpace(prompt)
	if strings.HasPrefix(p, "/") {
		return false
	}
	return hintRe.MatchString(p)
}
