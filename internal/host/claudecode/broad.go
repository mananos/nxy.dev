package claudecode

import (
	"os"
	"path/filepath"

	"github.com/mananos/nxy.dev/v2/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/v2/internal/platform"
	"github.com/mananos/nxy.dev/v2/internal/report"
	"github.com/mananos/nxy.dev/v2/internal/searchscope"
)

// isFileIn is fileCheck of hosts/claude-code/search-history.mjs: stat the path against the cwd of the
// call (an absolute path wins); when it cannot be stat'ed, whether it looks like a file.
func isFileIn(cwd, p string) bool {
	full := resolveLikeNode(platform.NativePath(cwd), platform.NativePath(p))
	if abs, err := filepath.Abs(full); err == nil {
		full = abs
	}
	st, err := os.Stat(full)
	if err != nil {
		return searchscope.LooksLikeFile(p)
	}
	return st.Mode().IsRegular()
}

// resolveLikeNode joins p to cwd the way path.resolve(cwd, p) does: an absolute p wins, and on
// Windows a p rooted without a volume (\src\a.go, /src/a.go) takes the volume of cwd instead of
// being joined under it.
func resolveLikeNode(cwd, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	if p != "" && os.IsPathSeparator(p[0]) && filepath.VolumeName(p) == "" {
		base := cwd
		if abs, err := filepath.Abs(base); err == nil {
			base = abs
		}
		return filepath.VolumeName(base) + p
	}
	return filepath.Join(cwd, p)
}

// BroadSearchesInTranscript counts the broad searches of the main transcript at path made at or above
// threshold. An unreadable file gives nil.
func BroadSearchesInTranscript(path string, threshold float64) []report.BroadRow {
	return broadSearchesSince(path, threshold, 0)
}

// ProjectBroadSearches is projectBroadSearches of the 1.x: the broad searches of every main session of
// the project at cwd since sinceMs, summarized. A threshold not above zero gives an empty summary.
func ProjectBroadSearches(cwd, projectsDir string, sinceMs int64, threshold float64) report.BroadSummary {
	if !(threshold > 0) {
		return report.BroadSummary{}
	}
	var rows []report.BroadRow
	for _, s := range ListSessions(ListOpts{Cwd: cwd, ProjectsDir: projectsDir, Since: sinceMs}) {
		rows = append(rows, broadSearchesSince(s.Path, threshold, float64(sinceMs))...)
	}
	return report.SummarizeBroadSearches(rows, threshold)
}

func broadSearchesSince(path string, threshold, sinceMs float64) []report.BroadRow {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var rows []report.BroadRow
	for _, r := range transcript.BroadSearches(string(data), threshold, sinceMs, isFileIn) {
		kind, _ := r.Kind.(string)
		rows = append(rows, report.BroadRow{Ts: r.Ts, Tool: r.Tool, Kind: kind, ContextTokens: r.ContextTokens})
	}
	return rows
}
