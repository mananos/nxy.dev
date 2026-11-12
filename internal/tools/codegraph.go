package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/jscompat"
	"github.com/mananos/nxy.dev/internal/platform"
)

const codegraphVersionTimeout = 4 * time.Second

// Per-step budgets of the codegraph lifecycle. Vars so a test can shrink them.
var (
	initBudget   = 90 * time.Second
	syncBudget   = 60 * time.Second
	queryBudget  = 30 * time.Second
	unlockBudget = 15 * time.Second
	uninitBudget = 15 * time.Second
)

// KnownCodegraphLocations are the install locations to try when codegraph is not on PATH
// (ported from core/scout/codegraph.mjs).
func KnownCodegraphLocations(goos, home string, getenv func(string) string) []string {
	var out []string
	if goos == "windows" {
		local := getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		install := getenv("CODEGRAPH_INSTALL_DIR")
		if install == "" {
			install = filepath.Join(local, "codegraph")
		}
		return append(out,
			filepath.Join(install, "current", "bin", "codegraph.cmd"),
			filepath.Join(local, "Microsoft", "WinGet", "Links", "codegraph.exe"),
			filepath.Join(home, ".local", "bin", "codegraph.exe"),
			filepath.Join(home, ".cargo", "bin", "codegraph.exe"),
			filepath.Join(home, "AppData", "Roaming", "npm", "codegraph.cmd"))
	}
	if d := getenv("CODEGRAPH_BIN_DIR"); d != "" {
		out = append(out, filepath.Join(d, "codegraph"))
	}
	out = append(out, "/usr/local/bin/codegraph", "/usr/bin/codegraph", "/opt/homebrew/bin/codegraph",
		filepath.Join(home, ".local", "bin", "codegraph"),
		filepath.Join(home, ".cargo", "bin", "codegraph"))
	install := getenv("CODEGRAPH_INSTALL_DIR")
	if install == "" {
		install = filepath.Join(home, ".codegraph")
	}
	return append(out, filepath.Join(install, "current", "bin", "codegraph"))
}

var shimToken = regexp.MustCompile(`(?i)"((?:%~dp0|%dp0%)[^"]*)"`)
var shimPrefix = regexp.MustCompile(`(?i)^(?:%~dp0|%dp0%)[\\/]*`)

// ResolveLauncher turns a codegraph binary into an argv prefix that runs without a shell. On Windows
// a .cmd/.bat shim cannot be exec'd safely, so its node + js script are read out of it. ok is false
// when the shim cannot be understood.
func ResolveLauncher(goos, bin string, readFile func(string) ([]byte, error), exists func(string) bool) (argv []string, ok bool) {
	lower := strings.ToLower(bin)
	if goos != "windows" || !(strings.HasSuffix(lower, ".cmd") || strings.HasSuffix(lower, ".bat")) {
		return []string{bin}, true
	}
	data, err := readFile(bin)
	if err != nil {
		return nil, false
	}
	dir := filepath.Dir(bin)
	rooted := func(suffix string) string {
		for _, m := range shimToken.FindAllStringSubmatch(string(data), -1) {
			if !strings.HasSuffix(strings.ToLower(m[1]), suffix) {
				continue
			}
			rest := strings.ReplaceAll(shimPrefix.ReplaceAllString(m[1], ""), "\\", "/")
			return filepath.Join(dir, filepath.FromSlash(rest))
		}
		return ""
	}
	js := rooted(".js")
	if js == "" || !exists(js) {
		return nil, false
	}
	node := rooted("node.exe")
	if node == "" || !exists(node) {
		p, err := exec.LookPath("node")
		if err != nil {
			return nil, false
		}
		node = p
	}
	return []string{node, js}, true
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// versionOf runs `<argv> --version`; ok only on exit 0 with output.
func versionOf(argv []string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), codegraphVersionTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, argv[0], append(append([]string{}, argv[1:]...), "--version")...)
	hideWindow(c)
	var out bytes.Buffer
	c.Stdout = &out
	if c.Run() != nil {
		return "", false
	}
	v := strings.TrimSpace(out.String())
	if v == "" {
		return "", false
	}
	return strings.SplitN(v, "\n", 2)[0], true
}

