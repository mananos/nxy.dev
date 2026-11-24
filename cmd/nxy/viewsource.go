package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	nxydev "github.com/mananos/nxy.dev/v2"
	"github.com/mananos/nxy.dev/v2/internal/app"
	"github.com/mananos/nxy.dev/v2/internal/configedit"
	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/host/claudecode"
	"github.com/mananos/nxy.dev/v2/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/v2/internal/memory"
	"github.com/mananos/nxy.dev/v2/internal/metrics"
	"github.com/mananos/nxy.dev/v2/internal/platform"
	"github.com/mananos/nxy.dev/v2/internal/report"
	"github.com/mananos/nxy.dev/v2/internal/setup"
	"github.com/mananos/nxy.dev/v2/internal/store/sqlite"
	"github.com/mananos/nxy.dev/v2/internal/tools"
	"github.com/mananos/nxy.dev/v2/internal/version"
	"github.com/mananos/nxy.dev/v2/internal/views"
)

// viewSetupState and viewBinary are the machine-dependent parts of Config; tests replace them.
var (
	viewSetupState = setupState
	viewBinary     = realBinary
	viewWorktrees  = platform.ListWorktrees
)

// viewToolset is what the plan, file and engineer screens ask of the machine: the code index, a
// search, d2. The defaults are real; tests replace viewTools as a whole.
type viewToolset struct {
	// Indexed says whether codegraph is installed and root has an index.
	Indexed func(root string) bool
	// Affected is the tests the index says depend on files (query only, never builds the index).
	Affected func(ctx context.Context, root string, files []string) ([]string, bool)
	// RgFound says whether rg is installed.
	RgFound func() bool
	// Grep is the rows (one per file, at most 30) where word appears in root.
	Grep     func(ctx context.Context, root, word string) []tools.RgRow
	FindD2   func() (path, version string, ok bool)
	RenderD2 func(ctx context.Context, bin, src string) ([]string, error)
}

func realViewTools(getenv func(string) string) viewToolset {
	var (
		once sync.Once
		g    *tools.Graph
	)
	graph := func() *tools.Graph {
		once.Do(func() { g = tools.NewGraph(getenv) })
		return g
	}
	return viewToolset{
		Indexed: func(root string) bool { return len(graph().Argv) > 0 && tools.HasIndex(root, getenv) },
		Affected: func(ctx context.Context, root string, files []string) ([]string, bool) {
			return graph().AffectedIndexed(ctx, root, files)
		},
		RgFound: func() bool { _, ok := tools.FindRg(getenv); return ok },
		Grep: func(ctx context.Context, root, word string) []tools.RgRow {
			r := tools.RgSearch(ctx, root, word, tools.RgOpts{MaxRows: 30, MaxPerFile: 1, Word: true, Getenv: getenv})
			return r.Rows
		},
		FindD2:   func() (string, string, bool) { return tools.FindD2(getenv) },
		RenderD2: tools.RenderD2,
	}
}

// viewTools is the replacement tests install; the zero value means the real tools of the source.
var viewTools viewToolset

func (t viewToolset) isSet() bool {
	return t.Indexed != nil || t.Affected != nil || t.RgFound != nil || t.Grep != nil || t.FindD2 != nil || t.RenderD2 != nil
}

