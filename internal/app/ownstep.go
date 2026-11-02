package app

import (
	"context"
	"strings"
)

// ownStep pre-approves the commands nxy itself asks the model to run: `batch red|green` and
// `plan save` for the active feature, with the binary's own path. The shape is checked as pure
// string work; only a command that matches reads the store (for the feature id). Anything else
// gets no decision and goes the normal way.
func (h Hooks) ownStep(ctx context.Context, ev HookEvent) (HookResponse, bool) {
	if h.Exe == "" {
		return HookResponse{}, false
	}
	cmd := ev.Command
	cdDir, hasCd := "", false
	if ev.Tool != "PowerShell" {
		cdDir, cmd, hasCd = splitLeadingCd(cmd)
	}
	if ev.AgentID != "" && ev.AgentType == roleScout {
		// The scout's own search needs no feature: it only reads and writes the repo map.
		if !h.parseLocate(cmd, ev.Tool == "PowerShell") {
			return HookResponse{}, false
		}
		if hasCd && h.normDir(cdDir) != h.normDir(ev.Cwd) {
			return HookResponse{}, false
		}
		return HookResponse{AllowReason: "nxy: its own locate for the scout"}, true
	}
	var id string
	switch {
	case ev.AgentID == "" || ev.AgentType == roleImplementer:
		id = h.parseBatchStep(cmd, ev.Tool == "PowerShell")
		if id == "" {
			id = h.parseAffectedStep(cmd, ev.Tool == "PowerShell")
		}
		if id == "" && ev.AgentID == "" {
			id = h.parseSave(cmd, ev.Tool == "PowerShell", h.designWords)
		}
	case ev.AgentType == rolePlanner:
		id = h.parsePlanSave(cmd, ev.Tool == "PowerShell")
		if id == "" {
			id = h.parseSave(cmd, ev.Tool == "PowerShell", h.planGapsWords)
		}
	case ev.AgentType == roleReviewer || ev.AgentType == roleReviewerQuick:
		id = h.parseSave(cmd, ev.Tool == "PowerShell", h.reviewSaveWords)
	case ev.AgentType == roleRefuter:
		id = h.parseSave(cmd, ev.Tool == "PowerShell", h.reviewRefuteWords)
	case ev.AgentType == roleDocumenter:
		id = h.parseDocsStep(cmd, ev.Tool == "PowerShell")
	}
	if id == "" {
		return HookResponse{}, false
	}
	f, ok := h.activeFeature(ctx, ev)
	if !ok || f.ID != id {
		return HookResponse{}, false
	}
	if hasCd {
		d := h.normDir(cdDir)
		if d == "" || (d != h.normDir(ev.Cwd) && d != h.normDir(f.ProjectDir)) {
			return HookResponse{}, false
		}
	}
	return HookResponse{AllowReason: "nxy: its own step for feature " + f.ID}, true
}

// parseBatchStep returns the feature id of `"<exe>" batch red|green --feature <id>` (a leading
// `& ` only in PowerShell), or "".
func (h Hooks) parseBatchStep(cmd string, ps bool) string {
	if strings.ContainsAny(cmd, "\r\n") {
		return ""
	}
	toks, ok := shellWords(cmd)
	if !ok {
		return ""
	}
	if ps && len(toks) > 0 && toks[0] == "&" {
		toks = toks[1:]
	}
	if len(toks) != 5 || !h.isExe(toks[0]) || toks[1] != "batch" || (toks[2] != "red" && toks[2] != "green") || toks[3] != "--feature" {
		return ""
	}
	return toks[4]
}

// parseDocsStep returns the feature id of `"<exe>" review docs --feature <id>` (a leading `& `
// only in PowerShell), or "".
func (h Hooks) parseDocsStep(cmd string, ps bool) string {
	if strings.ContainsAny(cmd, "\r\n") {
		return ""
	}
	toks, ok := shellWords(cmd)
	if !ok {
		return ""
	}
	if ps && len(toks) > 0 && toks[0] == "&" {
		toks = toks[1:]
	}
	if len(toks) != 5 || !h.isExe(toks[0]) || toks[1] != "review" || toks[2] != "docs" || toks[3] != "--feature" {
		return ""
	}
	return toks[4]
}

