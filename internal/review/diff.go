package review

import (
	"regexp"
	"strconv"
	"strings"
)

// MaxDiffLines is how many diff lines a reviewer gets in its prompt.
const MaxDiffLines = 3000

// FileDiff is the diff of one changed file against the approved base.
type FileDiff struct {
	Path   string
	Status string // "M", "A", "D"
	Text   string
}

var hunkRE = regexp.MustCompile(`(?m)^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// Hunks returns the line ranges, in the new file, of every hunk header of a diff.
func Hunks(text string) [][2]int {
	var out [][2]int
	for _, m := range hunkRE.FindAllStringSubmatch(text, -1) {
		start, _ := strconv.Atoi(m[1])
		n := 1
		if m[2] != "" {
			n, _ = strconv.Atoi(m[2])
		}
		end := start + n - 1
		if end < start {
			end = start
		}
		out = append(out, [2]int{start, end})
	}
	return out
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(strings.TrimRight(s, "\n"), "\n") + 1
}

// Cut keeps the diffs that fit in max lines, code first and tests last. A file
// that does not fit whole is left out; its path is returned so the reviewer
// reads it on its own. max <= 0 means MaxDiffLines.
func Cut(files []FileDiff, max int) (kept []FileDiff, cut []string) {
	if max <= 0 {
		max = MaxDiffLines
	}
	var code, tests []FileDiff
	for _, f := range files {
		if IsTestPath(f.Path) {
			tests = append(tests, f)
		} else {
			code = append(code, f)
		}
	}
	total := 0
	for _, f := range append(code, tests...) {
		n := lineCount(f.Text)
		if total+n > max {
			cut = append(cut, f.Path)
			continue
		}
		total += n
		kept = append(kept, f)
	}
	return kept, cut
}