// tools is the toolset of this source: the test replacement, else the real one over its getenv.
func (v *viewSource) tools() viewToolset {
	if viewTools.isSet() {
		return viewTools
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.ts == nil {
		ts := realViewTools(v.getenv)
		v.ts = &ts
	}
	return *v.ts
}

// viewSource builds the views from the real data of the repo at cwd: nxy.db, the transcripts of
// the sessions linked to each feature, the config and the tools. It implements views.Source.
type viewSource struct {
	store  *sqlite.Store
	usage  app.Usage
	tails  *tails
	now    func() time.Time
	getenv func(string) string
	cwd    string
	loc    *time.Location
	done   func()
	// todayApart: the cockpit leaves hoy $ out (the TUI asks CostToday on its own, with Gasto).
	todayApart bool

	mu      sync.Mutex
	impacts map[string]planImpact   // by feature id, plan revision and HEAD of each repo
	probes  map[string]fileProbeHit // tests and importers by repo dir, path, source and HEAD
	d2      *d2Cache
	ts      *viewToolset // real tools over getenv, built on first use
}

// fileProbeHit is what probe found for one file at one HEAD.
type fileProbeHit struct {
	tests []string
	refs  []tools.RgRow
}

var _ views.Source = (*viewSource)(nil)

// newViewSource wires a source over an open store. A fresh one reads every transcript whole once;
// a long-lived one (the TUI) keeps the offsets and reads only what grew.
func newViewSource(store *sqlite.Store, getenv func(string) string, cwd string, now func() time.Time, loc *time.Location) *viewSource {
	if loc == nil {
		loc = time.Local
	}
	ts := newTails()
	usage, done := newUsage(getenv, func() string { return cwd }, now, loc)
	usage.Transcripts = cachedTranscripts{Transcripts: usage.Transcripts, ts: ts}
	if store != nil {
		usage.SessionCache = storeSessionCache{store: store}
	}
	return &viewSource{store: store, usage: usage, tails: ts, now: now, getenv: getenv, cwd: cwd, loc: loc, done: done}
}

// Close frees what the source opened (not the store, which the caller owns).
func (v *viewSource) Close() {
	if v.done != nil {
		v.done()
	}
}

func (v *viewSource) lookup() app.SessionLookup {
	return app.SessionLookup{Cwd: v.cwd, All: true, ProjectsDir: report.SettingsFrom(v.usage.Config(v.cwd)).ProjectsDir}
}

func (v *viewSource) midnight() int64 {
	n := v.now().In(v.loc)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, v.loc).UnixMilli()
}

// ActiveFeatureID is the feature that holds the folder, when there is one.
func (v *viewSource) ActiveFeatureID(ctx context.Context) (string, bool, error) {
	f, ok, err := v.store.ActiveFeature(ctx, repoKeys{}.Checkout(v.cwd))
	return f.ID, ok, err
}

// featData is everything read for one feature.
type featData struct {
	row   sqlite.FeatureRow
	runs  []domain.AgentRun // finished (a row of its own, or ended without one)
	live  []views.LiveAgent
	main  *views.MainThread
	files map[string]string // agent id -> its transcript
	usd   float64
	est   bool
}

func (fd featData) featureIn(now time.Time) views.FeatureIn {
	f := fd.row.Feature
	// Forward slashes on Windows only (ToSlash is a no-op elsewhere: a backslash in a Linux path is real).
	f.ProjectDir = filepath.ToSlash(f.ProjectDir)
	if len(f.Repos) > 0 {
		repos := make([]domain.FeatureRepo, len(f.Repos))
		for i, r := range f.Repos {
			r.Root, r.Checkout = filepath.ToSlash(r.Root), filepath.ToSlash(r.Checkout)
			repos[i] = r
		}
		f.Repos = repos
	}
	return views.FeatureIn{Feature: f, CreatedAt: fd.row.CreatedAt, UpdatedAt: fd.row.UpdatedAt, CostUSD: fd.usd, CostEstimated: fd.est, Now: now}
}

var batchInPrompt = regexp.MustCompile(`(?i)\bbatch\s+(\d+)`)

var nxyRoles = map[string]bool{"planner": true, "scout": true, "implementer": true, "reviewer": true, "reviewer-quick": true, "refuter": true, "documenter": true}

// dispatching says whether the phase of the feature is waiting for an agent of that role.
func dispatching(f domain.Feature, role string, batch int) bool {
	switch f.State.Phase {
	case domain.PhasePlanning:
		return role == "planner" || role == "scout"
	case domain.PhaseRunningBatch:
		for _, n := range f.State.Running() {
			if n == batch {
				return role == "implementer"
			}
		}
	case domain.PhaseReviewing:
		return role == "reviewer" || role == "reviewer-quick"
	case domain.PhaseRefuting:
		return role == "refuter"
	case domain.PhaseFixing, domain.PhaseAffectedFix:
		return role == "fixer"
	case domain.PhaseDocumenting:
		return role == "documenter"
	}
	return false
}

// readHead and readTail read at most n bytes of the start or the end of a file (the tail without
// the line it cuts).
func readHead(path string, n int64) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, n))
	return b
}

