package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/mananos/nxy.dev/v2/internal/app"
	"github.com/mananos/nxy.dev/v2/internal/platform"
	"github.com/mananos/nxy.dev/v2/internal/repomap"
	"github.com/mananos/nxy.dev/v2/internal/scout"
	"github.com/mananos/nxy.dev/v2/internal/tools"
)

const locateUsage = `Usage: nxy locate "<question>"  |  --callers|--callees|--impact <symbol>  |  --status  |  --index`

// parseLocateArgs reads the flags like the 1.x parseArgs: --k=v, --k v (v is anything that does not
// start with "--") and a bare --k, which is "true".
func parseLocateArgs(args []string) (opts map[string]string, positional []string) {
	opts = map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			positional = append(positional, a)
			continue
		}
		if eq := strings.Index(a, "="); eq != -1 {
			opts[a[2:eq]] = a[eq+1:]
		} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			i++
			opts[a[2:]] = args[i]
		} else {
			opts[a[2:]] = "true"
		}
	}
	return opts, positional
}

// repoIndex is the repo map under <runtime dir>/index, shared with the 1.x.
type repoIndex struct{}

func (repoIndex) Info(dir string) (scout.IndexInfo, bool) {
	i := platform.RepoMapInfo(dir, nil)
	if i == nil {
		return scout.IndexInfo{}, false
	}
	return scout.IndexInfo{Files: i.Files, Symbols: i.Symbols, AgeMs: i.AgeMs}, true
}

func (repoIndex) Build(dir string, force bool) (scout.IndexInfo, error) {
	r, err := platform.BuildRepoMap(dir, force, nil)
	return scout.IndexInfo{Files: r.Files, Symbols: r.Symbols}, err
}

func (repoIndex) Query(dir, term string, limit int) []repomap.Row {
	return platform.QueryRepoMap(dir, term, repomap.Opts{Limit: &limit}).Rows
}

// rgSearch finds rg once and runs it.
type rgSearch struct {
	once  sync.Once
	bin   string
	found bool
}

func (r *rgSearch) find() {
	r.once.Do(func() { r.bin, r.found = tools.FindRg(os.Getenv) })
}

func (r *rgSearch) Available() bool {
	r.find()
	return r.found
}

func (r *rgSearch) Search(ctx context.Context, dir, pattern string, o scout.RgOpts) scout.RgResult {
	r.find()
	if !r.found {
		return scout.RgResult{Reason: "rg-missing"}
	}
	res := tools.RgSearch(ctx, dir, pattern, tools.RgOpts{Bin: r.bin, MaxRows: o.MaxRows, MaxPerFile: o.MaxPerFile, Word: o.Word})
	out := scout.RgResult{OK: res.OK, Reason: res.Reason, Truncated: res.Truncated}
	for _, row := range res.Rows {
		out.Rows = append(out.Rows, scout.RgRow{Path: row.Path, Line: row.Line, Text: row.Text})
	}
	return out
}

// codegraph runs codegraph through tools.Graph.
type codegraph struct{ g *tools.Graph }

func toCodeResult(r tools.QueryResult) scout.CodeResult {
	return scout.CodeResult{OK: r.OK, Reason: r.Reason, Text: r.Text, Truncated: r.Truncated, Ms: r.Ms}
}

func (c codegraph) Explore(ctx context.Context, dir, question string) scout.CodeResult {
	return toCodeResult(c.g.Explore(ctx, dir, question))
}

func (c codegraph) Relations(ctx context.Context, dir, kind, symbol string) scout.CodeResult {
	return toCodeResult(c.g.Relations(ctx, dir, kind, symbol))
}

func (c codegraph) Describe(dir string) string { return c.g.DescribeCodegraph(dir) }

// newLocator wires the three engines; codegraph is only looked for when it is wanted.
func newLocator(withCodegraph bool) app.Locator {
	l := app.Locator{Index: repoIndex{}, Text: &rgSearch{}}
	if withCodegraph {
		l.Code = codegraph{g: tools.NewGraph(os.Getenv)}
	}
	return l
}

