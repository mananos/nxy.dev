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
	False     []string // A<n> the agent found false
}

var (
	learnedRe = regexp.MustCompile(`(?i)^\s*learned:\s*(.+?)\s*$`)
	falseRe   = regexp.MustCompile(`(?i)^\s*false:\s*(A\d+)\b`)
)

// ParseReport reads "learned: <text> [| files: a, b]" and "false: A7" lines
// anywhere in a response; the rest is ignored.
func ParseReport(text string) Report {
	var r Report
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if m := falseRe.FindStringSubmatch(line); m != nil {
			r.False = append(r.False, strings.ToUpper(m[1]))
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