func readTail(path string, n int64) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	off := info.Size() - n
	if off < 0 {
		off = 0
	}
	buf := make([]byte, info.Size()-off)
	got, _ := f.ReadAt(buf, off)
	buf = buf[:got]
	if off > 0 {
		if i := strings.IndexByte(string(buf), '\n'); i >= 0 {
			buf = buf[i+1:]
		}
	}
	return buf
}

// featureData reads the runs of the feature, its main thread (from its linked sessions, cut at the
// start of the feature) and its subagents that have no row yet.
func (v *viewSource) featureData(ctx context.Context, row sqlite.FeatureRow) (featData, error) {
	f := row.Feature
	fd := featData{row: row, files: map[string]string{}}
	runs, err := v.store.AgentRuns(ctx, f.ID)
	if err != nil {
		return fd, err
	}
	fd.runs = runs
	have := map[string]bool{}
	for _, r := range runs {
		have[r.AgentID] = true
	}
	sessions, err := v.store.SessionsOfFeature(ctx, f.ID)
	if err != nil {
		return fd, err
	}

	mainBucket := metrics.NewBucket()
	var mainTool *views.ToolUse
	var toolKeys []string
	toolN := map[string]int{}
	model, effort := "", ""
	haveMain := false
	for _, s := range sessions {
		ref := v.usage.Transcripts.ResolveSession(s.ID, v.lookup())
		if ref == nil {
			continue
		}
		seen := v.tails.seenOf(s.ID)
		mt := v.tails.read(ref.Path, tailOpts{Since: row.CreatedAt, Seen: seen})
		if !mt.empty() {
			haveMain = true
			for _, c := range mt.calls {
				mainBucket.Add(c)
			}
			for _, tc := range mt.toolCounts() {
				if _, ok := toolN[tc.Name]; !ok {
					toolKeys = append(toolKeys, tc.Name)
				}
				toolN[tc.Name] += tc.Count
			}
			if m, e := mt.modelEffort(); m != "" {
				model, effort = m, e
			}
			if mt.lastTool != nil {
				mainTool = mt.lastTool
			}
		}
		for _, sub := range claudecode.SubagentFiles(*ref) {
			fd.files[sub.AgentID] = sub.Path
			if have[sub.AgentID] {
				continue
			}
			meta, _ := sub.Meta.(map[string]any)
			agentType, _ := meta["agentType"].(string)
			if !strings.HasPrefix(agentType, "nxy:") || !nxyRoles[strings.TrimPrefix(agentType, "nxy:")] {
				continue
			}
			if info, err := os.Stat(sub.Path); err != nil || info.ModTime().UnixMilli() < row.CreatedAt {
				continue
			}
			at := v.tails.read(sub.Path, tailOpts{AgentID: sub.AgentID, Seen: seen})
			if at.empty() || at.firstTs < row.CreatedAt {
				continue
			}
			role := strings.TrimPrefix(agentType, "nxy:")
			batch := 0
			if role == "implementer" {
				switch f.State.Phase {
				case domain.PhaseFixing, domain.PhaseAffectedFix:
					role = "fixer"
				default:
					batch = batchOfAgent(f, sub.AgentID, sub.Path)
				}
			}
			am, ae := at.modelEffort()
			fin := at.usage()
			if dispatching(f, role, batch) {
				fd.live = append(fd.live, views.LiveAgent{
					AgentID: sub.AgentID, Role: role, Model: am, Effort: ae, Batch: batch,
					Usage: fin, StartedAt: at.firstTs, Tool: at.lastTool, Tools: at.toolCounts(),
				})
				continue
			}
			have[sub.AgentID] = true
			fd.runs = append(fd.runs, domain.AgentRun{
				FeatureID: f.ID, Batch: batch, AgentID: sub.AgentID, Role: role, Model: am, Effort: ae,
				Calls: fin.Calls, TokensIn: int64(fin.Usage.Input), TokensOut: int64(fin.Usage.Output),
				CacheRead: int64(fin.Usage.CacheRead), CacheWrite: int64(fin.Usage.CacheWrite5m + fin.Usage.CacheWrite1h),
				CostUSD: fin.USD, CostEstimated: fin.USDEstimated || fin.USDPartial,
				StartedAt: at.firstTs, EndedAt: at.lastTs,
			})
		}
	}
	if haveMain {
		fin := mainBucket.Finish()
		mt := &views.MainThread{Model: model, Effort: effort, Usage: fin, Tool: mainTool, Tools: []views.ToolCount{}}
		for _, k := range toolKeys {
			mt.Tools = append(mt.Tools, views.ToolCount{Name: k, Count: toolN[k]})
		}
		fd.main = mt
		fd.usd += fin.USD
		fd.est = fd.est || fin.USDEstimated || fin.USDPartial
	}
	for _, r := range fd.runs {
		fd.usd += r.CostUSD
		fd.est = fd.est || r.CostEstimated
	}
	for _, l := range fd.live {
		fd.usd += l.Usage.USD
		fd.est = fd.est || l.Usage.USDEstimated || l.Usage.USDPartial
	}
	sort.SliceStable(fd.runs, func(i, j int) bool { return fd.runs[i].StartedAt < fd.runs[j].StartedAt })
	return fd, nil
}