// scoutUsesCodegraph is cfg.scout.codegraph !== false of the 1.x.
func scoutUsesCodegraph(cwd string) bool {
	cfg := platform.LoadConfig(cwd, os.Getenv)
	s, _ := cfg["scout"].(map[string]any)
	return s["codegraph"] != false
}

type locateJSON struct {
	OK        bool        `json:"ok"`
	Mode      string      `json:"mode"`
	Question  string      `json:"question"`
	Hits      []scout.Hit `json:"hits"`
	Terms     []string    `json:"terms"`
	Degraded  []string    `json:"degraded"`
	Ms        int64       `json:"ms"`
	Codegraph string      `json:"codegraph"`
}

func printJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// runLocate is the search the scout runs before it reads anything (`nxy locate`):
//
//	nxy locate "<question>"                    every model-free engine, printed
//	nxy locate --callers|--callees|--impact <symbol> [--json]
//	nxy locate --status                        which engines this repo has
//	nxy locate --index | --rebuild "<question>" rebuild the repo map (and search)
//
// The repo map is <runtime dir>/index/repomap.tsv and repomap.meta.json, the same files the 1.x
// writes: size proportional to the repo's source (about 100 bytes per symbol), rebuilt only for the
// files that changed, deleted with .nxy/local. The output is dense on purpose and is meant to be read
// inside the scout, never in the main thread.
func runLocate(args []string, _ io.Reader, stdout, _ io.Writer) (code int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stdout, "error: %v\n", r)
			code = 1
		}
	}()
	opts, positional := parseLocateArgs(args)
	cwd := opts["cwd"]
	if cwd == "" || cwd == "true" {
		cwd = osGetwd()
	}
	cwd = platform.NativePath(cwd)
	// `--json "q"` and `--rebuild "q"` read as a flag with a value: give the word back to the question.
	var words []string
	for _, k := range []string{"json", "rebuild"} {
		if v := opts[k]; v != "" && v != "true" {
			words = append(words, v)
		}
	}
	question := strings.TrimSpace(strings.Join(append(words, positional...), " "))

	kind, symbol := "", ""
	for _, k := range []string{"callers", "callees", "impact"} {
		if v := opts[k]; v != "" && v != "true" {
			kind, symbol = k, strings.TrimSpace(v)
			break
		}
	}

	if _, ok := opts["status"]; ok {
		fmt.Fprintln(stdout, newLocator(true).Status(cwd))
		return 0
	}
	if _, ok := opts["index"]; ok {
		r, err := platform.BuildRepoMap(cwd, true, nil)
		if err != nil {
			fmt.Fprintf(stdout, "error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "repo map: %d symbols from %d files in %d ms -> %s\n", r.Symbols, r.Files, r.Ms, r.Map)
		return 0
	}
	_, asJSON := opts["json"]
	if question == "" && kind == "" {
		if asJSON {
			fmt.Fprintln(stdout, `{"ok":false,"reason":"no question"}`)
		} else {
			fmt.Fprintln(stdout, locateUsage)
		}
		return 0
	}

	withCodegraph := scoutUsesCodegraph(cwd)
	l := newLocator(withCodegraph)
	so := scout.SearchOpts{Codegraph: withCodegraph}
	var res app.LocateResult
	mode := "search"
	if kind != "" {
		mode = kind
		res = l.Relations(context.Background(), cwd, kind, symbol, so)
	} else {
		_, so.Rebuild = opts["rebuild"]
		res = l.Locate(context.Background(), cwd, question, so)
	}
	if asJSON {
		printJSON(stdout, locateJSON{OK: true, Mode: mode, Question: question, Hits: res.Hits, Terms: res.Terms, Degraded: res.Degraded, Ms: res.Ms, Codegraph: res.Codegraph})
	} else {
		fmt.Fprintln(stdout, res.Text)
	}
	return 0
}
