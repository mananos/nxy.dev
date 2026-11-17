package gate

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/mananos/nxy.dev/internal/jscompat"
	"github.com/mananos/nxy.dev/internal/searchscope"
)

// Config is the gate section of the config (core/gate.mjs GateConfig). Reads false means broad
// searches are observed only; the loader defaults it to true.
type Config struct {
	Enabled       bool
	ContextTokens float64
	EscapeMinutes int
	Reads         bool
}

// CodeExtensions is the set of extensions the write gate treats as code: the ones the repo map
// indexes plus the extension of every test glob of the affected runners (.kt, .groovy...). Lowercase,
// with the dot, first seen first.
func CodeExtensions(indexed []string, runnerGlobs []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(e string) {
		e = strings.ToLower(e)
		if len(e) < 2 || e[0] != '.' || seen[e] {
			return
		}
		seen[e] = true
		out = append(out, e)
	}
	for _, e := range indexed {
		add(e)
	}
	for _, g := range runnerGlobs {
		add(extOf(g))
	}
	return out
}

// extOf is the extension of the last path element (both separators), "" when it has none.
func extOf(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		p = p[i+1:]
	}
	i := strings.LastIndexByte(p, '.')
	if i <= 0 || i == len(p)-1 {
		return ""
	}
	return p[i:]
}

// IsCodeFile reports whether path has one of the code extensions (case-insensitive).
func IsCodeFile(path string, exts []string) bool {
	e := strings.ToLower(extOf(path))
	if e == "" {
		return false
	}
	for _, x := range exts {
		if x == e {
			return true
		}
	}
	return false
}

// WriteInput is what the write gate knows about an edit made without an active feature.
type WriteInput struct {
	IsSubagent   bool
	InsideRepo   bool
	IsCode       bool
	EscapeActive bool
}

// Decision is a verdict: Reason is one of code, escape, text (and subagent, outside, disabled).
type Decision struct {
	Allow  bool
	Reason string
}

// DecideWrite is the write gate without a feature: the main thread may edit text and docs, and may
// edit code only with a one-shot escape. The implementer and files outside the repo are never stopped.
func DecideWrite(in WriteInput, cfg Config) Decision {
	switch {
	case !cfg.Enabled:
		return Decision{true, "disabled"}
	case in.IsSubagent:
		return Decision{true, "subagent"}
	case !in.InsideRepo:
		return Decision{true, "outside"}
	case !in.IsCode:
		return Decision{true, "text"}
	case in.EscapeActive:
		return Decision{true, "escape"}
	}
	return Decision{false, "code"}
}

// WriteGateMessage is the refusal text of the write gate.
func WriteGateMessage(tool, file string) string {
	target := ""
	if file != "" {
		target = " `" + file + "`"
	}
	what := "edit"
	switch tool {
	case "":
	case "Bash", "PowerShell":
		what = "write"
	default:
		what = tool
	}
	return strings.Join([]string{
		"nxy: no feature is active — the main thread does not " + what + " code" + target + " itself.",
		"",
		"Start a feature with `/nxy:feature`, or dispatch the `implementer` subagent if it is a one-off, with: the exact `path:line`, the concrete change, and an acceptance criterion.",
		"Docs and plain text are not stopped.",
		"",
		"If doing it here really is cheaper, the user can run `/nxy:gate once`.",
	}, "\n")
}

// ReadInput is what the read gate knows about a search (decideReadGate without hasScout).
type ReadInput struct {
	ContextTokens *float64 // nil when unknown
	IsSubagent    bool
	EscapeActive  bool
	Kind          string // "" when the search has no kind
	Broad         bool
}

// DecideRead is decideReadGate of core/gate.mjs: a broad grep/glob/find past the threshold is
// refused; anything unknown passes.
func DecideRead(in ReadInput, cfg Config) Decision {
	if !cfg.Enabled || !cfg.Reads {
		return Decision{true, "disabled"}
	}
	if in.IsSubagent {
		return Decision{true, "subagent"}
	}
	if !in.Broad || !(in.Kind == "grep" || in.Kind == "glob" || in.Kind == "find") {
		return Decision{true, "not-broad-search"}
	}
	if in.ContextTokens == nil || math.IsNaN(*in.ContextTokens) || math.IsInf(*in.ContextTokens, 0) {
		return Decision{true, "context-unknown"}
	}
	t := cfg.ContextTokens
	if math.IsNaN(t) || math.IsInf(t, 0) || t <= 0 {
		return Decision{true, "no-threshold"}
	}
	if *in.ContextTokens < t {
		return Decision{true, "under-threshold"}
	}
	if in.EscapeActive {
		return Decision{true, "escape"}
	}
	return Decision{false, "over-threshold"}
}

// Search is the part of a classified search the refusal text names.
type Search struct {
	Kind    string
	Pattern string
}