// batchOfAgent is the batch an implementer worked on: its lane, else the batch its prompt names.
func batchOfAgent(f domain.Feature, agentID, path string) int {
	for _, l := range f.Lanes {
		if l.AgentID == agentID {
			return l.Batch
		}
	}
	if m := batchInPrompt.FindStringSubmatch(transcript.FirstUserText(readHead(path, 64<<10))); m != nil {
		var n int
		fmt.Sscanf(m[1], "%d", &n)
		return n
	}
	return 0
}

// rows lists the features the cockpit shows: active ones, paused ones and the ones done today.
func (v *viewSource) rows(ctx context.Context) ([]sqlite.FeatureRow, error) {
	active, err := v.store.FeatureRows(ctx, sqlite.FeatureFilter{Active: true}, v.midnight())
	if err != nil {
		return nil, err
	}
	paused, err := v.store.FeatureRows(ctx, sqlite.FeatureFilter{Paused: true}, 0)
	if err != nil {
		return nil, err
	}
	return append(active, paused...), nil
}

func (v *viewSource) row(ctx context.Context, id string) (sqlite.FeatureRow, error) {
	all, err := v.store.FeatureRows(ctx, sqlite.FeatureFilter{}, 0)
	if err != nil {
		return sqlite.FeatureRow{}, err
	}
	for _, r := range all {
		if r.Feature.ID == id {
			return r, nil
		}
	}
	return sqlite.FeatureRow{}, fmt.Errorf("no hay una feature %s", id)
}

// Cockpit is Inicio.
func (v *viewSource) Cockpit(ctx context.Context) (views.Cockpit, error) {
	now := v.now()
	rows, err := v.rows(ctx)
	if err != nil {
		return views.Cockpit{}, err
	}
	in := views.CockpitIn{Now: now}
	waiting, err := v.store.OpenPendingAsks(ctx)
	if err != nil {
		return views.Cockpit{}, err
	}
	var open []domain.Feature
	for _, row := range rows {
		fd, err := v.featureData(ctx, row)
		if err != nil {
			return views.Cockpit{}, err
		}
		it := views.CockpitItem{FeatureIn: fd.featureIn(now), Running: row.Feature.State.Running(), Waiting: waiting}
		for _, st := range row.Feature.State.Steps {
			if st.Status == domain.StepFailed {
				it.Failed = append(it.Failed, st.N)
			}
		}
		if fd.main != nil {
			it.TokensIn += int64(fd.main.Usage.Usage.Input)
			it.TokensOut += int64(fd.main.Usage.Usage.Output)
		}
		for _, r := range fd.runs {
			it.TokensIn += r.TokensIn
			it.TokensOut += r.TokensOut
		}
		for _, l := range fd.live {
			it.TokensIn += int64(l.Usage.Usage.Input)
			it.TokensOut += int64(l.Usage.Usage.Output)
		}
		if len(fd.live) == 1 {
			it.Tool = fd.live[0].Tool
		}
		in.Features = append(in.Features, it)
		if row.Feature.State.Phase != domain.PhaseDone {
			open = append(open, row.Feature)
		}
	}
	if !v.todayApart {
		in.CostTodayUSD, _ = v.CostToday(ctx)
	}
	in.Worktrees = v.worktrees(ctx, open)
	return views.BuildCockpit(in), nil
}

