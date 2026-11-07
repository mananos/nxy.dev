package legacy

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/mananos/nxy.dev/internal/memory"
)

// CutMark closes a note body that did not fit.
const CutMark = "(cortado al importar; completo en la 1.x)"

var (
	sectionRE = regexp.MustCompile(`^##\s`)
	planRE    = regexp.MustCompile(`(?i)^##\s*Plan\s*$`)
	goalRE    = regexp.MustCompile(`(?i)^Goal:[ \t]*(.*)$`)
	routeRE   = regexp.MustCompile(`(?i)^route:\s*[A-Za-z-]+`)
	headerRE  = regexp.MustCompile(`^handoff · .+ · updated \d{4}-\d\d-\d\d \d\d:\d\d`)
	nameRE    = regexp.MustCompile(`^##\s*(.*?)\s*$`)
)

const progressPrefix = "Progress (recorded by nxy, not by the model): "

type section struct {
	name  string // lower-cased title, "" for the text before the first "## "
	lines []string
}

// NoteBody shortens the body of a 1.x handoff to what a v2 note holds: the plan's
// Goal first, then Next, Done, Decisions, Files and the other sections in their
// order. The plan itself, the route line and the lines `handoff show` derives
// stay out. It is built line by line while it fits in memory.NoteMaxBytes; if
// something is left out the last line says so.
func NoteBody(body string) string {
	text := strings.ReplaceAll(body, "\r\n", "\n")
	var secs []section
	cur := section{}
	for _, l := range strings.Split(text, "\n") {
		if headerRE.MatchString(l) || strings.HasPrefix(l, progressPrefix) {
			continue
		}
		if sectionRE.MatchString(l) {
			secs = append(secs, cur)
			cur = section{name: strings.ToLower(nameRE.FindStringSubmatch(l)[1])}
		}
		cur.lines = append(cur.lines, l)
	}
	secs = append(secs, cur)

	goal := ""
	byName := map[string]*section{}
	var rest []*section
	for i := range secs {
		s := &secs[i]
		if planRE.MatchString(firstLine(s)) {
			for _, l := range s.lines {
				if m := goalRE.FindStringSubmatch(l); m != nil {
					goal = strings.TrimSpace(m[1])
					break
				}
			}
			continue
		}
		if _, dup := byName[s.name]; s.name != "" && !dup && isKnown(s.name) {
			byName[s.name] = s
			continue
		}
		rest = append(rest, s)
	}

	var lines []string
	if goal != "" {
		lines = append(lines, "Goal: "+goal)
	}
	emit := func(s *section) {
		ls := dropRoute(s.lines)
		ls = trimBlank(ls)
		if len(ls) == 0 {
			return
		}
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
		lines = append(lines, ls...)
	}
	// the text before the first header keeps its place among the others
	var pre []*section
	var others []*section
	for _, s := range rest {
		if s.name == "" {
			pre = append(pre, s)
		} else {
			others = append(others, s)
		}
	}
	for _, s := range pre {
		emit(s)
	}
	for _, n := range []string{"next", "done", "decisions", "files"} {
		if s := byName[n]; s != nil {
			emit(s)
		}
	}
	for _, s := range others {
		emit(s)
	}
	return fit(lines)
}

func isKnown(name string) bool {
	switch name {
	case "next", "done", "decisions", "files":
		return true
	}
	return false
}

func firstLine(s *section) string {
	if len(s.lines) == 0 {
		return ""
	}
	return s.lines[0]
}

func dropRoute(ls []string) []string {
	out := make([]string, 0, len(ls))
	for _, l := range ls {
		if routeRE.MatchString(l) {
			continue
		}
		out = append(out, l)
	}
	return out
}

func trimBlank(ls []string) []string {
	for len(ls) > 0 && strings.TrimSpace(ls[0]) == "" {
		ls = ls[1:]
	}
	for len(ls) > 0 && strings.TrimSpace(ls[len(ls)-1]) == "" {
		ls = ls[:len(ls)-1]
	}
	return ls
}

// fit joins the lines while they fit; a line that does not is cut at a rune
// boundary and ends the body, followed by CutMark.
func fit(lines []string) string {
	full := strings.Join(lines, "\n")
	if len(full) <= memory.NoteMaxBytes {
		return full
	}
	budget := memory.NoteMaxBytes - len("\n"+CutMark)
	var out strings.Builder
	for i, l := range lines {
		sep := 0
		if i > 0 {
			sep = 1
		}
		if out.Len()+sep+len(l) <= budget {
			if sep == 1 {
				out.WriteByte('\n')
			}
			out.WriteString(l)
			continue
		}
		if left := budget - out.Len() - sep; left > 0 {
			if sep == 1 {
				out.WriteByte('\n')
			}
			out.WriteString(cutRunes(l, left))
		}
		break
	}
	if out.Len() > 0 {
		out.WriteByte('\n')
	}
	out.WriteString(CutMark)
	return out.String()
}

// cutRunes keeps at most n bytes of s, never splitting a rune.
func cutRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
