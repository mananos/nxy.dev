package repomap

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/mananos/nxy.dev/internal/jscompat"
)

// AlwaysSkip are directories that never hold source worth indexing, whatever the repo says (the
// 1.x ALWAYS_SKIP).
var AlwaysSkip = map[string]bool{}

func init() {
	for _, d := range strings.Fields(".git node_modules .nxy dist build target out bin obj vendor .venv venv __pycache__ .next .nuxt .svelte-kit coverage .gradle .idea .vscode .cache tmp .terraform site-packages .tox .mypy_cache") {
		AlwaysSkip[d] = true
	}
}

// MaxFileBytes: bigger than this is generated or minified, not source to map.
const MaxFileBytes = 1_000_000

// SourceFile is a file worth indexing, as the walk found it. Rel is slash-separated from the root.
type SourceFile struct {
	Rel     string
	Size    int64
	MtimeMs float64
}

// Entry is what the meta file keeps per source file: its stat and its map lines.
type Entry struct {
	MtimeMs float64
	Size    int64
	Lines   []string
}

// DecodeMeta reads the files of a repomap.meta.json. A parse error, a missing or non-object
// "files" and entries without a numeric mtimeMs/size or a lines array all give nothing, as the
// 1.x treats them (an entry that fails its checks is parsed again).
func DecodeMeta(raw []byte) map[string]Entry {
	out := map[string]Entry{}
	var meta struct {
		Files json.RawMessage `json:"files"`
	}
	if json.Unmarshal(raw, &meta) != nil {
		return out
	}
	var files map[string]json.RawMessage
	if json.Unmarshal(meta.Files, &files) != nil {
		return out
	}
	for rel, r := range files {
		var e struct {
			MtimeMs *float64  `json:"mtimeMs"`
			Size    *float64  `json:"size"`
			Lines   *[]string `json:"lines"`
		}
		if json.Unmarshal(r, &e) != nil || e.MtimeMs == nil || e.Size == nil || e.Lines == nil {
			continue
		}
		size := int64(-1) // a size that is not a whole number never equals a file's
		if *e.Size == math.Trunc(*e.Size) && math.Abs(*e.Size) < 1<<53 {
			size = int64(*e.Size)
		}
		out[rel] = Entry{MtimeMs: *e.MtimeMs, Size: size, Lines: *e.Lines}
	}
	return out
}

// Build refreshes the map: a file whose mtimeMs and size equal the previous entry is reused, any
// other is read (read returns false for an unreadable file, which is skipped) and its symbols
// extracted. mapText has one tab-separated `path line kind name sig` line per symbol, files in
// UTF-16 code-unit order of their path, and is "" when there are none.
func Build(files []SourceFile, prev map[string]Entry, read func(rel string) (string, bool)) (next map[string]Entry, mapText string, parsed, reused int) {
	next = map[string]Entry{}
	for _, f := range files {
		if before, ok := prev[f.Rel]; ok && before.MtimeMs == f.MtimeMs && before.Size == f.Size {
			next[f.Rel] = before
			reused++
			continue
		}
		rs := RulesFor(Ext(f.Rel))
		if rs == nil {
			continue
		}
		text, ok := read(f.Rel)
		if !ok {
			continue
		}
		var lines []string
		for _, s := range ExtractSymbols(text, rs) {
			lines = append(lines, f.Rel+"\t"+strconv.Itoa(s.Line)+"\t"+s.Kind+"\t"+s.Name+"\t"+s.Sig)
		}
		next[f.Rel] = Entry{MtimeMs: f.MtimeMs, Size: f.Size, Lines: lines}
		parsed++
	}
	keys := sortedKeys(next)
	var all []string
	for _, k := range keys {
		all = append(all, next[k].Lines...)
	}
	if len(all) > 0 {
		mapText = strings.Join(all, "\n") + "\n"
	}
	return next, mapText, parsed, reused
}

func sortedKeys(m map[string]Entry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return jscompat.CompareUTF16(keys[i], keys[j]) < 0 })
	return keys
}

// EncodeMeta writes repomap.meta.json the way JSON.stringify does (compact, the same number
// spelling). Files go in path order; the 1.x wrote them in directory order, so readers must not
// depend on it.
func EncodeMeta(builtAt float64, root string, files map[string]Entry) []byte {
	var b strings.Builder
	b.WriteString(`{"builtAt":`)
	b.WriteString(mustJSON(builtAt))
	b.WriteString(`,"root":`)
	b.WriteString(mustJSON(root))
	b.WriteString(`,"files":{`)
	for i, k := range sortedKeys(files) {
		if i > 0 {
			b.WriteByte(',')
		}
		e := files[k]
		lines := e.Lines
		if lines == nil {
			lines = []string{}
		}
		b.WriteString(mustJSON(k) + `:{"mtimeMs":` + mustJSON(e.MtimeMs) + `,"size":` + mustJSON(e.Size) + `,"lines":` + mustJSON(lines) + `}`)
	}
	b.WriteString(`}}`)
	out, err := jscompat.Restringify([]byte(b.String()), "")
	if err != nil {
		return []byte(b.String())
	}
	return out
}

func mustJSON(v any) string {
	if f, ok := v.(float64); ok && (math.IsNaN(f) || math.IsInf(f, 0)) {
		return "null" // what JSON.stringify writes
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(raw)
}
