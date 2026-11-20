package filter

import (
	"regexp"
	"strings"
)

var toolAliases = map[string]string{
	"mvn": "maven", "mvnw": "maven", "mvnd": "maven", "gradlew": "gradle",
	"python3": "python", "py": "python", "pnpx": "npx",
}

// withSubcommand are the tools whose first non-option argument is a meaningful subcommand.
var withSubcommand = map[string]bool{
	"git": true, "npm": true, "pnpm": true, "yarn": true, "bun": true, "docker": true, "cargo": true, "go": true,
	"maven": true, "gradle": true, "gh": true, "kubectl": true, "dotnet": true, "pip": true, "uv": true,
	"poetry": true, "make": true, "npx": true, "deno": true,
}

// stateSegments only set shell state (bash or PowerShell).
var stateSegments = map[string]bool{
	"cd": true, "export": true, "source": true, "set": true, "unset": true, "pushd": true,
	"set-location": true, "push-location": true, "chdir": true,
}

var (
	kindStripRtk = regexp.MustCompile(`(?i)^` + reS + `*((?:` + assignRe + `)*)"?(?:[^"` + jsSpaceClass + `]*[\\/])?rtk(?:\.exe)?"?` + reS + `+(?:proxy` + reS + `+)?`)
	kindEnvVar   = regexp.MustCompile(`(?i)^\$env:`)
	kindSpaces   = regexp.MustCompile(reS + `+`)
	kindLines    = regexp.MustCompile(`\r?\n`)
	kindJunk     = regexp.MustCompile(`[^A-Za-z0-9_.:-]`)
)

func aliasOf(tok string) string {
	if a, ok := toolAliases[tok]; ok {
		return a
	}
	return tok
}

func isStateSegment(seg string) bool {
	return stateSegments[strings.ToLower(FirstToken(seg))] || kindEnvVar.MatchString(seg)
}

// CommandKind is the normalized command kind for metrics grouping (core/filter/kind.mjs): rewritten
// and raw invocations of a tool land in one bucket, and a `cd … && export … && mvn test` chain
// counts as maven. `rtk mvn test` is "maven-test", `git status -s` is "git-status", `ls -la` is "ls".
func CommandKind(cmd string) string {
	var segments []string
	for _, line := range kindLines.Split(jsTrim(cmd), -1) {
		for _, seg := range SplitTopLevel(line) {
			segments = append(segments, kindStripRtk.ReplaceAllString(seg, "${1}"))
		}
	}
	head := ""
	found := false
	for _, seg := range segments {
		if !isStateSegment(seg) {
			head, found = seg, true
			break
		}
	}
	if !found && len(segments) > 0 {
		head = segments[0]
	}
	tool := aliasOf(FirstToken(head))
	if tool == "" {
		return "unknown"
	}
	if strings.HasPrefix(tool, "$") {
		return "powershell-script"
	}
	if !withSubcommand[tool] {
		return tool
	}
	words := kindSpaces.Split(head, -1)
	start := -1
	for i, w := range words {
		if aliasOf(FirstToken(w)) == tool {
			start = i
			break
		}
	}
	for i := start + 1; i < len(words); i++ {
		w := words[i]
		if w == "-C" || w == "-c" {
			i++
			continue
		}
		if strings.HasPrefix(w, "-") || isAssignment(w) {
			continue
		}
		return tool + "-" + kindJunk.ReplaceAllString(w, "")
	}
	return tool
}
