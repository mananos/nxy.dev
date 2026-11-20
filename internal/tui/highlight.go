package tui

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

var lexerCache = map[string]chroma.Lexer{}

// lexerFor is the lexer of a language name, else the one that matches the file name; nil when
// there is none.
func lexerFor(lang, path string) chroma.Lexer {
	key := lang + "|" + path
	if lx, ok := lexerCache[key]; ok {
		return lx
	}
	lx := lexers.Get(lang)
	if lx == nil {
		lx = lexers.Match(path)
	}
	if lx != nil {
		lx = chroma.Coalesce(lx)
	}
	lexerCache[key] = lx
	return lx
}

// tokenColor maps a chroma token type to a token of the theme (never to a chroma style, so the
// colors follow the dark and light palettes).
func tokenColor(t chroma.TokenType) tok {
	switch {
	case t == chroma.NameDecorator:
		return tNeed
	case t == chroma.KeywordType, t == chroma.NameClass, t == chroma.NameBuiltin, t == chroma.NameBuiltinPseudo:
		return tAc
	case t == chroma.NameFunction, t == chroma.NameFunctionMagic:
		return tWork
	case t.InCategory(chroma.Keyword):
		return tVer
	case t.InCategory(chroma.LiteralString):
		return tDone
	case t.InCategory(chroma.Comment):
		return tSb
	}
	return tFg
}

// highlight cuts one line of code in segments colored by the kind of token. Without a lexer the
// text comes out in the default color. A name that starts with a capital is a type and a name
// followed by ( is a call, the same two guesses the mockup makes for languages whose lexer does
// not tell them apart.
func highlight(lang, path, code string) []seg {
	return highlightLines(lang, path, []string{code})[0]
}

// highlightLines colors a run of lines tokenising them once together, so a comment or a string
// that spans several lines keeps its color on each of them. The result has one entry per line.
func highlightLines(lang, path string, codes []string) [][]seg {
	out := make([][]seg, len(codes))
	if len(codes) == 0 {
		return out
	}
	for i := range codes {
		codes[i] = strings.ReplaceAll(codes[i], "\t", "    ")
		out[i] = []seg{T(codes[i], tFg)}
	}
	lx := lexerFor(lang, path)
	if lx == nil {
		return out
	}
	it, err := lx.Tokenise(nil, strings.Join(codes, "\n"))
	if err != nil {
		return out
	}
	toks := it.Tokens()
	res := make([][]seg, len(codes))
	n := 0
	for i, tk := range toks {
		c := tokenColor(tk.Type)
		if c == tFg && tk.Type.InCategory(chroma.Name) && tk.Type != chroma.NameDecorator && tk.Value != "" {
			switch r := tk.Value[0]; {
			case r >= 'A' && r <= 'Z':
				c = tAc
			case i+1 < len(toks) && strings.HasPrefix(toks[i+1].Value, "("):
				c = tWork
			}
		}
		for j, part := range strings.Split(tk.Value, "\n") {
			if j > 0 {
				n++
			}
			if part == "" || n >= len(res) {
				continue
			}
			res[n] = append(res[n], T(part, c))
		}
	}
	for i := range res {
		if len(res[i]) > 0 {
			out[i] = res[i]
		}
	}
	return out
}
