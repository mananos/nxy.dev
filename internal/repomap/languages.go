package repomap

import (
	"regexp"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/jscompat"
)

// Rule is one symbol pattern: the first capture group is the name.
type Rule struct {
	Kind string
	Re   *regexp.Regexp
}

// Symbol is one extracted definition (Line is 1-based).
type Symbol struct {
	Line int
	Kind string
	Name string
	Sig  string
}

// jsToGo turns a JS regex source into RE2: \s (outside and inside classes) becomes the JS
// whitespace set, everything else these patterns use means the same in both engines.
func jsToGo(src string) string {
	var b strings.Builder
	inClass := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '\\' && i+1 < len(src):
			if src[i+1] == 's' {
				if inClass {
					b.WriteString(jscompat.SpaceClass)
				} else {
					b.WriteString("[" + jscompat.SpaceClass + "]")
				}
			} else {
				b.WriteByte(c)
				b.WriteByte(src[i+1])
			}
			i++
			continue
		case c == '[' && !inClass:
			inClass = true
		case c == ']' && inClass:
			inClass = false
		}
		b.WriteByte(c)
	}
	return b.String()
}

func rules(pairs ...string) []Rule {
	out := make([]Rule, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, Rule{Kind: pairs[i], Re: regexp.MustCompile(jsToGo(pairs[i+1]))})
	}
	return out
}

var (
	jsRules = rules(
		"class", `^\s*(?:export\s+(?:default\s+)?)?(?:abstract\s+)?class\s+([A-Za-z_$][\w$]*)`,
		"function", `^\s*(?:export\s+(?:default\s+)?)?(?:async\s+)?function\s*\*?\s*([A-Za-z_$][\w$]*)`,
		"const", `^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?(?:\([^)]*\)|[A-Za-z_$][\w$]*)\s*=>`,
		"interface", `^\s*(?:export\s+)?interface\s+([A-Za-z_$][\w$]*)`,
		"type", `^\s*(?:export\s+)?type\s+([A-Za-z_$][\w$]*)\s*[=<]`,
		"enum", `^\s*(?:export\s+)?(?:const\s+)?enum\s+([A-Za-z_$][\w$]*)`,
		// class members: two-space-ish indent, not a control keyword, followed by a call signature
		"method", `^\s{2,}(?:(?:public|private|protected|static|readonly|async|get|set|override)\s+)*([A-Za-z_$][\w$]*)\s*\([^)]*\)\s*(?::[^{;]+)?\s*\{`,
	)
	javaRules = rules(
		"class", `^\s*(?:(?:public|private|protected|static|final|abstract|sealed)\s+)*class\s+([A-Za-z_][\w$]*)`,
		"interface", `^\s*(?:(?:public|private|protected|static|sealed)\s+)*interface\s+([A-Za-z_][\w$]*)`,
		"enum", `^\s*(?:(?:public|private|protected|static)\s+)*enum\s+([A-Za-z_][\w$]*)`,
		"record", `^\s*(?:(?:public|private|protected|static|final)\s+)*record\s+([A-Za-z_][\w$]*)`,
		"method", `^\s+(?:(?:public|private|protected|static|final|abstract|synchronized|native|default)\s+)+(?:<[^>]+>\s*)?[\w$<>\[\],.?\s]+\s+([A-Za-z_][\w$]*)\s*\([^;]*$`,
		"endpoint", `^\s*@(?:Get|Post|Put|Delete|Patch|Request)Mapping\s*\(\s*(?:value\s*=\s*)?"([^"]+)"`,
	)
	scalaRules = rules(
		"object", `^\s*(?:(?:private|protected|implicit|case)\s+)*object\s+([A-Za-z_][\w$]*)`,
		"class", `^\s*(?:(?:private|protected|abstract|final|sealed|implicit|case)\s+)*class\s+([A-Za-z_][\w$]*)`,
		"trait", `^\s*(?:(?:private|protected|sealed)\s+)*trait\s+([A-Za-z_][\w$]*)`,
		"def", `^\s*(?:(?:private|protected|override|implicit|final)\s+)*def\s+([A-Za-z_][\w$]*)`,
	)
	pyRules = rules(
		"class", `^\s*class\s+([A-Za-z_]\w*)`,
		"def", `^\s*(?:async\s+)?def\s+([A-Za-z_]\w*)`,
		// Route decorators carry the name people actually search for; the def line is indexed too.
		"endpoint", `^\s*@(?:[\w.]+)\.(?:route|get|post|put|delete|patch|websocket)\s*\(\s*['"]([^'"]+)['"]`,
		// Module-level constants: config and registry values are searched by name.
		"const", `^([A-Z][A-Z0-9_]{2,})\s*(?::[^=]+)?=`,
	)
	goRules = rules(
		"func", `^func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)`,
		"type", `^type\s+([A-Za-z_]\w*)`,
	)
	csRules = rules(
		"class", `^\s*(?:(?:public|private|protected|internal|static|sealed|abstract|partial)\s+)*class\s+([A-Za-z_]\w*)`,
		"interface", `^\s*(?:(?:public|private|protected|internal)\s+)*interface\s+([A-Za-z_]\w*)`,
		"method", `^\s+(?:(?:public|private|protected|internal|static|virtual|override|async|sealed)\s+)+[\w<>\[\],.?\s]+\s+([A-Za-z_]\w*)\s*\([^;]*$`,
	)
)