// pathCandidates lists codegraph on PATH. Windows does not resolve PATHEXT for us here, so .exe then .cmd.
func pathCandidates(goos string, getenv func(string) string) []string {
	if goos != "windows" {
		return []string{"codegraph"}
	}
	path := getenv("PATH")
	if path == "" {
		path = getenv("Path")
	}
	var out []string
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		for _, n := range []string{"codegraph.exe", "codegraph.cmd"} {
			if p := filepath.Join(dir, n); fileExists(p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// FindCodegraph locates codegraph: NXY_CODEGRAPH_PATH, then PATH, then the known install locations.
// argv is the launcher prefix to run it (no shell).
func FindCodegraph(getenv func(string) string) (argv []string, version string, ok bool) {
	argv, version, _, ok = FindCodegraphLabeled(getenv)
	return argv, version, ok
}

// FindCodegraphLabeled is FindCodegraph plus the label the 1.x shows for the path: NXY_CODEGRAPH_PATH
// as given, "codegraph" when it was found on PATH, or the known location with forward slashes.
func FindCodegraphLabeled(getenv func(string) string) (argv []string, version, label string, ok bool) {
	try := func(bin string) ([]string, string, bool) {
		a, ok := ResolveLauncher(runtime.GOOS, bin, os.ReadFile, fileExists)
		if !ok {
			return nil, "", false
		}
		v, ok := versionOf(a)
		return a, v, ok
	}
	if p := getenv("NXY_CODEGRAPH_PATH"); p != "" {
		a, v, ok := try(p)
		if !ok {
			return nil, "", "", false
		}
		return a, v, p, true
	}
	for _, bin := range pathCandidates(runtime.GOOS, getenv) {
		if a, v, ok := try(bin); ok {
			return a, v, "codegraph", true
		}
	}
	for _, bin := range KnownCodegraphLocations(runtime.GOOS, homeOf(getenv), getenv) {
		if !fileExists(bin) {
			continue
		}
		if a, v, ok := try(bin); ok {
			return a, v, strings.ReplaceAll(bin, `\`, "/"), true
		}
	}
	return nil, "", "", false
}

// indexDirs are the names the codegraph index may have in a repo.
func indexDirs(getenv func(string) string) []string {
	var out []string
	if d := getenv("CODEGRAPH_DIR"); d != "" {
		out = append(out, d)
	}
	return append(out, ".codegraph")
}

// HasIndex reports whether root has a codegraph index; honors CODEGRAPH_DIR.
func HasIndex(root string, getenv func(string) string) bool {
	for _, n := range indexDirs(getenv) {
		p := n
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, n)
		}
		if fileExists(p) {
			return true
		}
	}
	return false
}

// Status is how the codegraph lifecycle ended.
type Status string

const (
	StatusMissing     Status = "Missing"     // codegraph is not installed
	StatusInitialized Status = "Initialized" // the index was created now; tests are valid
	StatusInitFailed  Status = "InitFailed"
	StatusInitTimeout Status = "InitTimeout"
	StatusLocked      Status = "Locked" // sync still stuck after unlock
	StatusSyncFailed  Status = "SyncFailed"
	StatusQueryFailed Status = "QueryFailed"
	StatusOK          Status = "OK"
)

// GraphResult is the outcome of Graph.Affected. Tests are only meaningful for OK and Initialized.
type GraphResult struct {
	Tests  []string
	Status Status
	Took   time.Duration // of the init step, when it ran
}

// ExecFunc runs argv in dir (no shell) feeding stdin, and returns the exit code and stdout. A non-zero
// exit is not an error; err is for a process that cannot start or an expired ctx.
type ExecFunc func(ctx context.Context, dir string, argv []string, stdin string) (exit int, stdout string, err error)

func defaultExec(ctx context.Context, dir string, argv []string, stdin string) (int, string, error) {
	exit, out, _, err := platform.RunArgvSplit(ctx, dir, argv, stdin, 4<<20)
	return exit, out, err
}

// Graph drives the codegraph CLI. A zero Argv means codegraph is missing.
type Graph struct {
	Argv    []string // launcher prefix, from FindCodegraph
	Getenv  func(string) string
	Exec    ExecFunc                                    // default: platform.RunArgvSplit
	Exclude func(ctx context.Context, dir string) error // default: add the index dir to .git/info/exclude
}

// NewGraph finds codegraph once; the result is Missing when it is not installed.
func NewGraph(getenv func(string) string) *Graph {
	argv, _, _ := FindCodegraph(getenv)
	return &Graph{Argv: argv, Getenv: getenv}
}

func (g *Graph) getenv(k string) string {
	if g.Getenv != nil {
		return g.Getenv(k)
	}
	return os.Getenv(k)
}

func (g *Graph) run(ctx context.Context, budget time.Duration, root, stdin string, args ...string) (int, string, error) {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	argv := append(append([]string{}, g.Argv...), args...)
	ex := g.Exec
	if ex == nil {
		ex = defaultExec
	}
	exit, out, err := ex(ctx, root, argv, stdin)
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	return exit, out, err
}

func ok(exit int, err error) bool { return err == nil && exit == 0 }

func timedOut(err error) bool { return errors.Is(err, context.DeadlineExceeded) }

func (g *Graph) exclude(ctx context.Context, root string) {
	if g.Exclude != nil {
		_ = g.Exclude(ctx, root)
		return
	}
	name := ".codegraph"
	if d := g.getenv("CODEGRAPH_DIR"); d != "" && !filepath.IsAbs(d) {
		name = filepath.ToSlash(d)
	}
	_ = platform.GitExcludeLocal(ctx, root, "/"+strings.Trim(name, "/")+"/")
}

// Affected makes sure the index of root exists and is fresh, then asks which test files the changed
// files affect. Every step has its own deadline, capped by ctx; nothing can hang. On a failed or
// timed-out init the half-built index is removed so it is never synced later.
func (g *Graph) Affected(ctx context.Context, root string, changed []string) GraphResult {
	if len(g.Argv) == 0 {
		return GraphResult{Status: StatusMissing}
	}
	res := GraphResult{Status: StatusOK}
	if !HasIndex(root, g.getenv) {
		start := time.Now()
		exit, _, err := g.run(ctx, initBudget, root, "", "init", "-y", root)
		res.Took = time.Since(start)
		if !ok(exit, err) {
			// Kill is done by the runner; clean up with a context that survives the expired step.
			cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), uninitBudget)
			g.run(cctx, uninitBudget, root, "", "uninit", "-f", root)
			cancel()
			if timedOut(err) || ctx.Err() != nil {
				res.Status = StatusInitTimeout
			} else {
				res.Status = StatusInitFailed
			}
			return res
		}
		g.exclude(ctx, root)
		res.Status = StatusInitialized
	}

	exit, _, err := g.run(ctx, syncBudget, root, "", "sync", "-q", root)
	if !ok(exit, err) {
		g.run(context.WithoutCancel(ctx), unlockBudget, root, "", "unlock", root)
		exit, _, err = g.run(ctx, syncBudget, root, "", "sync", "-q", root)
		if !ok(exit, err) {
			if timedOut(err) {
				res.Status = StatusLocked
			} else {
				res.Status = StatusSyncFailed
			}
			return res
		}
	}

	if len(changed) == 0 {
		return res
	}
	exit, out, err := g.run(ctx, queryBudget, root, strings.Join(changed, "\n")+"\n", "affected", "--json", "--stdin", "-p", root)
	if !ok(exit, err) {
		res.Status = StatusQueryFailed
		return res
	}
	tests, perr := ParseAffected(out)
	if perr != nil {
		res.Status = StatusQueryFailed
		return res
	}
	res.Tests = tests
	return res
}

// AffectedIndexed asks only the `affected` query of an index that already exists: it never inits or
// syncs, so a view does not build or refresh anything. false when there is no codegraph, no index
// or the query fails.
func (g *Graph) AffectedIndexed(ctx context.Context, root string, changed []string) ([]string, bool) {
	if len(g.Argv) == 0 || len(changed) == 0 || !HasIndex(root, g.getenv) {
		return nil, false
	}
	exit, out, err := g.run(ctx, queryBudget, root, strings.Join(changed, "\n")+"\n", "affected", "--json", "--stdin", "-p", root)
	if !ok(exit, err) {
		return nil, false
	}
	tests, perr := ParseAffected(out)
	if perr != nil {
		return nil, false
	}
	return tests, true
}

// Budgets and cap of Explore/Relations (core/scout/codegraph.mjs runCodegraph): the payload is dense,
// but it dies with the scout that reads it.
const exploreMaxChars = 24_000

// ExploreBudget is the time a codegraph explore/relations query gets. A var so a test can shrink it.
var ExploreBudget = 20 * time.Second

// QueryResult is the answer of Explore/Relations. Reason is "ok", "not-installed", "no-index",
// "spawn-failed", "timeout", "exit-N" or "empty"; Text is trimmed and capped at 24 000 UTF-16 units.
type QueryResult struct {
	OK        bool
	Reason    string
	Text      string
	Truncated bool
	Ms        int64
}

// Explore runs `codegraph explore <question>` in root. It never fails: every failure is a Reason, so
// the caller can declare the fallback instead of losing the answer.
func (g *Graph) Explore(ctx context.Context, root, question string) QueryResult {
	return g.query(ctx, root, "explore", question)
}

// Relations runs `codegraph callers|callees|impact <symbol>` in root, with the same caps as Explore.
func (g *Graph) Relations(ctx context.Context, root, kind, symbol string) QueryResult {
	return g.query(ctx, root, kind, symbol)
}

func (g *Graph) query(ctx context.Context, root string, args ...string) QueryResult {
	if len(g.Argv) == 0 {
		return QueryResult{Reason: "not-installed"}
	}
	if !HasIndex(root, g.getenv) {
		return QueryResult{Reason: "no-index"}
	}
	start := time.Now()
	exit, out, err := g.run(ctx, ExploreBudget, root, "", args...)
	ms := time.Since(start).Milliseconds()
	switch {
	case timedOut(err):
		return QueryResult{Reason: "timeout", Ms: ms}
	case err != nil:
		return QueryResult{Reason: "spawn-failed", Ms: ms}
	case exit != 0:
		return QueryResult{Reason: "exit-" + strconv.Itoa(exit), Ms: ms}
	}
	raw := jscompat.Trim(out)
	if raw == "" {
		return QueryResult{Reason: "empty", Ms: ms}
	}
	res := QueryResult{OK: true, Reason: "ok", Text: raw, Ms: ms}
	if jscompat.UTF16Len(raw) > exploreMaxChars {
		res.Truncated, res.Text = true, jscompat.UTF16Prefix(raw, exploreMaxChars)
	}
	return res
}

// DescribeCodegraph is the status line of `nxy locate --status`, as the 1.x describeCodegraph.
func (g *Graph) DescribeCodegraph(root string) string {
	_, version, label, ok := FindCodegraphLabeled(g.getenv)
	if !ok {
		return "codegraph:    not installed (scout falls back to the repo map + rg)"
	}
	if !HasIndex(root, g.getenv) {
		return "codegraph:    " + label + " " + version + " — but this repo has no index; run `codegraph init` to enable it"
	}
	return "codegraph:    " + label + " " + version + " — indexed"
}

// ParseAffected reads the affectedTests of `codegraph affected --json`. It tolerates text around the
// JSON object.
func ParseAffected(out string) ([]string, error) {
	start, end := strings.Index(out, "{"), strings.LastIndex(out, "}")
	if start < 0 || end < start {
		return nil, errors.New("no JSON object in codegraph output")
	}
	var v struct {
		AffectedTests []string `json:"affectedTests"`
	}
	if err := json.Unmarshal([]byte(out[start:end+1]), &v); err != nil {
		return nil, err
	}
	for i, t := range v.AffectedTests {
		v.AffectedTests[i] = filepath.ToSlash(t)
	}
	return v.AffectedTests, nil
}
