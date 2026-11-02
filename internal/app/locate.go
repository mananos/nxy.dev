package app

import (
	"context"
	"time"

	"github.com/mananos/nxy.dev/internal/repomap"
	"github.com/mananos/nxy.dev/internal/scout"
)

// Locator is the model-free search the scout runs before it reads anything: repo map, then rg, then
// codegraph. A nil engine is an engine that is not there, and the report says so; the search itself
// never fails and never answers nothing.
type Locator struct {
	Index RepoIndex
	Text  TextSearch
	Code  CodeExplorer
	Clock Clock
}

// LocateResult is a report and how long it took.
type LocateResult struct {
	scout.Report
	Ms int64
}

func (l Locator) now() time.Time {
	if l.Clock != nil {
		return l.Clock.Now()
	}
	return time.Now()
}

// engines wires the ports of dir into the functions the pure report builder takes.
func (l Locator) engines(ctx context.Context, dir string) scout.Engines {
	return scout.Engines{
		Index: func() (scout.IndexInfo, bool) {
			if l.Index == nil {
				return scout.IndexInfo{}, false
			}
			return l.Index.Info(dir)
		},
		Build: func(force bool) (scout.IndexInfo, error) {
			if l.Index == nil {
				return scout.IndexInfo{}, nil
			}
			return l.Index.Build(dir, force)
		},
		Query: func(term string, limit int) []repomap.Row {
			if l.Index == nil {
				return nil
			}
			return l.Index.Query(dir, term, limit)
		},
		RgFound: func() bool { return l.Text != nil && l.Text.Available() },
		Rg: func(pattern string, o scout.RgOpts) scout.RgResult {
			if l.Text == nil {
				return scout.RgResult{Reason: "rg-missing"}
			}
			return l.Text.Search(ctx, dir, pattern, o)
		},
		Explore: func(question string) scout.CodeResult {
			if l.Code == nil {
				return scout.CodeResult{Reason: "not-installed"}
			}
			return l.Code.Explore(ctx, dir, question)
		},
		Relations: func(kind, symbol string) scout.CodeResult {
			if l.Code == nil {
				return scout.CodeResult{Reason: "not-installed"}
			}
			return l.Code.Relations(ctx, dir, kind, symbol)
		},
	}
}

// Locate answers where something is: every available engine, in one report.
func (l Locator) Locate(ctx context.Context, dir, question string, opts scout.SearchOpts) LocateResult {
	start := l.now()
	r := scout.Search(question, opts, l.engines(ctx, dir))
	return LocateResult{Report: r, Ms: l.now().Sub(start).Milliseconds()}
}

// Relations answers who calls a symbol, what it calls or what breaks if it changes (kind: callers,
// callees or impact), with codegraph when it can and rg or the repo map otherwise.
func (l Locator) Relations(ctx context.Context, dir, kind, symbol string, opts scout.SearchOpts) LocateResult {
	start := l.now()
	r := scout.Relations(kind, symbol, opts, l.engines(ctx, dir))
	return LocateResult{Report: r, Ms: l.now().Sub(start).Milliseconds()}
}

// Status says which engines the scout would actually have in dir.
func (l Locator) Status(dir string) string {
	var info *scout.IndexInfo
	if l.Index != nil {
		if i, ok := l.Index.Info(dir); ok {
			info = &i
		}
	}
	code := "codegraph:    not installed (scout falls back to the repo map + rg)"
	if l.Code != nil {
		code = l.Code.Describe(dir)
	}
	return scout.DescribeEngines(info, l.Text != nil && l.Text.Available(), code)
}
