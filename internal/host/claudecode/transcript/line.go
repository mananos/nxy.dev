package transcript

import (
	"regexp"
	"strings"

	"github.com/mananos/nxy.dev/internal/jscompat"
)

var (
	nonHumanRe = regexp.MustCompile(`^<(task-notification|scheduled-wakeup|background-task|system-reminder)`)
	slashRe    = regexp.MustCompile(`<command-(?:name|message)>/?([^<]+)</command-`)
)

// editTools write a file the model names in the call (Bash edits via sed/heredoc are invisible).
var editTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

// jsTrimSpace is the JS String.prototype.trim whitespace set.
func jsTrimSpace(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		switch r {
		case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
			return true
		}
		return r >= 0x2000 && r <= 0x200A
	})
}

// humanText is the text of a human prompt line, or ok=false for tool results / meta / continuations.
func humanText(e any) (string, bool) {
	if jscompat.Truthy(jscompat.Get(e, "isMeta")) || jscompat.Truthy(jscompat.Get(e, "isCompactSummary")) {
		return "", false
	}
	var text string
	switch c := jscompat.Get(e, "message", "content").(type) {
	case string:
		text = c
	case []any:
		if len(c) == 0 {
			return "", false
		}
		first := c[0]
		if !jscompat.Truthy(first) {
			return "", false
		}
		typ := jscompat.Get(first, "type")
		if typ == "tool_result" {
			return "", false
		}
		if typ != "text" {
			return "", false
		}
		text = jscompat.String(jscompat.Or(jscompat.Get(first, "text"), ""))
	default:
		return "", false
	}
	if nonHumanRe.MatchString(text) || strings.HasPrefix(text, "[Request interrupted") {
		return "", false
	}
	return text, true
}

// slashCommand is the command a prompt starts, or ok=false. The command can be "" (only spaces).
func slashCommand(text string) (string, bool) {
	m := slashRe.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	return jsTrimSpace(m[1]), true
}
