package app

import (
	"context"
	"path"
	"runtime"
	"strconv"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/gate"
	"github.com/mananos/nxy.dev/internal/repomap"
)

// goos is the OS the path comparison follows; a variable so tests can pin it.
var goos = runtime.GOOS

func isEditTool(tool string) bool {
	switch tool {
	case "Edit", "Write", "NotebookEdit", "MultiEdit":
		return true
	}
	return false
}

// preToolEdit refuses edits inside the repo from the main thread while a feature is active; without
// a feature the write gate stops code files (docs and text go through).
func (h Hooks) preToolEdit(ctx context.Context, ev HookEvent) HookResponse {
	resp, active := h.preToolEditFeature(ctx, ev)
	if ev.FilePath != "" && ev.AgentID == "" && h.Gate != nil && ev.ProjectDir != "" {
		if active {
			if within(normPath(ev.ProjectDir, ""), normPath(ev.FilePath, ev.Cwd)) {
				h.recordEdit(ev, resp.Deny == "", "feature")
			}
		} else if r := h.writeGate(ctx, ev, ev.Tool, ev.FilePath); r.Deny != "" {
			return r
		}
	}
	if resp.Deny == "" && ev.FilePath != "" && (ev.AgentID == "" || ev.AgentType == roleImplementer || ev.AgentType == roleDocumenter) {
		h.markEdit(ctx, ev)
	}
	return resp
}

// recordEdit writes one ledger row for an edit of the main thread, with the measured context.
func (h Hooks) recordEdit(ev HookEvent, allow bool, reason string) {
	cfg := h.Gate.Settings(ev.ProjectDir)
	if !cfg.Enabled {
		return
	}
	h.Gate.Record(ev.ProjectDir, GateRecord{
		Ts: h.now().UnixMilli(), Tool: ev.Tool, Allow: allow, Reason: reason,
		ContextTokens: h.mainContext(ev), Threshold: cfg.ContextTokens,
	})
}

// mainContext is the measured context of the main thread, nil when unknown.
func (h Hooks) mainContext(ev HookEvent) *float64 {
	if h.Context == nil || ev.TranscriptPath == "" {
		return nil
	}
	if n, ok := h.Context.MainContextTokens(ev.TranscriptPath); ok {
		return &n
	}
	return nil
}

// codeExtensions is what the write gate calls code: the indexed languages plus the test globs of the runners.
func (h Hooks) codeExtensions(dir string) []string {
	var globs []string
	if h.Runners != nil {
		rows, _ := h.Runners.Rows(dir)
		for _, r := range rows {
			globs = append(globs, r.Tests...)
		}
	}
	return gate.CodeExtensions(repomap.IndexedExtensions(), globs)
}

// writeGate is the write gate without a feature: a code file inside the repo is refused unless the
// one-shot escape is armed. Called for the main thread only.
func (h Hooks) writeGate(ctx context.Context, ev HookEvent, tool, file string) HookResponse {
	return h.writeGateWith(ctx, ev, tool, file, nil)
}

// writeGateWith is writeGate where spent, when not nil, carries the escape across the targets of one
// command: the first code target consumes it, the rest ride on it.
func (h Hooks) writeGateWith(ctx context.Context, ev HookEvent, tool, file string, spent *bool) HookResponse {
	cfg := h.Gate.Settings(ev.ProjectDir)
	if !cfg.Enabled {
		return HookResponse{}
	}
	inside := within(normPath(ev.ProjectDir, ""), normPath(file, ev.Cwd))
	isCode := inside && gate.IsCodeFile(file, h.codeExtensions(ev.ProjectDir))
	escape := false
	if isCode {
		if spent != nil && *spent {
			escape = true
		} else if escape = h.Gate.Consume(ev.ProjectDir, GateEscapeEdit); escape && spent != nil {
			*spent = true
		}
	}
	d := gate.DecideWrite(gate.WriteInput{InsideRepo: inside, IsCode: isCode, EscapeActive: escape}, cfg)
	if inside {
		h.Gate.Record(ev.ProjectDir, GateRecord{
			Ts: h.now().UnixMilli(), Tool: tool, Allow: d.Allow, Reason: d.Reason,
			ContextTokens: h.mainContext(ev), Threshold: cfg.ContextTokens,
		})
	}
	if d.Allow {
		return HookResponse{}
	}
	return HookResponse{Deny: gate.WriteGateMessage(tool, file)}
}

func (h Hooks) preToolEditFeature(ctx context.Context, ev HookEvent) (HookResponse, bool) {
	if ev.FilePath == "" || (ev.AgentID != "" && ev.AgentType != roleImplementer && ev.AgentType != roleDocumenter) {
		return HookResponse{}, false
	}
	f, ok := h.activeFeature(ctx, ev)
	if !ok {
		return HookResponse{}, false
	}
	return h.featureEdit(ctx, ev, f), true
}

