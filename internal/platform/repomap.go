package platform

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/jscompat"
	"github.com/mananos/nxy.dev/v2/internal/repomap"
)

// RepoMapFiles are where the repo map and its metadata live for a checkout: the same files the
// 1.x writes (core/index/repomap.mjs indexPaths).
type RepoMapFiles struct {
	Dir  string
	Map  string
	Meta string
}

// RepoMapPaths is <runtime dir>/index/{repomap.tsv,repomap.meta.json}.
func RepoMapPaths(cwd string) RepoMapFiles {
	dir := filepath.Join(RuntimeDir(cwd), "index")
	return RepoMapFiles{Dir: dir, Map: filepath.Join(dir, "repomap.tsv"), Meta: filepath.Join(dir, "repomap.meta.json")}
}

// ListSourceFiles walks root and returns the files worth indexing: regular files with a language
// rule, at most repomap.MaxFileBytes, outside the always-skipped directories, dot directories and
// what the root .gitignore matches. An unreadable directory is skipped.
func ListSourceFiles(root string) []repomap.SourceFile {
	text, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	ignored := repomap.Matcher(string(text))
	out := []repomap.SourceFile{}
	var walk func(dir string)
	walk = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := e.Name()
			abs := filepath.Join(dir, name)
			r, err := filepath.Rel(root, abs)
			if err != nil {
				continue
			}
			rel := filepath.ToSlash(r)
			if e.IsDir() {
				if repomap.AlwaysSkip[name] || strings.HasPrefix(name, ".") || ignored(rel, name, true) {
					continue
				}
				walk(abs)
			} else if e.Type().IsRegular() {
				if repomap.RulesFor(repomap.Ext(name)) == nil || ignored(rel, name, false) {
					continue
				}
				st, err := e.Info()
				if err != nil || st.Size() > repomap.MaxFileBytes {
					continue
				}
				out = append(out, repomap.SourceFile{Rel: rel, Size: st.Size(), MtimeMs: MtimeMs(st.ModTime())})
			}
		}
	}
	walk(root)
	return out
}

// BuildResult is what BuildRepoMap reports (the 1.x buildRepoMap result).
type BuildResult struct {
	Files   int    `json:"files"`
	Symbols int    `json:"symbols"`
	Parsed  int    `json:"parsed"`
	Reused  int    `json:"reused"`
	Ms      int64  `json:"ms"`
	Map     string `json:"map"`
}

// BuildRepoMap builds or refreshes the repo map of cwd and writes the map and its meta. Unchanged
// files are reused from the previous meta unless force is set. now is the clock (nil: time.Now).
func BuildRepoMap(cwd string, force bool, now func() time.Time) (BuildResult, error) {
	if now == nil {
		now = time.Now
	}
	started := now()
	paths := RepoMapPaths(cwd)
	files := ListSourceFiles(cwd)
	prev := map[string]repomap.Entry{}
	if !force {
		if raw, err := os.ReadFile(paths.Meta); err == nil {
			prev = repomap.DecodeMeta(raw)
		}
	}
	read := func(rel string) (string, bool) {
		b, err := os.ReadFile(filepath.Join(cwd, filepath.FromSlash(rel)))
		if err != nil {
			return "", false
		}
		// readFileSync(abs, 'utf8'): a Latin-1 source gets U+FFFD where its bytes are not UTF-8.
		return jscompat.DecodeUTF8(b), true
	}
	next, mapText, parsed, reused := repomap.Build(files, prev, read)
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		return BuildResult{}, err
	}
	if err := WriteAtomic(paths.Map, []byte(mapText)); err != nil {
		return BuildResult{}, err
	}
	builtAt := now()
	if err := WriteAtomic(paths.Meta, repomap.EncodeMeta(float64(builtAt.UnixMilli()), cwd, next)); err != nil {
		return BuildResult{}, err
	}
	symbols := 0
	if mapText != "" {
		symbols = strings.Count(mapText, "\n")
	}
	return BuildResult{Files: len(files), Symbols: symbols, Parsed: parsed, Reused: reused, Ms: now().UnixMilli() - started.UnixMilli(), Map: paths.Map}, nil
}

// Info is the state of an existing index (the 1.x repoMapInfo result).
type Info struct {
	BuiltAt float64 `json:"builtAt"`
	Files   int     `json:"files"`
	Symbols int     `json:"symbols"`
	Map     string  `json:"map"`
	AgeMs   float64 `json:"ageMs"`
}

// RepoMapInfo reads the index stats without rebuilding; nil when there is no index (or its meta
// cannot be read). now is the clock (nil: time.Now).
func RepoMapInfo(cwd string, now func() time.Time) *Info {
	if now == nil {
		now = time.Now
	}
	paths := RepoMapPaths(cwd)
	if _, err := os.Stat(paths.Map); err != nil {
		return nil
	}
	raw, err := os.ReadFile(paths.Meta)
	if err != nil {
		return nil
	}
	// Decoded loosely, like the 1.x: meta.builtAt || 0 and Object.keys(meta.files || {}) accept any
	// type; only JSON null (meta.files throws) or unparsable text gives nil.
	var meta any
	if json.Unmarshal(raw, &meta) != nil || meta == nil {
		return nil
	}
	var entries []any
	files := 0
	switch f := jscompat.Get(meta, "files").(type) {
	case map[string]any:
		files = len(f)
		for _, v := range f {
			entries = append(entries, v)
		}
	case []any:
		files, entries = len(f), f
	case string:
		files = jscompat.UTF16Len(f) // Object.keys of a string: one key per code unit, none with lines
	}
	symbols := 0
	for _, v := range entries {
		if lines, ok := jscompat.Get(v, "lines").([]any); ok {
			symbols += len(lines)
		}
	}
	builtAt := 0.0
	if b := jscompat.Get(meta, "builtAt"); jscompat.Truthy(b) {
		builtAt = jscompat.ToNumber(b) // JS keeps a non-number value as is; here it is its number
	}
	return &Info{BuiltAt: builtAt, Files: files, Symbols: symbols, Map: paths.Map, AgeMs: float64(now().UnixMilli()) - builtAt}
}

// QueryRepoMap queries the map of cwd without a model; without a map the result is the 1.x
// {ok: false, reason: "no-index"}.
func QueryRepoMap(cwd, query string, opts repomap.Opts) repomap.Result {
	raw, err := os.ReadFile(RepoMapPaths(cwd).Map)
	if err != nil {
		return repomap.Result{Reason: "no-index", Rows: []repomap.Row{}}
	}
	return repomap.Query(string(raw), query, opts)
}