// CostToday is the spend of the sessions since midnight.
func (v *viewSource) CostToday(context.Context) (float64, error) {
	var sum float64
	mid := v.midnight()
	for _, s := range v.usage.TrendSessions(&mid, nil, false, v.cwd) {
		if int64(s.Ts) >= mid {
			sum += s.USD
		}
	}
	return sum, nil
}

// worktrees lists the worktrees of the repos of the open features: the ones a feature holds are
// active or paused, the others of those repos are orphans. The main checkout is not listed.
func (v *viewSource) worktrees(ctx context.Context, open []domain.Feature) []views.Worktree {
	var out []views.Worktree
	seen := map[string]bool{}
	for _, f := range open {
		for _, r := range f.RepoList() {
			dir := r.Root
			if dir == "" {
				continue
			}
			list, err := viewWorktrees(ctx, dir)
			if err != nil || seen[list.Main] {
				continue
			}
			seen[list.Main] = true
			for _, row := range list.Rows {
				if row.Main {
					continue
				}
				w := views.Worktree{Repo: list.RepoName, Path: filepath.ToSlash(row.Path), Branch: row.Branch, State: "orphan", Ahead: row.Ahead}
				if row.Dirty != nil && *row.Dirty > 0 {
					w.Dirty = true
				}
				for _, g := range open {
					for _, gr := range g.RepoList() {
						if gr.Checkout != "" && platform.SamePath(gr.Checkout, row.Path) {
							w.FeatureID = g.ID
							w.State = "active"
							if g.Paused || row.PausedAt != 0 {
								w.State = "paused"
							}
						}
					}
				}
				out = append(out, w)
			}
		}
	}
	return out
}

// memCatalog holds the K, G and A ids by repo key (the short ids repeat from one repo to another)
// and the global rules.
type memCatalog struct {
	byRepo map[string]map[string]views.MemRef
	global map[string]views.MemRef
}

func (c memCatalog) add(into map[string]views.MemRef, rules []memory.Rule, ls []memory.Learning) {
	for _, r := range rules {
		if _, ok := into[r.Short]; !ok && r.Short != "" {
			into[r.Short] = views.MemRef{ID: r.Short, Kind: "rule", Text: r.Text}
		}
	}
	for _, l := range ls {
		if _, ok := into[l.Short]; !ok && l.Short != "" {
			into[l.Short] = views.MemRef{ID: l.Short, Kind: "learning", Text: l.Text}
		}
	}
}

// catalogOf loads the memory of every repo of the feature and the global rules.
func (v *viewSource) catalogOf(ctx context.Context, f domain.Feature) (memCatalog, error) {
	c := memCatalog{byRepo: map[string]map[string]views.MemRef{}, global: map[string]views.MemRef{}}
	for _, r := range f.RepoList() {
		rules, err := v.store.Rules(ctx, r.Key)
		if err != nil {
			return c, err
		}
		ls, err := v.store.Learnings(ctx, r.Key)
		if err != nil {
			return c, err
		}
		m := map[string]views.MemRef{}
		c.add(m, rules, ls)
		c.byRepo[r.Key] = m
	}
	g, err := v.store.GlobalRules(ctx)
	if err != nil {
		return c, err
	}
	c.add(c.global, g, nil)
	return c, nil
}

// repoOrder is the repo keys an agent resolves its ids against: the repo of its batch first,
// then the others in the feature's order.
func repoOrder(f domain.Feature, batch int) []string {
	repos := f.RepoList()
	first := 0
	for _, st := range f.State.Steps {
		if st.N != batch || st.Repo == "" {
			continue
		}
		for i, r := range repos {
			if r.Name == st.Repo {
				first = i
			}
		}
	}
	out := []string{}
	if first < len(repos) {
		out = append(out, repos[first].Key)
	}
	for i, r := range repos {
		if i != first {
			out = append(out, r.Key)
		}
	}
	return out
}

