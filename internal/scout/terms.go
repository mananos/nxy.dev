// Package scout is the pure part of the port of core/scout/search.mjs: the terms worth searching in
// a question and the reports of the model-free search (repo map, rg, codegraph) and of the relation
// questions. The engines come in as functions (Engines); running them is internal/app's job and the
// adapters' (internal/tools, internal/platform). The golden of the 1.x is testdata/golden/locate.
//
// Differences from the 1.x:
//   - The repo map query with an explicit `i` flag (`/x/i`) is a regex here (see internal/repomap).
//   - A repo map that cannot be built is reported as degraded "repomap:error" instead of crashing.
package scout

import (
	"regexp"
	"strings"
)

// stopwords are the words of a question that never name code (core/text.mjs STOPWORDS).
var stopwords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`
		donde dónde como cómo cual cuál que qué quien quién para por
		con sin del las los una uno unos unas esta este esto esa
		ese eso estan están hace hacen tiene tienen puede pueden cuando
		cuándo archivo archivos codigo código funcion función metodo método
		clase sobre entre desde hasta todo toda todos todas mas más
		pero porque si no ser son era fue hay muy ya lo la el
		en de un se al su sus nos les aca acá ahi ahí
		where what which when how why who does did the and for
		with without from into this that these those are was were
		file files code function method class about there here happen
		happens handled handle find look show has have its not but
		you your can will should would been being they them`) {
		m[w] = true
	}
	return m
}()

var (
	tokenSplit = regexp.MustCompile(`[^A-Za-z0-9_$./-]+`)
	allDigits  = regexp.MustCompile(`^[0-9]+$`)
)

func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }

// splitIdentifier is t.split(/[._/-]|(?=[A-Z])/): a cut at each of . _ / - (dropped) and before an
// uppercase letter that does not start the string or a part.
func splitIdentifier(t string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(t); i++ {
		c := t[i]
		switch {
		case c == '.' || c == '_' || c == '/' || c == '-':
			parts = append(parts, t[start:i])
			start = i + 1
		case isUpper(c) && i > start:
			parts = append(parts, t[start:i])
			start = i
		}
	}
	return append(parts, t[start:])
}

// QueryTerms pulls the terms worth searching out of a natural-language question. Identifiers win:
// `cancelBooking` or `user_id` is almost always the answer, so it is kept verbatim and split too,
// since the index may hold either spelling. terms are the identifiers then the words, without
// duplicates (case-insensitive), at most 8.
func QueryTerms(question string) (identifiers, terms []string) {
	var words []string
	for _, t := range tokenSplit.Split(question, -1) {
		if t == "" {
			continue
		}
		rest := ""
		if len(t) > 1 {
			rest = t[1:]
		}
		looksLikeID := strings.ContainsFunc(rest, func(r rune) bool { return r >= 'A' && r <= 'Z' }) ||
			strings.ContainsAny(t, "_./")
		low := strings.ToLower(t)
		switch {
		case looksLikeID && len(t) >= 3:
			identifiers = append(identifiers, t)
			for _, part := range splitIdentifier(t) {
				if len(part) >= 4 && !stopwords[strings.ToLower(part)] {
					words = append(words, strings.ToLower(part))
				}
			}
		case len(t) >= 3 && !stopwords[low] && !allDigits.MatchString(t):
			words = append(words, low)
		}
	}
	seen := map[string]bool{}
	terms = []string{}
	for _, t := range append(append([]string{}, identifiers...), words...) {
		k := strings.ToLower(t)
		if seen[k] {
			continue
		}
		seen[k] = true
		terms = append(terms, t)
	}
	if len(terms) > 8 {
		terms = terms[:8]
	}
	if identifiers == nil {
		identifiers = []string{}
	}
	return identifiers, terms
}
