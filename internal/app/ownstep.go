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
	var id string
	switch {
	case ev.AgentID == "" || ev.AgentType == roleImplementer:
		id = h.parseBatchStep(cmd, ev.Tool == "PowerShell")
		if id == "" {
			id = h.parseAffectedStep(cmd, ev.Tool == "PowerShell")
		}
	case ev.AgentType == rolePlanner:
		id = h.parsePlanSave(cmd, ev.Tool == "PowerShell")
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
		return h.planSaveHead(head)
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
	return h.planSaveWords(toks[1:])
}

func (h Hooks) planSaveHead(head string) string {
	toks, ok := shellWords(head)
	if !ok {
		return ""
	}
	return h.planSaveWords(toks)
}

func (h Hooks) planSaveWords(toks []string) string {
	if len(toks) != 5 || !h.isExe(toks[0]) || toks[1] != "plan" || toks[2] != "save" || toks[3] != "--feature" {
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