// received lists the ids named in an agent prompt, resolved against its repo first; Cited is
// set when the agent's final text names the id again.
func (c memCatalog) received(order []string, prompt, final string) []views.MemRef {
	cited := map[string]bool{}
	for _, m := range memIDRe.FindAllStringSubmatch(final, -1) {
		cited[m[1]] = true
	}
	var out []views.MemRef
	got := map[string]bool{}
	for _, m := range memIDRe.FindAllStringSubmatch(prompt, -1) {
		id := m[1]
		if got[id] {
			continue
		}
		var ref views.MemRef
		ok := false
		for _, key := range order {
			if ref, ok = c.byRepo[key][id]; ok {
				break
			}
		}
		if !ok {
			ref, ok = c.global[id]
		}
		if !ok {
			continue
		}
		got[id] = true
		ref.Cited = cited[id]
		out = append(out, ref)
	}
	return out
}

var memIDRe = regexp.MustCompile(`\b([KGA]\d+)\b`)

// Agents is the agent tree of a feature.
func (v *viewSource) Agents(ctx context.Context, featureID string) (views.Agents, error) {
	row, err := v.row(ctx, featureID)
	if err != nil {
		return views.Agents{}, err
	}
	fd, err := v.featureData(ctx, row)
	if err != nil {
		return views.Agents{}, err
	}
	cat, err := v.catalogOf(ctx, row.Feature)
	if err != nil {
		return views.Agents{}, err
	}
	in := views.AgentsIn{
		FeatureIn: fd.featureIn(v.now()), Runs: fd.runs, Live: fd.live, Main: fd.main,
		Received: map[string][]views.MemRef{}, Learned: map[string][]string{},
	}
	batchOf := map[string]int{}
	var ids []string
	for _, r := range fd.runs {
		ids = append(ids, r.AgentID)
		batchOf[r.AgentID] = r.Batch
	}
	for _, l := range fd.live {
		ids = append(ids, l.AgentID)
		batchOf[l.AgentID] = l.Batch
	}
	for _, id := range ids {
		path := fd.files[id]
		if path == "" {
			continue
		}
		prompt := transcript.FirstUserText(readHead(path, 256<<10))
		final := transcript.LastAssistantText(readTail(path, 256<<10))
		if refs := cat.received(repoOrder(row.Feature, batchOf[id]), prompt, final); len(refs) > 0 {
			in.Received[id] = refs
		}
		for _, l := range memory.ParseReport(final).Learnings {
			in.Learned[id] = append(in.Learned[id], l.Text)
		}
	}
	return views.BuildAgents(in), nil
}

// Decision is the question waiting for the user in a feature.
func (v *viewSource) Decision(ctx context.Context, featureID string) (views.Decision, bool, error) {
	row, err := v.row(ctx, featureID)
	if err != nil {
		return views.Decision{}, false, err
	}
	waiting, err := v.store.OpenPendingAsks(ctx)
	if err != nil {
		return views.Decision{}, false, err
	}
	d, ok := views.BuildDecision(row.Feature, row.UpdatedAt, waiting)
	return d, ok, nil
}

// Memory is Memoria: the repo of the folder or the global one.
func (v *viewSource) Memory(ctx context.Context, scope string) (views.Memory, error) {
	in := views.MemoryIn{Scope: scope, Exists: func(p string) bool { _, err := os.Stat(p); return err == nil }}
	var err error
	if scope == views.ScopeGlobal {
		if in.Rules, err = v.store.GlobalRules(ctx); err != nil {
			return views.Memory{}, err
		}
		if in.Learnings, err = v.store.Learnings(ctx, memory.GlobalRepo); err != nil {
			return views.Memory{}, err
		}
		return views.BuildMemory(in), nil
	}
	in.Scope = views.ScopeRepo
	repo := repoKeys{}.Key(v.cwd)
	root, _ := repoKeys{}.Root(v.cwd)
	if root == "" {
		return views.Memory{}, errors.New("esta carpeta no es un repo")
	}
	in.Repo = views.RepoRef{Name: filepath.Base(root), Path: filepath.ToSlash(root), Branch: platform.CurrentBranch(ctx, v.cwd)}
	if in.Rules, err = v.store.Rules(ctx, repo); err != nil {
		return views.Memory{}, err
	}
	if in.Learnings, err = v.store.Learnings(ctx, repo); err != nil {
		return views.Memory{}, err
	}
	if in.Closed, err = v.store.ClosedFeatures(ctx, repo, 20); err != nil {
		return views.Memory{}, err
	}
	return views.BuildMemory(in), nil
}

