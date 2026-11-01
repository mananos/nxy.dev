package claudecode

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/host/claudecode/transcript"
)

// ListOpts select the sessions ListSessions returns.
type ListOpts struct {
	// Cwd limits the listing to the project of that working directory, unless All is set.
	Cwd string
	// All lists every project; so does an empty Cwd.
	All bool
	// ProjectsDir overrides ~/.claude/projects.
	ProjectsDir string
	// Since drops sessions last modified before this epoch ms; 0 keeps everything.
	Since int64
}

// ProjectsDir is Claude Code's transcript root (~/.claude/projects) unless overridden. Without a
// home directory and without an override it is "" and there are no sessions.
func ProjectsDir(override string) string {
	if override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// mtimeMs is the modification time as Node's stat.mtimeMs: fractional epoch milliseconds.
func mtimeMs(t time.Time) float64 {
	return float64(t.Unix())*1e3 + float64(t.Nanosecond())/1e6
}

// ListSessions returns the sessions found, newest first. A transcript that cannot be stat'ed (a
// dangling entry, a file removed after the listing) is skipped; the 1.x threw there.
func ListSessions(opts ListOpts) []transcript.SessionRef {
	out := []transcript.SessionRef{}
	root := ProjectsDir(opts.ProjectsDir)
	if root == "" {
		return out
	}
	var projects []string
	if opts.All || opts.Cwd == "" {
		entries, err := os.ReadDir(root)
		if err != nil {
			return out
		}
		for _, e := range entries {
			projects = append(projects, e.Name())
		}
	} else {
		if _, err := os.Stat(root); err != nil {
			return out
		}
		projects = []string{transcript.ProjectSlug(opts.Cwd)}
	}
	for _, project := range projects {
		dir := filepath.Join(root, project)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			st, err := os.Stat(p)
			if err != nil {
				continue
			}
			mtime := mtimeMs(st.ModTime())
			if opts.Since != 0 && mtime < float64(opts.Since) {
				continue
			}
			id := strings.TrimSuffix(e.Name(), ".jsonl")
			out = append(out, transcript.SessionRef{
				SessionID: id,
				Path:      p,
				Project:   project,
				AgentsDir: filepath.Join(dir, id, "subagents"),
				MtimeMs:   mtime,
				Size:      st.Size(),
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].MtimeMs > out[j].MtimeMs })
	return out
}

// ResolveSession finds a session: "last" is the most recent of the project (of Cwd); anything
// else is an id or a unique id prefix searched in every project. nil when there is none.
func ResolveSession(selector string, opts ListOpts) *transcript.SessionRef {
	opts.All = opts.All || selector != "last"
	sessions := ListSessions(opts)
	if selector == "last" {
		if len(sessions) == 0 {
			return nil
		}
		return &sessions[0]
	}
	for i := range sessions {
		if sessions[i].SessionID == selector {
			return &sessions[i]
		}
	}
	for i := range sessions {
		if strings.HasPrefix(sessions[i].SessionID, selector) {
			return &sessions[i]
		}
	}
	return nil
}

// SubagentFiles lists the subagent transcripts of a session (name order) with their decoded
// .meta.json (nil when absent or malformed).
func SubagentFiles(ref transcript.SessionRef) []transcript.Subagent {
	out := []transcript.Subagent{}
	entries, err := os.ReadDir(ref.AgentsDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		n := e.Name()
		if !strings.HasPrefix(n, "agent-") || !strings.HasSuffix(n, ".jsonl") {
			continue
		}
		p := filepath.Join(ref.AgentsDir, n)
		var meta any
		if raw, err := os.ReadFile(strings.TrimSuffix(p, ".jsonl") + ".meta.json"); err == nil {
			if err := json.Unmarshal(raw, &meta); err != nil {
				meta = nil
			}
		}
		out = append(out, transcript.Subagent{
			Path:    p,
			AgentID: n[len("agent-") : len(n)-len(".jsonl")],
			Meta:    meta,
		})
	}
	return out
}

// IncrementalOpts are the options of ReadIncremental.
type IncrementalOpts struct {
	// TurnSinceTs is the parent's turn start (epoch ms) when reading a subagent transcript; nil for the main one.
	TurnSinceTs *int64
}

// ReadIncremental reads only the bytes appended since st.Offset and folds them into st. A file
// that cannot be opened leaves st unchanged (after the turn reset, as the 1.x).
func ReadIncremental(path string, st *transcript.IncrementalState, opts IncrementalOpts) {
	st.StartTurnAt(opts.TurnSinceTs)
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return
	}
	foldNew(f, info.Size(), st, opts.TurnSinceTs)
}

// foldNew folds the bytes of r between st.Offset and size into st. A read error other than
// io.EOF (a file that shrank under us) leaves st.Offset where it was, so the next call retries.
func foldNew(r io.ReaderAt, size int64, st *transcript.IncrementalState, since *int64) {
	st.Rewind(size)
	n := size - st.Offset
	if n <= 0 {
		return
	}
	buf := make([]byte, n)
	got, err := r.ReadAt(buf, st.Offset)
	if err != nil && err != io.EOF {
		return
	}
	st.Offset += int64(got)
	st.Fold(buf[:got], since)
}

// defaultTailBytes is how much of the end of a transcript LastCacheTTL reads.
const defaultTailBytes = 524288

// LastCacheTTL is the cache TTL the main thread writes with ("5m" or "1h"; ok=false, the 1.x null,
// when the file is unreadable or the tail has no cache write), read from at most tailBytes
// (<= 0: 512 KiB) at the end of path.
func LastCacheTTL(path string, tailBytes int64) (ttl string, ok bool) {
	if path == "" {
		return "", false
	}
	if tailBytes <= 0 {
		tailBytes = defaultTailBytes
	}
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", false
	}
	size := info.Size()
	n := size
	if tailBytes < n {
		n = tailBytes
	}
	buf := make([]byte, n)
	got, _ := f.ReadAt(buf, size-n)
	return transcript.LastCacheTTL(buf[:got], size > n)
}

// ParseSession reads the session and its subagents from disk and aggregates them.
func ParseSession(ref transcript.SessionRef, opts transcript.SessionOpts) transcript.Session {
	var subs []transcript.Subagent
	if opts.IncludeSubagents == nil || *opts.IncludeSubagents {
		subs = SubagentFiles(ref)
	}
	return transcript.ParseSession(ref, subs, os.ReadFile, opts)
}
