package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/mananos/nxy.dev/internal/app"
	"github.com/mananos/nxy.dev/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/internal/metrics"
	"github.com/mananos/nxy.dev/internal/pricing"
	"github.com/mananos/nxy.dev/internal/views"
)

// tail is what one transcript file says so far. It is fed with only the bytes that grew since the
// last read, so a long-lived process (the TUI) never parses a file twice.
type tail struct {
	offset   int64 // end of the last complete line read
	since    int64
	calls    []metrics.Call
	callIdx  map[string]int // Call.Key -> index in calls (a later chunk keeps the last)
	toolKeys []string
	toolN    map[string]int
	lastTool *views.ToolUse
	firstTs  int64
	lastTs   int64
}

func (t *tail) empty() bool { return len(t.calls) == 0 }

// usage is the file's calls added up.
func (t *tail) usage() metrics.Finished {
	b := metrics.NewBucket()
	for _, c := range t.calls {
		b.Add(c)
	}
	return b.Finish()
}

// modelEffort is the last real model of the file and the last effort it named.
func (t *tail) modelEffort() (model, effort string) {
	for _, c := range t.calls {
		if c.Model != "" && !pricing.IsSyntheticModel(c.Model) {
			model = c.Model
		}
		if c.Effort != nil && *c.Effort != "" {
			effort = *c.Effort
		}
	}
	return model, effort
}

// toolCounts is how many times each tool ran, in first-use order.
func (t *tail) toolCounts() []views.ToolCount {
	out := []views.ToolCount{}
	for _, k := range t.toolKeys {
		out = append(out, views.ToolCount{Name: k, Count: t.toolN[k]})
	}
	return out
}

// tailOpts say how one file is read: a subagent file names its agent, and Seen is shared by the
// files of a session so a line copied between them counts once.
type tailOpts struct {
	AgentID string
	Since   int64
	Seen    *transcript.UUIDSet
}

// tails keeps the offset of every transcript read, in process memory. A fresh tails (`nxy view`)
// reads each file whole once.
type tails struct {
	mu    sync.Mutex
	files map[string]*tail
	seen  map[string]*transcript.UUIDSet
	// parsed caches whole-session parses by path, size and mtime (stats and trend).
	parsed map[string]transcript.Session
}

func newTails() *tails {
	return &tails{files: map[string]*tail{}, seen: map[string]*transcript.UUIDSet{}, parsed: map[string]transcript.Session{}}
}

// seenOf is the uuid set of a session.
func (ts *tails) seenOf(session string) *transcript.UUIDSet {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	s, ok := ts.seen[session]
	if !ok {
		s = transcript.NewUUIDSet()
		ts.seen[session] = s
	}
	return s
}

// read brings the file up to date: only the new complete lines are parsed. A file that shrank (it
// was rewritten) is read again from the start. A missing file gives an empty tail.
func (ts *tails) read(path string, o tailOpts) *tail {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	key := fmt.Sprintf("%s|%d", path, o.Since)
	t := ts.files[key]
	if t == nil {
		t = &tail{since: o.Since, callIdx: map[string]int{}, toolN: map[string]int{}}
		ts.files[key] = t
	}
	f, err := os.Open(path)
	if err != nil {
		return t
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return t
	}
	if info.Size() < t.offset {
		*t = tail{since: o.Since, callIdx: map[string]int{}, toolN: map[string]int{}}
		o.Seen = nil // lines of the old content were marked as seen
	}
	if info.Size() == t.offset {
		return t
	}
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return t
	}
	buf, err := io.ReadAll(io.LimitReader(f, info.Size()-t.offset))
	if err != nil {
		return t
	}
	end := bytes.LastIndexByte(buf, '\n')
	if end < 0 {
		return t // a line still being written
	}
	chunk := buf[:end+1]
	t.offset += int64(len(chunk))

	fo := transcript.FileOpts{Since: o.Since, Seen: o.Seen}
	if o.AgentID != "" {
		id := o.AgentID
		fo.AgentID = &id
	}
	res := transcript.ParseFile(path, chunk, true, fo)
	for _, c := range res.Calls {
		if i, ok := t.callIdx[c.Key]; ok && c.Key != "" {
			t.calls[i] = c
			continue
		}
		t.callIdx[c.Key] = len(t.calls)
		t.calls = append(t.calls, c)
	}
	for _, k := range res.Tools.Calls.Keys {
		if _, ok := t.toolN[k]; !ok {
			t.toolKeys = append(t.toolKeys, k)
		}
		t.toolN[k] += res.Tools.Calls.Values[k]
	}
	if res.FirstTs != nil && (t.firstTs == 0 || *res.FirstTs < t.firstTs) {
		t.firstTs = *res.FirstTs
	}
	if res.LastTs != nil && *res.LastTs > t.lastTs {
		t.lastTs = *res.LastTs
	}
	if name, arg, ok := transcript.LastToolUse(chunk); ok {
		t.lastTool = &views.ToolUse{Name: name, Arg: arg}
	}
	return t
}

// cachedTranscripts wraps the transcript reader so a session parsed again with the same file size
// and mtime is not parsed twice (stats and trend read the same sessions).
type cachedTranscripts struct {
	app.Transcripts
	ts *tails
}

func (c cachedTranscripts) ParseSession(ref transcript.SessionRef, o transcript.SessionOpts) transcript.Session {
	inc, thr := true, 0.0
	if o.IncludeSubagents != nil {
		inc = *o.IncludeSubagents
	}
	if o.CacheBreakThreshold != nil {
		thr = *o.CacheBreakThreshold
	}
	key := fmt.Sprintf("%s|%d|%v|%d|%v|%v|%v", ref.Path, ref.Size, ref.MtimeMs, o.Since, o.Timing, inc, thr)
	c.ts.mu.Lock()
	s, ok := c.ts.parsed[key]
	c.ts.mu.Unlock()
	if ok {
		return s
	}
	s = c.Transcripts.ParseSession(ref, o)
	c.ts.mu.Lock()
	c.ts.parsed[key] = s
	c.ts.mu.Unlock()
	return s
}
