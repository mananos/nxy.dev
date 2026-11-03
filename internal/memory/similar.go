package memory

import (
	"strings"
	"unicode"
)

// SimilarMin is the overlap from which two texts count as similar.
const SimilarMin = 0.3

func words(s string, min int) []string {
	f := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	seen := map[string]bool{}
	var out []string
	for _, w := range f {
		if len([]rune(w)) >= min && !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

// SimilarQuery builds an FTS query (OR) from the words of 4 letters or more.
func SimilarQuery(text string) string {
	ws := words(text, 4)
	for i, w := range ws {
		ws[i] = `"` + w + `"`
	}
	return strings.Join(ws, " OR ")
}

// Overlap is the Jaccard similarity of the normalized words of a and b.
func Overlap(a, b string) float64 {
	wa, wb := words(a, 1), words(b, 1)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	set := map[string]bool{}
	for _, w := range wa {
		set[w] = true
	}
	inter := 0
	for _, w := range wb {
		if set[w] {
			inter++
		}
	}
	return float64(inter) / float64(len(wa)+len(wb)-inter)
}