func (h Hooks) featureEdit(ctx context.Context, ev HookEvent, f domain.Feature) HookResponse {
	root := f.ProjectDir
	if root == "" {
		return HookResponse{}
	}
	if !within(normPath(root, ""), normPath(ev.FilePath, ev.Cwd)) {
		return HookResponse{}
	}
	if ev.AgentID != "" {
		if ev.AgentType == roleDocumenter {
			if f.State.Phase != domain.PhaseDocumenting {
				return HookResponse{}
			}
			if h.docsNamesFile(f, ev) {
				return HookResponse{AllowReason: "nxy: feature " + f.ID + " updates this doc"}
			}
			return HookResponse{Deny: "nxy: feature " + f.ID + " — the documenter only edits the docs nxy listed in its prompt" +
				docsWikiHint(f) + "; this file is not one of them"}
		}
		if f.State.Phase == domain.PhaseRunningBatch {
			h.noteWrite(ctx, f.ID)
			if ev.AgentType == roleImplementer && h.batchNamesFile(f, ev) {
				return HookResponse{AllowReason: "nxy: batch " + strconv.Itoa(f.State.Batch) + " of feature " + f.ID + " names this file"}
			}
		}
		if f.State.Phase == domain.PhaseFixing && ev.AgentType == roleImplementer && h.fixNamesFile(f, ev) {
			return HookResponse{AllowReason: "nxy: feature " + f.ID + " fixes a finding in this file"}
		}
		return HookResponse{}
	}
	return HookResponse{Deny: mainThreadWriteDeny(f)}
}

// batchNamesFile reports whether the edited file is one the current batch's change list
// names (path part of path:line, relative to the project dir) and stays inside it.
func (h Hooks) batchNamesFile(f domain.Feature, ev HookEvent) bool {
	if f.Plan == nil || f.State.Batch < 1 || f.State.Batch > len(f.Plan.Batches) {
		return false
	}
	root := normPath(f.ProjectDir, "")
	target := normPath(ev.FilePath, ev.Cwd)
	if !within(root, target) {
		return false
	}
	for _, c := range f.Plan.Batches[f.State.Batch-1].Change {
		p := changePath(c.At)
		if p == "" {
			continue
		}
		if h.Files != nil {
			p = h.Files.NativePath(p)
		}
		if normPath(p, f.ProjectDir) == target {
			return true
		}
	}
	return false
}

// fixNamesFile reports whether the edited file is the file of a finding the user chose to fix.
func (h Hooks) fixNamesFile(f domain.Feature, ev HookEvent) bool {
	if f.Review == nil {
		return false
	}
	root := normPath(f.ProjectDir, "")
	target := normPath(ev.FilePath, ev.Cwd)
	if !within(root, target) {
		return false
	}
	chosen := map[string]bool{}
	for _, id := range f.Review.Chosen {
		chosen[id] = true
	}
	for _, fd := range f.Review.Findings {
		if !chosen[fd.ID] || fd.File == "" {
			continue
		}
		p := fd.File
		if h.Files != nil {
			p = h.Files.NativePath(p)
		}
		if normPath(p, f.ProjectDir) == target {
			return true
		}
	}
	return false
}

// docsWikiHint is the part of the documenter's refusal that names the wiki folder, if any.
func docsWikiHint(f domain.Feature) string {
	if f.Docs == nil || f.Docs.WikiDir == "" {
		return ""
	}
	return " (or one new .md page inside " + f.Docs.WikiDir + "/)"
}

// docsNamesFile reports whether the edited file is one of the docs the feature found, or a new
// .md/.mdx page inside the folder the CI publishes to the wiki.
func (h Hooks) docsNamesFile(f domain.Feature, ev HookEvent) bool {
	if f.Docs == nil {
		return false
	}
	root := normPath(f.ProjectDir, "")
	target := normPath(ev.FilePath, ev.Cwd)
	if !within(root, target) {
		return false
	}
	for _, d := range f.Docs.Docs {
		p := d.Path
		if h.Files != nil {
			p = h.Files.NativePath(p)
		}
		if normPath(p, f.ProjectDir) == target {
			return true
		}
	}
	if f.Docs.WikiDir == "" || !isMarkdown(target) {
		return false
	}
	w := f.Docs.WikiDir
	if h.Files != nil {
		w = h.Files.NativePath(w)
	}
	wiki := normPath(w, f.ProjectDir)
	if !within(wiki, target) || target == wiki {
		return false
	}
	// A page that already exists is an edit of a doc nxy did not list.
	if h.Files != nil && h.Files.Exists(target) {
		return false
	}
	return true
}

func isMarkdown(p string) bool {
	p = strings.ToLower(p)
	return strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".mdx")
}

// changePath drops a trailing :line or :line-line from a change location.
func changePath(at string) string {
	at = strings.TrimSpace(at)
	if i := strings.LastIndex(at, ":"); i >= 0 {
		rest := at[i+1:]
		if strings.Trim(rest, "0123456789-,") == "" {
			at = at[:i]
		}
	}
	return at
}

func mainThreadWriteDeny(f domain.Feature) string {
	next := nextSituation(f)
	if f.Next.MainThread() && f.Next.Label() != "" {
		next = "next step " + next
	}
	return "nxy: feature " + f.ID + " is in progress — the main thread does not write code; " +
		next + ". To end the feature: `nxy feature stop`"
}

// normPath makes p absolute against cwd, with forward slashes, cleaned and (on Windows)
// lowercase. Git Bash and extended prefixes are already resolved by the host decoder
// (platform.NativePath).
func normPath(p, cwd string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if !isAbs(p) && cwd != "" {
		p = strings.ReplaceAll(cwd, "\\", "/") + "/" + p
	}
	p = path.Clean(p)
	if goos == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

func isLetter(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }

func isAbs(p string) bool {
	return strings.HasPrefix(p, "/") || (len(p) >= 2 && p[1] == ':' && isLetter(p[0]))
}

func within(root, p string) bool {
	return p == root || strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/")
}