// parseLocate reports whether cmd is a search the scout may run on its own: `"<exe>" locate "<question>"`,
// `"<exe>" locate --callers|--callees|--impact <symbol>` or `"<exe>" locate --status` (a leading `& `
// only in PowerShell). Anything with more words, or with another flag, is not pre-approved.
func (h Hooks) parseLocate(cmd string, ps bool) bool {
	if strings.ContainsAny(cmd, "\r\n") {
		return false
	}
	toks, ok := shellWords(cmd)
	if !ok {
		return false
	}
	if ps && len(toks) > 0 && toks[0] == "&" {
		toks = toks[1:]
	}
	if len(toks) < 3 || !h.isExe(toks[0]) || toks[1] != "locate" {
		return false
	}
	switch toks[2] {
	case "--status":
		return len(toks) == 3
	case "--callers", "--callees", "--impact":
		return len(toks) == 4 && toks[3] != "" && !strings.HasPrefix(toks[3], "--")
	}
	return len(toks) == 3 && toks[2] != "" && !strings.HasPrefix(toks[2], "--")
}

// parseAffectedStep returns the feature id of `"<exe>" affected --feature <id>` (a leading `& `
// only in PowerShell), or "".
func (h Hooks) parseAffectedStep(cmd string, ps bool) string {
	if strings.ContainsAny(cmd, "\r\n") {
		return ""
	}
	toks, ok := shellWords(cmd)
	if !ok {
		return ""
	}
	if ps && len(toks) > 0 && toks[0] == "&" {
		toks = toks[1:]
	}
	if len(toks) != 4 || !h.isExe(toks[0]) || toks[1] != "affected" || toks[2] != "--feature" {
		return ""
	}
	return toks[3]
}

// parsePlanSave returns the feature id of the planner's two save forms, or "": the Bash heredoc
// (`"<exe>" plan save --feature <id> <<'JSON'` ... `JSON`) and the PowerShell here-string piped
// into `& "<exe>" plan save --feature <id>`. The body is ignored but must end the command.
func (h Hooks) parsePlanSave(cmd string, ps bool) string {
	return h.parseSave(cmd, ps, h.planSaveWords)
}