// Stats is Gasto of one session ("" is the current one, or the last of the repo).
func (v *viewSource) Stats(ctx context.Context, session string) (views.Stats, error) {
	args := []string{"--json", "--cwd", v.cwd}
	if session != "" {
		args = append(args, "--session", session)
	}
	d, ref, early := v.usage.StatsData(args)
	here := true
	if early != nil && session == "" && early.Code != 0 {
		// no session for this folder (outside a repo, say): the last one of any project
		d, ref, early = v.usage.StatsData(append(args, "--all"))
		here = false
		if early != nil && early.Code != 0 {
			return views.Stats{}, views.ErrNoSession
		}
	}
	if early != nil {
		msg := strings.TrimSpace(early.Stdout + early.Stderr)
		if msg == "" {
			msg = "nxy stats: no session"
		}
		return views.Stats{}, errors.New(msg)
	}
	since := v.now().AddDate(0, 0, -30).UnixMilli()
	var others []views.SessionRef
	sessions := v.usage.TrendSessions(&since, nil, here, v.cwd)
	sort.SliceStable(sessions, func(i, j int) bool { return sessions[i].Ts > sessions[j].Ts })
	for _, s := range sessions {
		if s.SessionID == ref.SessionID || len(others) >= 10 {
			continue
		}
		label := strings.TrimSpace(strings.SplitN(s.FirstPrompt, "\n", 2)[0])
		if rs := []rune(label); len(rs) > 60 {
			label = string(rs[:60])
		}
		others = append(others, views.SessionRef{
			ID: s.SessionID, Label: label,
			StartedAt: time.UnixMilli(int64(s.Ts)).UTC().Format(time.RFC3339), USD: s.USD,
		})
	}
	return views.BuildStats(d, others), nil
}

// Trend is Tendencia: the last 8 weeks of every project, and the agent runs of the same range.
func (v *viewSource) Trend(ctx context.Context, anchor time.Time) (views.Trend, error) {
	now := v.now()
	var until *int64
	if !anchor.IsZero() {
		now = anchor
		a := anchor.In(v.loc)
		// the end of the Sunday of the anchor's week: what the weeks shown reach
		sunday := a.AddDate(0, 0, (7-int(a.Weekday()))%7)
		end := time.Date(sunday.Year(), sunday.Month(), sunday.Day(), 23, 59, 59, 999e6, v.loc).UnixMilli()
		until = &end
	}
	since := now.In(v.loc).AddDate(0, 0, -64).UnixMilli()
	runs, err := v.store.AgentRunsSince(ctx, since)
	if err != nil {
		return views.Trend{}, err
	}
	if until != nil {
		kept := runs[:0]
		for _, r := range runs {
			if r.StartedAt <= *until {
				kept = append(kept, r)
			}
		}
		runs = kept
	}
	return views.BuildTrend(views.TrendIn{
		Now: now, Loc: v.loc, Scope: views.ScopeGlobal,
		Sessions: v.usage.TrendSessions(&since, until, false, v.cwd), Runs: runs,
	}), nil
}

// InRepo says whether the folder of the source is inside a git repo.
func (v *viewSource) InRepo() bool {
	root, _ := repoKeys{}.Root(v.cwd)
	return root != ""
}

