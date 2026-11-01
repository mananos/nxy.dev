package jscompat

import "strings"

// ICU root collation order of the printable ASCII that is not a letter or digit. Punctuation and
// symbols are non-ignorable and sort before digits, which sort before letters.
const punctOrder = " _-,;:!?.'\"()[]{}@*/\\&#%`^+<=>|~$"

// primary returns the primary weight of an ASCII byte; ok=false for ignorable control characters.
func primary(c byte) (int, bool) {
	switch {
	case c >= 'a' && c <= 'z':
		return 100 + int(c-'a'), true
	case c >= 'A' && c <= 'Z':
		return 100 + int(c-'A'), true
	case c >= '0' && c <= '9':
		return 50 + int(c-'0'), true
	}
	if i := strings.IndexByte(punctOrder, c); i >= 0 {
		return i, true
	}
	return 0, false
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func weights(s string) (prim []int, upper []bool) {
	for i := 0; i < len(s); i++ {
		if w, ok := primary(s[i]); ok {
			prim = append(prim, w)
			upper = append(upper, s[i] >= 'A' && s[i] <= 'Z')
		}
	}
	return
}

// LocaleCompare is a.localeCompare(b) with ICU's root collation (what Node uses with no locale
// data to tell otherwise): negative, zero or positive. For ASCII it compares primary weights
// (case-insensitive, punctuation < digits < letters), then the case at the first difference
// (lowercase first), then code points. Strings with non-ASCII text fall back to code-point
// order, which is NOT what ICU does for accented letters.
func LocaleCompare(a, b string) int {
	if !isASCII(a) || !isASCII(b) {
		return strings.Compare(a, b)
	}
	pa, ua := weights(a)
	pb, ub := weights(b)
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] != pb[i] {
			return sign(pa[i] - pb[i])
		}
	}
	if len(pa) != len(pb) {
		return sign(len(pa) - len(pb))
	}
	for i := range ua {
		if ua[i] != ub[i] {
			if ua[i] {
				return 1
			}
			return -1
		}
	}
	return strings.Compare(a, b)
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}
