package app

import (
	"context"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/gate"
	"github.com/mananos/nxy.dev/v2/internal/repomap"
	"github.com/mananos/nxy.dev/v2/internal/repos"
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
	target := normPath(ev.FilePath, ev.Cwd)
	if !inFeatureRoots(f, target) {
		return h.undeclaredEdit(ctx, ev, f, target)
	}
	if ev.AgentID != "" && ev.AgentType == roleImplementer {
		if wt, ok := originalOfWorktree(f, target); ok {
			return HookResponse{Deny: "nxy: feature " + f.ID + " works in a worktree — edit in " + wt}
		}
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
			n, named := h.editBatch(f, ev, target)
			h.noteWrite(ctx, f.ID, n)
			if ev.AgentType == roleImplementer && named {
				return HookResponse{AllowReason: "nxy: batch " + strconv.Itoa(n) + " of feature " + f.ID + " names this file"}
			}
		}
		if f.State.Phase == domain.PhaseFixing && ev.AgentType == roleImplementer && h.fixNamesFile(f, ev) {
			return HookResponse{AllowReason: "nxy: feature " + f.ID + " fixes a finding in this file"}
		}
		return HookResponse{}
	}
	return HookResponse{Deny: mainThreadWriteDeny(f)}
}

// editBatch is the running batch an edit of target counts in: the one whose change list names the
// file; if none does, the running one whose repo holds it; else the batch in focus.
func (h Hooks) editBatch(f domain.Feature, ev HookEvent, target string) (int, bool) {
	running := f.State.Running()
	if len(running) == 0 {
		return f.State.Batch, h.batchNamesFile(f, ev, f.State.Batch)
	}
	for _, n := range running {
		if h.batchNamesFile(f, ev, n) {
			return n, true
		}
	}
	if len(running) > 1 {
		for _, n := range running {
			if d := batchDir(f, n); d != "" && within(normPath(d, ""), target) {
				return n, false
			}
		}
	}
	return f.State.Batch, false
}

// batchNamesFile reports whether the edited file is one the change list of batch n
// names (path part of path:line, relative to the project dir) and stays inside it.
func (h Hooks) batchNamesFile(f domain.Feature, ev HookEvent, n int) bool {
	if f.Plan == nil || n < 1 || n > len(f.Plan.Batches) {
		return false
	}
	target := normPath(ev.FilePath, ev.Cwd)
	if !inFeatureRoots(f, target) {
		return false
	}
	var names []string
	if len(f.Plan.Repos) > 0 {
		for _, r := range f.Repos {
			names = append(names, r.Name)
		}
	}
	dir := batchDir(f, n)
	for _, c := range f.Plan.Batches[n-1].Change {
		p := changePath(c.At)
		if p == "" {
			continue
		}
		d := dir
		if name, rel, ok := repos.Split(p, names); ok {
			r, _ := f.RepoByName(name)
			d, p = repoDir(r), rel
		}
		if h.Files != nil {
			p = h.Files.NativePath(p)
		}
		if normPath(p, d) == target {
			return true
		}
	}
	return false
}

// inFeatureRoots reports whether target (normalized) is inside the session folder or a repo of the feature.
func inFeatureRoots(f domain.Feature, target string) bool {
	if f.ProjectDir != "" && within(normPath(f.ProjectDir, ""), target) {
		return true
	}
	for _, r := range f.Repos {
		for _, d := range []string{r.Checkout, r.Root} {
			if d != "" && within(normPath(d, ""), target) {
				return true
			}
		}
	}
	return false
}

// originalOfWorktree is the worktree to edit in when target sits in the original checkout of a repo
// that the feature works on in a worktree.
func originalOfWorktree(f domain.Feature, target string) (string, bool) {
	if !f.UsesWorktrees() {
		return "", false
	}
	for _, r := range f.Repos {
		if r.Root == "" || r.Checkout == "" || normPath(r.Root, "") == normPath(r.Checkout, "") {
			continue
		}
		if within(normPath(r.Root, ""), target) && !within(normPath(r.Checkout, ""), target) {
			return r.Checkout, true
		}
	}
	return "", false
}

