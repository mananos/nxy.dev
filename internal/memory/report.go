package memory

import (
	"regexp"
	"strings"
)

// Limits of a report.
const (
	ReportMaxLearnings = 5
	ReportMaxRunes     = 300
)

// LearningIn is a learning as an agent reports it.
type LearningIn struct {
	Text  string   `json:"text"`
	Files []string `json:"files,omitempty"`
}

// Report is what an agent returns to the memory.
type Report struct {
	Learnings []LearningIn
	False     []string // A<n> the agent found false, or "<repo>:A<n>" when it named the repo
}

var (
	learnedRe  = regexp.MustCompile(`(?i)^\s*learned:\s*(.+?)\s*$`)
	falseRe    = regexp.MustCompile(`(?i)^\s*false:\s*(?:\[([^\]]+)\]\s*|([A-Za-z0-9_.-]+):\s*)?(A\d+)\b`)
	falseRefRe = regexp.MustCompile(`(?i)^\s*(?:\[([^\]]+)\]\s*|([A-Za-z0-9_.-]+):\s*)?(A\d+)\s*$`)
)

// SplitFalse reads a false reference: "A3", "[api] A3" or "api:A3". The repo is empty for the plain
// form; ok is false when it is none of them.
func SplitFalse(s string) (repo, id string, ok bool) {
	m := falseRefRe.FindStringSubmatch(s)
	if m == nil {
		return "", "", false
	}
	repo = strings.TrimSpace(m[1] + m[2])
	return repo, strings.ToUpper(m[3]), true
}

// ParseReport reads "learned: <text> [| files: a, b]" and "false: A7" lines
// anywhere in a response; the rest is ignored.
func ParseReport(text string) Report {
	var r Report
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if m := falseRe.FindStringSubmatch(line); m != nil {
			ref := strings.ToUpper(m[3])
			if repo := strings.TrimSpace(m[1] + m[2]); repo != "" {
				ref = repo + ":" + ref
			}
			r.False = append(r.False, ref)
			continue
		}
		m := learnedRe.FindStringSubmatch(line)
		if m == nil || len(r.Learnings) >= ReportMaxLearnings {
			continue
		}
		body, filesPart := m[1], ""
		if i := strings.Index(strings.ToLower(body), "| files:"); i >= 0 {
			body, filesPart = body[:i], body[i+len("| files:"):]
		}
		body = strings.TrimSpace(body)
		if rs := []rune(body); len(rs) > ReportMaxRunes {
			body = string(rs[:ReportMaxRunes])
		}
		if body == "" {
			continue
		}
		in := LearningIn{Text: body}
		for _, f := range strings.Split(filesPart, ",") {
			if c := CleanPath(f); c != "" {
				in.Files = append(in.Files, c)
			}
		}
		r.Learnings = append(r.Learnings, in)
	}
	return r
}

// Within keeps the files that are in allowed; with no files of its own it returns
// allowed as is (empty -> all). If every named file is outside allowed, it keeps
// the named ones (cleaned): it never attaches the learning to files it did not name.
func (l LearningIn) Within(allowed []string) []string {
	if len(l.Files) == 0 {
		return append([]string(nil), allowed...)
	}
	set := map[string]bool{}
	for _, a := range allowed {
		set[CleanPath(a)] = true
	}
	var out []string
	for _, f := range l.Files {
		if set[CleanPath(f)] {
			out = append(out, CleanPath(f))
		}
	}
	if len(out) == 0 {
		for _, f := range l.Files {
			out = append(out, CleanPath(f))
		}
	}
	return out
}
