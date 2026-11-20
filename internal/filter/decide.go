package filter

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// RewriteResult is the engine's answer: 0 = rewritten+allow, 1 = no equivalent,
// 2 = deny, 3 = rewritten but ask. Stdout is the rewritten command for 0 and 3.
type RewriteResult struct {
	Code   int
	Stdout string
}

// Context is what Decide needs besides the command. Rewrite is nil when no engine is available.
type Context struct {
	Tool            string
	Engine          string
	RtkHookDetected bool
	ExcludeCommands []string
	OnlyCommands    []string
	Rewrite         func(string) (RewriteResult, error)
}

// Decision: Action is "skip" or "rewrite"; Command and Allow only mean something on rewrite.
type Decision struct {
	Action  string
	Reason  string
	Command string
	Allow   bool
}

func skip(reason string) Decision { return Decision{Action: "skip", Reason: reason} }

func set(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// Commands that are interactive, stateful or write-only.
var neverFirstTokens = set(
	"sudo", "su", "vim", "vi", "nano", "less", "more", "top", "htop", "ssh", "telnet", "claude",
	"irb", "psql", "mysql", "sqlite3", "watch", "tmux", "screen", "alias",
)

// Script runners: a REPL when bare, an opaque one-off when given a script.
var scriptRunners = set("node", "python", "python3")

// Shell state setters: harmless as a prefix of a chain, pointless alone.
var stateOnlyTokens = set("cd", "export", "source", "unset", "set")

// git subcommands that mutate state or prompt.
var neverGitSubcommands = set("commit", "rebase", "push", "merge", "cherry-pick", "stash", "tag", "reset", "checkout", "switch")

const cappedMaxLines = 20

var (
	escapeCommentRe = regexp.MustCompile(`(?:^|` + reS + `)#` + reS + `*(?:nxy:)?raw\b`)
	trailingLimitN  = regexp.MustCompile(`(?:head|tail)(?:` + reS + `+-n?` + reS + `*(\d+)|` + reS + `+-(\d+)|` + reS + `+--lines[= ](\d+))?` + reS + `*$`)
	gitAddInteract  = regexp.MustCompile(reS + `(?:-p|--patch|-i|--interactive)\b`)
	dockerInteract  = regexp.MustCompile(`^docker` + reS + `+(?:exec|run)\b[^\n\r\x{2028}\x{2029}]*` + reS + `-\w*it?\b`)
	nxyInternalRe   = regexp.MustCompile(`(?:^|[` + jsSpaceClass + `'"/])\.nxy[\\/]`)
	envRawRe        = regexp.MustCompile(`^\$env:NXY_RAW` + reS + `*=` + reS + `*["']?1`)
	envRawReI       = regexp.MustCompile(`(?i)^\$env:NXY_RAW` + reS + `*=` + reS + `*["']?1`)
	psSplitRe       = regexp.MustCompile(`\r?\n|;`)
	// PowerShell statements that only set state before the real command.
	psStateRe = regexp.MustCompile(`(?i)^(?:Set-Location|cd|chdir|pushd|Push-Location)` + reS + `|^\$env:[A-Za-z_][A-Za-z0-9_]*` + reS + `*=`)
	// Anything that makes a PowerShell input a script. The JS `=(?!=)` lookahead is `=` then non-`=` or end.
	psScriptRe = regexp.MustCompile("(?i)[|`]|\\$\\(|@\\(|\\bforeach\\b|\\bif" + reS + "*\\(|\\btry" + reS + "*\\{|\\bfunction\\b|\\$[A-Za-z_]+" + reS + "*=(?:[^=]|$)")
)

// gitSubcommand is the first non-option word after `git`, skipping the argument of -C / -c.
func gitSubcommand(segment string) (string, bool) {
	words := jsFields(jsTrim(segment))
	start := -1
	for i, w := range words {
		if FirstToken(w) == "git" {
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
		if strings.HasPrefix(w, "-") {
			continue
		}
		return w, true
	}
	return "", false
}

// trailingLimitLines: lines kept by a trailing `| head/tail -N` suffix; 10 is the default, +Inf when unknown.
func trailingLimitLines(suffix string) float64 {
	m := trailingLimitN.FindStringSubmatch(suffix)
	if m == nil {
		return posInf
	}
	for _, g := range m[1:] {
		if g != "" {
			n, _ := strconv.ParseFloat(g, 64)
			return n
		}
	}
	return 10
}

var posInf = math.Inf(1)

// segmentIsOpaque: a script runner given something to run, as opposed to a bare REPL.
func segmentIsOpaque(segment string) bool {
	if !scriptRunners[FirstToken(segment)] {
		return false
	}
	n := 0
	for _, w := range jsFields(jsTrim(segment)) {
		if !isAssignment(w) {
			n++
		}
	}
	return n > 1
}

func segmentIsInteractive(segment string) bool {
	t := FirstToken(segment)
	if neverFirstTokens[t] {
		return true
	}
	if scriptRunners[t] && !segmentIsOpaque(segment) {
		return true
	}
	if t == "git" {
		sub, ok := gitSubcommand(segment)
		if ok && neverGitSubcommands[sub] {
			return true
		}
		if ok && sub == "add" && gitAddInteract.MatchString(segment) {
			return true
		}
	}
	return dockerInteract.MatchString(jsTrim(segment))
}

func decidePowerShell(cmd string, ctx Context) Decision {
	var segments []string
	for _, s := range psSplitRe.Split(cmd, -1) {
		if s = jsTrim(s); s != "" {
			segments = append(segments, s)
		}
	}
	if len(segments) == 0 {
		return skip("empty")
	}
	for _, s := range segments {
		if envRawReI.MatchString(s) {
			return skip("escape-env")
		}
	}
	last := segments[len(segments)-1]
	prefixes := segments[:len(segments)-1]
	for _, p := range prefixes {
		if !psStateRe.MatchString(p) {
			return skip("powershell-script")
		}
	}
	if psStateRe.MatchString(last) {
		return skip("interactive-or-stateful")
	}
	if UsesRtk(last) {
		return skip("already-wrapped")
	}
	if psScriptRe.MatchString(last) {
		return skip("powershell-script")
	}
	if HasRedirect(last) {
		return skip("redirect")
	}
	if segmentIsInteractive(last) {
		return skip("interactive-or-stateful")
	}
	if segmentIsOpaque(last) {
		return skip("opaque")
	}
	if ctx.RtkHookDetected {
		return skip("rtk-hook-present")
	}
	if ctx.Rewrite == nil {
		return skip("no-engine")
	}
	stripped := StripTrailingLimit(last)
	limit := last[len(stripped):]
	asked := NormalizeForRtk(stripped)
	res, err := ctx.Rewrite(asked)
	if err != nil {
		return skip("rtk-error")
	}
	rewritten := jsTrim(res.Stdout)
	if (res.Code == 0 || res.Code == 3) && rewritten != "" && rewritten != asked {
		reason := "rtk-ask"
		if res.Code == 0 {
			reason = "rtk"
		}
		return Decision{Action: "rewrite", Reason: reason, Command: strings.Join(append(append([]string{}, prefixes...), rewritten+limit), "; "), Allow: res.Code == 0}
	}
	if res.Code == 2 {
		return skip("rtk-deny")
	}
	if res.Code == 1 {
		return skip("rtk-none")
	}
	return skip("rtk-unchanged")
}

// Decide: should this command be rewritten through the filter engine? Pure apart from ctx.Rewrite.
func Decide(command string, ctx Context) Decision {
	cmd := jsTrim(command)
	if cmd == "" {
		return skip("empty")
	}
	if ctx.Engine != "rtk" {
		return skip("engine-off")
	}

	// 1. Escape hatches
	if LeadingAssignments(cmd)["NXY_RAW"] == "1" || envRawRe.MatchString(cmd) {
		return skip("escape-env")
	}
	if escapeCommentRe.MatchString(cmd) {
		return skip("escape-comment")
	}

	if ctx.Tool == "PowerShell" {
		return decidePowerShell(cmd, ctx)
	}

	segments := SplitTopLevel(cmd)
	tokens := make([]string, len(segments))
	for i, s := range segments {
		tokens[i] = FirstToken(s)
	}
	for _, s := range segments {
		if LeadingAssignments(s)["NXY_RAW"] == "1" {
			return skip("escape-env")
		}
	}

	// 2. Already wrapped
	if UsesRtk(cmd) {
		return skip("already-wrapped")
	}

	// 3. Allow/deny lists on first tokens
	exclude := set(ctx.ExcludeCommands...)
	for _, t := range tokens {
		if exclude[t] {
			return skip("excluded")
		}
	}
	if len(ctx.OnlyCommands) > 0 {
		found := false
		for _, t := range tokens {
			for _, o := range ctx.OnlyCommands {
				if t == o {
					found = true
				}
			}
		}
		if !found {
			return skip("not-in-only")
		}
	}

	// 4. Constructs we never touch
	if HasHeredoc(cmd) {
		return skip("heredoc")
	}
	if HasSubstitution(cmd) {
		return skip("substitution")
	}
	if HasRedirect(cmd) {
		return skip("redirect")
	}
	if IsBackgrounded(cmd) {
		return skip("background")
	}
	allState := true
	for _, t := range tokens {
		if !stateOnlyTokens[t] {
			allState = false
		}
	}
	if allState {
		return skip("interactive-or-stateful")
	}
	for _, s := range segments {
		if segmentIsInteractive(s) {
			return skip("interactive-or-stateful")
		}
	}
	allOpaque := true
	for _, s := range segments {
		if !(stateOnlyTokens[FirstToken(s)] || segmentIsOpaque(s)) {
			allOpaque = false
		}
	}
	if allOpaque {
		return skip("opaque")
	}
	if nxyInternalRe.MatchString(cmd) {
		return skip("nxy-internal")
	}

	// 5. RTK's own hook is present
	if ctx.RtkHookDetected {
		return skip("rtk-hook-present")
	}

	// 6. Ask the engine
	if ctx.Rewrite == nil {
		return skip("no-engine")
	}
	stripped := StripTrailingLimit(cmd)
	limit := cmd[len(stripped):]
	if limit != "" && trailingLimitLines(limit) <= cappedMaxLines {
		return skip("capped")
	}
	asked := NormalizeForRtk(stripped)
	res, err := ctx.Rewrite(asked)
	if err != nil {
		return skip("rtk-error")
	}
	rewritten := jsTrim(res.Stdout)
	if res.Code == 0 && rewritten != "" && rewritten != asked {
		return Decision{Action: "rewrite", Reason: "rtk", Command: rewritten + limit, Allow: true}
	}
	if res.Code == 3 && rewritten != "" && rewritten != asked {
		return Decision{Action: "rewrite", Reason: "rtk-ask", Command: rewritten + limit, Allow: false}
	}
	if res.Code == 1 {
		return skip("rtk-none")
	}
	if res.Code == 2 {
		return skip("rtk-deny")
	}
	return skip("rtk-unchanged")
}