// undeclaredEdit handles an edit of the implementer outside the feature: when it lands in another git
// repo, the repo is added to the feature once (Undeclared, from its HEAD) and the edit is allowed with
// a notice; it takes the checkout only when no other feature holds it.
func (h Hooks) undeclaredEdit(ctx context.Context, ev HookEvent, f domain.Feature, target string) HookResponse {
	if ev.AgentID == "" || ev.AgentType != roleImplementer || h.Repos == nil || h.Store == nil || f.ProjectDir == "" {
		return HookResponse{}
	}
	if f.State.Phase != domain.PhaseRunningBatch && f.State.Phase != domain.PhaseFixing {
		return HookResponse{}
	}
	file := ev.FilePath
	if !filepath.IsAbs(file) && ev.Cwd != "" {
		file = filepath.Join(ev.Cwd, file)
	}
	root, ok := h.Repos.Root(filepath.Dir(file))
	if !ok || root == "" || within(normPath(root, ""), normPath(f.ProjectDir, "")) {
		return HookResponse{}
	}
	checkout := h.Repos.Checkout(root)
	free := false
	if other, found, err := h.Store.ActiveFeature(ctx, checkout); err == nil && (!found || other.ID == f.ID) {
		free = true
	}
	name := filepath.Base(root)
	_, applied, err := h.mutate(ctx, f.ID, func(x *domain.Feature) bool {
		for _, r := range x.Repos {
			if normPath(r.Root, "") == normPath(root, "") {
				return false
			}
		}
		if len(x.Repos) == 0 {
			prim := x.RepoList()[0]
			prim.Name = filepath.Base(prim.Root)
			x.Repos = []domain.FeatureRepo{prim}
		}
		r := domain.FeatureRepo{Name: name, Key: h.Repos.Key(root), Root: root, Checkout: root, Undeclared: true}
		if h.Changes != nil {
			r.Base = h.Changes.Head(root)
		}
		x.Repos = append(x.Repos, r)
		if free && !x.Paused {
			x.Holds = append(slices.Clone(x.Holds), checkout)
		}
		return true
	})
	if err != nil || !applied {
		return HookResponse{}
	}
	return HookResponse{AllowReason: "nxy: feature " + f.ID + " — " + name + " is a repo outside the plan; added to the feature as undeclared"}
}

// fixNamesFile reports whether the edited file is the file of a finding the user chose to fix.
func (h Hooks) fixNamesFile(f domain.Feature, ev HookEvent) bool {
	if f.Review == nil {
		return false
	}
	root := normPath(f.ProjectDir, "")
	target := normPath(ev.FilePath, ev.Cwd)
	multi := len(f.Repos) > 1
	if multi {
		if !inFeatureRoots(f, target) {
			return false
		}
	} else if !within(root, target) {
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
		p, dir := fd.File, f.ProjectDir
		if multi {
			// A finding of a multi-repo review names its file as "<repo>/<path>".
			if name, rel, ok := repos.Split(p, featureRepoNames(f)); ok {
				r, _ := f.RepoByName(name)
				p, dir = rel, repoDir(r)
			}
		}
		if h.Files != nil {
			p = h.Files.NativePath(p)
		}
		if normPath(p, dir) == target {
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
	target := normPath(ev.FilePath, ev.Cwd)
	if !inFeatureRoots(f, target) {
		return false
	}
	for _, d := range f.Docs.Docs {
		if h.docsAbs(f, d.Path) == target {
			return true
		}
	}
	if f.Docs.WikiDir == "" || !isMarkdown(target) {
		return false
	}
	wiki := h.docsAbs(f, f.Docs.WikiDir)
	if !within(wiki, target) || target == wiki {
		return false
	}
	// A page that already exists is an edit of a doc nxy did not list.
	if h.Files != nil && h.Files.Exists(target) {
		return false
	}
	return true
}

// docsAbs is the normalized absolute path of a doc (or the wiki folder) of f.Docs: with several repos
// it carries the repo name ("<name>/<path>"), otherwise it is relative to the repo of the feature.
func (h Hooks) docsAbs(f domain.Feature, p string) string {
	dir := repoDir(f.RepoList()[0])
	if dir == "" {
		dir = f.ProjectDir
	}
	if len(f.Repos) > 1 {
		if name, rel, ok := repos.Split(p, featureRepoNames(f)); ok {
			r, _ := f.RepoByName(name)
			dir, p = repoDir(r), rel
		}
	}
	if h.Files != nil {
		p = h.Files.NativePath(p)
	}
	return normPath(p, dir)
}

// docsAbsolute is f with the docs of several repos as absolute paths (the documenter edits in the
// checkouts); with one repo it is f itself.
func (h Hooks) docsAbsolute(f domain.Feature) domain.Feature {
	if f.Docs == nil || len(f.Repos) <= 1 {
		return f
	}
	d := *f.Docs
	d.Docs = make([]domain.DocFile, len(f.Docs.Docs))
	for i, x := range f.Docs.Docs {
		x.Path = h.docsAbs(f, x.Path)
		d.Docs[i] = x
	}
	if d.WikiDir != "" {
		d.WikiDir = h.docsAbs(f, d.WikiDir)
	}
	f.Docs = &d
	return f
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