type extRules struct {
	ext   string
	rules []Rule
}

// byExt keeps the insertion order of the 1.x BY_EXT object (IndexedExtensions reads it).
var byExt = []extRules{
	{".js", jsRules}, {".mjs", jsRules}, {".cjs", jsRules}, {".jsx", jsRules},
	{".ts", jsRules}, {".tsx", jsRules}, {".mts", jsRules}, {".cts", jsRules},
	{".java", javaRules},
	{".scala", scalaRules}, {".sc", scalaRules},
	{".py", pyRules}, {".pyi", pyRules},
	{".go", goRules},
	{".cs", csRules},
}

// RulesFor returns the rules of an extension (case-insensitive, with the dot) or nil.
func RulesFor(ext string) []Rule {
	ext = strings.ToLower(ext)
	for _, e := range byExt {
		if e.ext == ext {
			return e.rules
		}
	}
	return nil
}

// IndexedExtensions lists every extension the index reads symbols from, in the 1.x order.
func IndexedExtensions() []string {
	out := make([]string, len(byExt))
	for i, e := range byExt {
		out[i] = e.ext
	}
	return out
}

// reserved are keywords the shallow patterns would otherwise pick up as names.
var reserved = map[string]bool{}

func init() {
	for _, w := range strings.Fields("if for while switch catch return function class new do else try finally await typeof delete throw case with yield super this") {
		reserved[w] = true
	}
}

// ExtractSymbols reads the symbols of one file's text: the first matching rule wins per line,
// blank, comment-only and over-long (400 UTF-16 units, minified) lines are skipped.
func ExtractSymbols(text string, rs []Rule) []Symbol {
	var out []Symbol
	for i, raw := range strings.Split(text, "\n") {
		// a byte length up to 400 is at most 400 units: only longer lines need the UTF-16 count
		if raw == "" || len(raw) > 400 && jscompat.UTF16Len(raw) > 400 {
			continue
		}
		t := jscompat.TrimStart(raw)
		if t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "/*") {
			continue
		}
		for _, r := range rs {
			m := r.Re.FindStringSubmatch(raw)
			if m == nil {
				continue
			}
			name := m[1]
			if name == "" || reserved[name] {
				break
			}
			out = append(out, Symbol{Line: i + 1, Kind: r.Kind, Name: name, Sig: jscompat.UTF16Prefix(jscompat.Trim(raw), 160)})
			break
		}
	}
	return out
}

// Ext is Node's path.extname: the last dot of the base name to its end; a name that starts with a
// dot and has no other has none, and "a." gives ".". Only "/" separates (the map uses forward slashes).
func Ext(p string) string {
	startDot, startPart, end, preDot := -1, 0, -1, 0
	matchedSlash := true
	for i := len(p) - 1; i >= 0; i-- {
		c := p[i]
		if c == '/' {
			if !matchedSlash {
				startPart = i + 1
				break
			}
			continue
		}
		if end == -1 {
			matchedSlash = false
			end = i + 1
		}
		if c == '.' {
			if startDot == -1 {
				startDot = i
			} else if preDot != 1 {
				preDot = 1
			}
		} else if startDot != -1 {
			preDot = -1
		}
	}
	if startDot == -1 || end == -1 || preDot == 0 || (preDot == 1 && startDot == end-1 && startDot == startPart+1) {
		return ""
	}
	return p[startDot:end]
}