// Config is Config: the layers, the roles, the settings, the statusline, the tools and the binary.
func (v *viewSource) Config(ctx context.Context) (views.Config, error) {
	cwd := v.cwd
	userPath, repoPath, err := configFilePaths(cwd)
	if err != nil {
		return views.Config{}, err
	}
	userState, userObj := fileState(userPath)
	repoState, repoObj := fileState(repoPath)
	files := configedit.Files{User: userObj, Repo: repoObj}
	cfg := platform.LoadConfig(cwd, v.getenv)
	root, _ := repoKeys{}.Root(cwd)
	in := views.ConfigIn{
		InRepo: root != "",
		Layers: []views.Layer{
			{Name: "user", Path: filepath.ToSlash(userPath), Exists: userState != "no existe"},
			{Name: "repo", Path: filepath.ToSlash(repoPath), Exists: repoState != "no existe"},
		},
	}
	for _, p := range configedit.Invalid(cfg) {
		in.Warnings = append(in.Warnings, p.Message())
	}
	for _, r := range configedit.Roles {
		model, effort := configedit.RoleOf(cfg, r)
		src := configedit.SourcesOf("roles."+r+".model", files, v.getenv)
		if eff := configedit.SourcesOf("roles."+r+".effort", files, v.getenv); sourceRank(eff) > sourceRank(src) {
			src = eff
		}
		in.Roles = append(in.Roles, views.RoleIn{Role: r, Model: model, Effort: effort, Source: string(src)})
	}
	for _, f := range configedit.Fields() {
		if strings.HasPrefix(f.ID, "roles.") {
			continue
		}
		in.Settings = append(in.Settings, views.SettingIn{
			ID: f.ID, Value: lookupPath(cfg, f.Path), Source: string(configedit.SourcesOf(f.ID, files, v.getenv)),
		})
	}
	in.Statusline = statuslineView()
	st := viewSetupState(cwd, v.getenv)
	for _, name := range setup.Tools {
		info := st.Tool(name)
		t := views.Tool{Name: name, Found: info.Found, Version: info.Version, Path: filepath.ToSlash(info.Path)}
		if !info.Found {
			t.Install = &views.Action{ID: "install", Label: "Instalar " + name, Argv: []string{"nxy", "setup", "run", name, "--yes"}, Confirm: "Instala " + name}
		}
		in.Tools = append(in.Tools, t)
	}
	nxyTool := views.Tool{Name: "nxy"}
	if p := nxyOnPath(); p != "" {
		nxyTool.Found, nxyTool.Path = true, filepath.ToSlash(p)
	} else {
		cmd := "nxy setup run path --yes"
		nxyTool.Install = &views.Action{ID: "install", Label: "Poner nxy en el PATH", Argv: []string{"nxy", "setup", "run", "path", "--yes"}, Confirm: cmd}
	}
	in.Tools = append(in.Tools, nxyTool)
	in.Binary = viewBinary()
	return views.BuildConfig(in), nil
}

// sourceRank orders the config layers by priority: env over repo over user over default.
func sourceRank(s configedit.Source) int {
	switch s {
	case configedit.SourceEnv:
		return 3
	case configedit.SourceRepo:
		return 2
	case configedit.SourceUser:
		return 1
	}
	return 0
}

// statuslineView says whether nxy's statusline is in Claude Code's settings.
func statuslineView() views.Statusline {
	out := views.Statusline{
		Install: views.Action{ID: "install", Label: "Instalar", Argv: []string{"nxy", "statusline", "setup", "install"}, Confirm: "Escribe la statusline en ~/.claude/settings.json (queda un backup)"},
		Remove:  views.Action{ID: "remove", Label: "Quitar", Argv: []string{"nxy", "statusline", "setup", "remove"}, Confirm: "Quita la statusline de ~/.claude/settings.json"},
	}
	s, err := realStatuslineSetup()
	if err != nil {
		return out
	}
	out.SettingsPath = filepath.ToSlash(s.settingsPath())
	if doc, exists, err := platform.ReadJSONDoc(s.settingsPath()); err == nil && exists {
		if cur, ok := doc.Get("statusLine"); ok {
			m, _ := cur.(map[string]any)
			out.Installed = s.isOurs(m["command"])
		}
	}
	return out
}

var shimVersionRe = regexp.MustCompile(`NXY_VERSION=([^"\s]+)`)

// realBinary is the binary that runs and the version the shim wants.
func realBinary() views.Binary {
	b := views.Binary{Installed: version.Version, Download: "none"}
	if m := shimVersionRe.FindSubmatch(nxydev.Shim); m != nil {
		b.Wanted = string(m[1])
	}
	if e, err := os.Executable(); err == nil {
		p := filepath.ToSlash(e)
		if h := filepath.ToSlash(homeDir()); h != "" && strings.HasPrefix(p, h+"/") {
			p = "~" + p[len(h):]
		}
		b.Path = p
	}
	return b
}