// parseSave is parsePlanSave for any command that saves a JSON body: words judges the command
// words (without the call operator) and returns the feature id, or "".
func (h Hooks) parseSave(cmd string, ps bool, words func([]string) string) string {
	lines := strings.Split(strings.ReplaceAll(cmd, "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) < 2 {
		return ""
	}
	if !ps {
		head, ok := strings.CutSuffix(strings.TrimSpace(lines[0]), " <<'JSON'")
		if !ok || strings.TrimSpace(lines[len(lines)-1]) != "JSON" {
			return ""
		}
		for _, l := range lines[1 : len(lines)-1] {
			if strings.TrimSpace(l) == "JSON" {
				return ""
			}
		}
		toks, ok := shellWords(head)
		if !ok {
			return ""
		}
		return words(toks)
	}
	if len(lines) < 4 || strings.TrimSpace(lines[0]) != "$OutputEncoding = [System.Text.UTF8Encoding]::new($false)" || strings.TrimSpace(lines[1]) != "@'" {
		return ""
	}
	for _, l := range lines[2 : len(lines)-1] {
		if strings.HasPrefix(l, "'@") {
			return ""
		}
	}
	tail, ok := strings.CutPrefix(lines[len(lines)-1], "'@ | ")
	if !ok {
		return ""
	}
	toks, ok := shellWords(tail)
	if !ok || len(toks) == 0 || toks[0] != "&" {
		return ""
	}
	return words(toks[1:])
}

// reviewSaveWords is `"<exe>" review save --feature <id> --lens <name>`; it returns the id.
func (h Hooks) reviewSaveWords(toks []string) string {
	if len(toks) != 7 || !h.isExe(toks[0]) || toks[1] != "review" || toks[2] != "save" || toks[3] != "--feature" || toks[5] != "--lens" || toks[6] == "" {
		return ""
	}
	return toks[4]
}

// reviewRefuteWords is `"<exe>" review refute --feature <id>`; it returns the id.
func (h Hooks) reviewRefuteWords(toks []string) string {
	if len(toks) != 5 || !h.isExe(toks[0]) || toks[1] != "review" || toks[2] != "refute" || toks[3] != "--feature" {
		return ""
	}
	return toks[4]
}

// designWords is `"<exe>" design agenda|propose|close --feature <id>`; it returns the id.
func (h Hooks) designWords(toks []string) string {
	if len(toks) != 5 || !h.isExe(toks[0]) || toks[1] != "design" || (toks[2] != "agenda" && toks[2] != "propose" && toks[2] != "close") || toks[3] != "--feature" {
		return ""
	}
	return toks[4]
}

func (h Hooks) planSaveWords(toks []string) string {
	if len(toks) != 5 || !h.isExe(toks[0]) || toks[1] != "plan" || toks[2] != "save" || toks[3] != "--feature" {
		return ""
	}
	return toks[4]
}

// planGapsWords is `"<exe>" plan gaps --feature <id>`; it returns the id.
func (h Hooks) planGapsWords(toks []string) string {
	if len(toks) != 5 || !h.isExe(toks[0]) || toks[1] != "plan" || toks[2] != "gaps" || toks[3] != "--feature" {
		return ""
	}
	return toks[4]
}

// isExe reports whether tok names the nxy binary, normalizing slashes, case on Windows and the /c/ form.
func (h Hooks) isExe(tok string) bool {
	return h.normExe(tok) == h.normExe(h.Exe)
}

func (h Hooks) normExe(p string) string {
	if h.Files != nil {
		p = h.Files.NativePath(p)
	}
	p = strings.ReplaceAll(p, "\\", "/")
	if goos == "windows" {
		p = strings.TrimSuffix(strings.ToLower(p), ".exe")
	}
	return p
}

// splitLeadingCd splits exactly one leading `cd <dir> &&` (dir quoted or not) off cmd. Without
// that prefix it returns cmd unchanged and ok=false.
func splitLeadingCd(cmd string) (dir, rest string, ok bool) {
	s, found := strings.CutPrefix(strings.TrimLeft(cmd, " \t"), "cd ")
	if !found {
		return "", cmd, false
	}
	s = strings.TrimLeft(s, " \t")
	if s == "" {
		return "", cmd, false
	}
	if q := s[0]; q == '"' || q == '\'' {
		end := strings.IndexByte(s[1:], q)
		if end < 0 {
			return "", cmd, false
		}
		dir, s = s[1:1+end], s[end+2:]
	} else {
		end := strings.IndexAny(s, " \t")
		if end < 0 {
			return "", cmd, false
		}
		dir, s = s[:end], s[end:]
	}
	s = strings.TrimLeft(s, " \t")
	rest, found = strings.CutPrefix(s, "&& ")
	if !found || dir == "" || strings.ContainsAny(dir, "$`;&|<>()\r\n") {
		return "", cmd, false
	}
	return dir, strings.TrimLeft(rest, " \t"), true
}

// normDir normalizes a directory like normExe does the binary, without the trailing slash.
func (h Hooks) normDir(p string) string {
	if p == "" {
		return ""
	}
	if h.Files != nil {
		p = h.Files.NativePath(p)
	}
	p = strings.TrimRight(strings.ReplaceAll(p, "\\", "/"), "/")
	if goos == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

// shellWords splits one simple command into words, honoring single and double quotes. It refuses
// (ok=false) any unquoted operator (; & | < > ( ) ), any substitution ($ or backtick) and
// unbalanced quotes; a lone `&` word is returned as such so PowerShell's call operator can be
// told apart by the caller.
func shellWords(s string) (words []string, ok bool) {
	var cur strings.Builder
	has := false
	var quote byte
	flush := func() {
		if has {
			words = append(words, cur.String())
		}
		cur.Reset()
		has = false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else if c == '$' || c == '`' {
				return nil, false
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote, has = c, true
		case c == ' ' || c == '\t':
			flush()
		case c == '&' && !has && (i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\t'):
			cur.WriteByte(c)
			has = true
		case strings.IndexByte(";&|<>()$`\n\r", c) >= 0:
			return nil, false
		default:
			cur.WriteByte(c)
			has = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	flush()
	return words, true
}
