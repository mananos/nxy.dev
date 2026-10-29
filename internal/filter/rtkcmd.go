package filter

import (
	"regexp"
	"strings"
)

// assignRe is one VAR=value prefix; segRe is SEG of core/filter/rtk.mjs (segment start plus prefixes).
const assignRe = `[A-Za-z_][A-Za-z0-9_]*=(?:"[^"]*"|'[^']*'|` + reN + `+)` + reS + `+`

const segRe = `(^|&&` + reS + `*|\|\|` + reS + `*|;` + reS + `*)((?:` + assignRe + `)*)`

// The JS lookahead `(?=\s|$)` becomes the capture `(\s|$)`, re-emitted: nothing after it can need that char.
var (
	normWrapper = regexp.MustCompile(segRe + `\.[\\/](mvnw(?:\.cmd)?|gradlew(?:\.bat)?)(` + reS + `|$)`)
	normNpm     = regexp.MustCompile(segRe + `(npm|pnpm)` + reS + `+(?:test|t)(` + reS + `|$)`)
	normPnpm    = regexp.MustCompile(segRe + `pnpm` + reS + `+(build|lint|start|dev|check|typecheck|format)(` + reS + `|$)`)
)

// NormalizeForRtk rewrites spellings rtk's `rewrite` does not recognise (see rtk.mjs).
func NormalizeForRtk(cmd string) string {
	cmd = normWrapper.ReplaceAllString(cmd, "${1}${2}${3}${4}")
	cmd = normNpm.ReplaceAllString(cmd, "${1}${2}${3} run test${4}")
	return normPnpm.ReplaceAllString(cmd, "${1}${2}pnpm run ${3}${4}")
}

var rtkHeadRe = regexp.MustCompile(`^` + reS + `*(?:` + assignRe + `)*"?(?:[^"` + jsSpaceClass + `]*[\\/])?[rR][tT][kK](?:\.[eE][xX][eE])?"?` + reS)

// IsRtkInvocation is true when the command already starts with rtk, bare or by path.
func IsRtkInvocation(cmd string) bool { return rtkHeadRe.MatchString(cmd) }

// UsesRtk is true when any top-level segment runs through rtk.
func UsesRtk(cmd string) bool {
	for _, s := range SplitTopLevel(cmd) {
		if IsRtkInvocation(s) {
			return true
		}
	}
	return false
}

var (
	bareRtkRe = regexp.MustCompile(`^[rR][tT][kK](\.[eE][xX][eE])?$`)
	pinRe     = regexp.MustCompile(`(^|&&` + reS + `*|\|\|` + reS + `*|;` + reS + `*|\|` + reS + `*|` + assignRe + `)rtk(?:\.exe)?(` + reS + `)`)
)

// PinRtkPath replaces the bare `rtk` rtk emits with the absolute path it was found at.
func PinRtkPath(rewritten, rtkPath string) string {
	if rtkPath == "" || bareRtkRe.MatchString(rtkPath) {
		return rewritten
	}
	quoted := `"` + strings.ReplaceAll(rtkPath, `\`, "/") + `"`
	return pinRe.ReplaceAllString(rewritten, "${1}"+strings.ReplaceAll(quoted, "$", "$$")+"${2}")
}

var rtkHookRe = regexp.MustCompile(`\brtk(\.exe)?\b[^\n]*\bhook\b`)

// RtkHookIn is true when any PreToolUse hook command invokes `rtk hook`.
func RtkHookIn(commands []string) bool {
	for _, c := range commands {
		if rtkHookRe.MatchString(c) {
			return true
		}
	}
	return false
}