func num(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }

// k is the `k` helper of core/gate.mjs: 1234 -> "1k", below 1000 as is.
func k(n float64) string {
	if n >= 1000 {
		return num(jscompat.Round(n/1000)) + "k"
	}
	return num(n)
}

// ReadGateMessage is readGateMessage of core/gate.mjs. It starts with searchscope.ReadGateMark.
func ReadGateMessage(s Search, contextTokens, threshold float64, firstInSession bool) string {
	what := ""
	if s.Pattern != "" {
		what = " for `" + jscompat.UTF16Prefix(s.Pattern, 80) + "`"
	}
	kind := s.Kind
	if kind == "" {
		kind = "search"
	}
	lines := []string{
		searchscope.ReadGateMark + " main thread context is " + k(contextTokens) + " > " + k(threshold) + " — a broad " + kind + what + " would pull its results into every later call.",
		"",
		"Dispatch `nxy:scout` with the question (several scouts in one message for several questions); it answers with `path:line`.",
		"Or narrow this to one file, or a `Read` with offset/limit — those are never stopped.",
		"",
		"If searching here really is cheaper, the user can run `/nxy:gate once search` to let one search through.",
	}
	if firstInSession {
		lines = append(lines, "To stop this in this repo: `/nxy:gate reads off`")
	}
	return strings.Join(lines, "\n")
}

// Row is one ledger row (hosts/claude-code/gate-ledger.mjs). Kind "read" is a search, "" an edit.
type Row struct {
	Kind          string
	Allow         bool
	ContextTokens *float64
}

// Summary is what the ledger says, as the three lines of `/nxy:gate status`: decisions, context at
// edit (only when some edit measured it) and searches. No leading blank line.
func Summary(rows []Row) []string {
	var edits, searches []Row
	for _, r := range rows {
		if r.Kind == "read" {
			searches = append(searches, r)
		} else {
			edits = append(edits, r)
		}
	}
	var out []string
	if len(edits) > 0 {
		blocked := 0
		var ctx []float64
		for _, r := range edits {
			if !r.Allow {
				blocked++
			}
			if r.ContextTokens != nil {
				ctx = append(ctx, *r.ContextTokens)
			}
		}
		pct := jscompat.Round(float64(blocked) / float64(len(edits)) * 100)
		out = append(out, "decisiones:         "+strconv.Itoa(blocked)+" de "+strconv.Itoa(len(edits))+" ediciones bloqueadas ("+num(pct)+"%)")
		if len(ctx) > 0 {
			sort.Float64s(ctx)
			q := func(p float64) float64 {
				i := int(math.Floor(float64(len(ctx)) * p))
				if i > len(ctx)-1 {
					i = len(ctx) - 1
				}
				return ctx[i]
			}
			kk := func(n float64) string { return num(jscompat.Round(n/1000)) + "k" }
			out = append(out, "contexto al editar: p10 "+kk(q(0.1))+" - p50 "+kk(q(0.5))+" - p90 "+kk(q(0.9))+" - max "+kk(ctx[len(ctx)-1]))
		}
	} else {
		out = append(out, "decisiones:         todavía no pasó ninguna edición por el gate en este repo")
	}
	stopped, escaped := 0, 0
	for _, r := range searches {
		if r.Allow {
			escaped++
		} else {
			stopped++
		}
	}
	out = append(out, "búsquedas:          "+strconv.Itoa(stopped)+" frenadas, "+strconv.Itoa(escaped)+" dejadas pasar con `/nxy:gate once search`")
	return out
}

// Status is what StatusHead describes. Until values are epoch milliseconds, 0 when not armed.
type Status struct {
	Cfg               Config
	EscapeUntil       int64
	SearchEscapeUntil int64
	NowMs             int64
}

// StatusHead is the top of `nxy gate status`: the write gate by file type, the read gate with its
// threshold, and the one-shot escapes.
func StatusHead(s Status) []string {
	left := func(t int64) string {
		if t > s.NowMs {
			return "armado, quedan " + strconv.FormatInt((t-s.NowMs+59999)/60000, 10) + " min"
		}
		return "no"
	}
	write := "activo (sin una feature, el hilo principal no edita archivos de código; docs y texto pasan)"
	read := "apagado (no se frenan las búsquedas amplias)"
	if !s.Cfg.Enabled {
		write = "DESACTIVADO"
		read = "apagado (el gate está desactivado)"
	} else if s.Cfg.Reads {
		read = "activo (se frena un grep/glob/find amplio con más de " + k(s.Cfg.ContextTokens) + " tokens de contexto en el hilo principal)"
	}
	return []string{
		"gate de escritura:  " + write,
		"gate de lectura:    " + read,
		"edición única:      " + left(s.EscapeUntil),
		"escape de búsqueda: " + left(s.SearchEscapeUntil),
	}
}
