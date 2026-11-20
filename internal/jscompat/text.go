package jscompat

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

func units(r rune) int {
	if r >= 0x10000 {
		return 2
	}
	return 1
}

// UTF16Len is s.length in JS: code units, so a character outside the BMP counts 2.
func UTF16Len(s string) int {
	n := 0
	for _, r := range s {
		n += units(r)
	}
	return n
}

// UTF16Prefix is s.slice(0, n). A cut that would split a surrogate pair stops before the pair
// (a Go string cannot hold half of it).
func UTF16Prefix(s string, n int) string {
	if n <= 0 {
		return ""
	}
	used := 0
	for i, r := range s {
		u := units(r)
		if used+u > n {
			return s[:i]
		}
		used += u
	}
	return s
}

// pad builds the filler for padStart/padEnd: fill repeated and cut at n UTF-16 units.
func pad(s string, n int, fill string) string {
	missing := n - UTF16Len(s)
	if missing <= 0 || fill == "" {
		return ""
	}
	var b strings.Builder
	for UTF16Len(b.String()) < missing {
		b.WriteString(fill)
	}
	return UTF16Prefix(b.String(), missing)
}

// PadStart is s.padStart(n, fill), counted in UTF-16 units like .length. An empty fill returns s.
func PadStart(s string, n int, fill string) string { return pad(s, n, fill) + s }

// PadEnd is s.padEnd(n, fill).
func PadEnd(s string, n int, fill string) string { return s + pad(s, n, fill) }

// CollapseSpace is s.replace(/\s+/g, " ") with the JS \s set (NBSP, U+2028/2029, U+FEFF,
// U+2000..U+200A, ...).
func CollapseSpace(s string) string {
	var b strings.Builder
	inSpace := false
	for _, r := range s {
		if isSpace(r) {
			if !inSpace {
				b.WriteByte(' ')
			}
			inSpace = true
			continue
		}
		inSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

// Trim is s.trim() with the JS whitespace set.
func Trim(s string) string { return strings.TrimFunc(s, isSpace) }

// TrimEnd is s.trimEnd().
func TrimEnd(s string) string { return strings.TrimRightFunc(s, isSpace) }

// TrimStart is s.trimStart() with the JS whitespace set.
func TrimStart(s string) string { return strings.TrimLeftFunc(s, isSpace) }

// CompareUTF16 orders a and b by UTF-16 code unit, like the default JS sort() and the < operator
// on strings (a character outside the BMP sorts before U+E000..U+FFFF, unlike byte order). It
// returns -1, 0 or 1.
func CompareUTF16(a, b string) int {
	// Walk both strings rune by rune without allocating: up to the first different rune the
	// orders agree, and only that pair needs its UTF-16 units compared.
	for a != "" && b != "" {
		ra, na := utf8.DecodeRuneInString(a)
		rb, nb := utf8.DecodeRuneInString(b)
		if ra != rb {
			return compareRuneUnits(ra, rb)
		}
		a, b = a[na:], b[nb:]
	}
	switch {
	case a == "" && b != "":
		return -1
	case a != "" && b == "":
		return 1
	}
	return 0
}

// compareRuneUnits orders two different runes by their UTF-16 units (an invalid byte decodes to
// U+FFFD, as []rune does).
func compareRuneUnits(a, b rune) int {
	a1, a2 := firstUnits(a)
	b1, b2 := firstUnits(b)
	if a1 != b1 {
		if a1 < b1 {
			return -1
		}
		return 1
	}
	if a2 < b2 {
		return -1
	}
	return 1
}

func firstUnits(r rune) (rune, rune) {
	if r < 0x10000 {
		return r, 0
	}
	hi, lo := utf16.EncodeRune(r)
	return hi, lo
}

// DecodeUTF8 is what Node's buffer.toString('utf8') (and readFileSync(path, 'utf8')) gives: valid
// UTF-8 as is, and each maximal invalid subpart replaced by one U+FFFD (the WHATWG decoder), unlike
// strings.ToValidUTF8, which collapses a whole run of invalid bytes into one. A BOM is kept.
func DecodeUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var out strings.Builder
	out.Grow(len(b) + 8)
	var cp rune
	needed, seen := 0, 0
	lower, upper := byte(0x80), byte(0xBF)
	for i := 0; i < len(b); i++ {
		c := b[i]
		if needed == 0 {
			switch {
			case c <= 0x7F:
				out.WriteByte(c)
			case c >= 0xC2 && c <= 0xDF:
				needed, cp = 1, rune(c&0x1F)
			case c >= 0xE0 && c <= 0xEF:
				if c == 0xE0 {
					lower = 0xA0
				} else if c == 0xED {
					upper = 0x9F
				}
				needed, cp = 2, rune(c&0x0F)
			case c >= 0xF0 && c <= 0xF4:
				if c == 0xF0 {
					lower = 0x90
				} else if c == 0xF4 {
					upper = 0x8F
				}
				needed, cp = 3, rune(c&0x07)
			default:
				out.WriteRune(utf8.RuneError)
			}
			continue
		}
		if c < lower || c > upper {
			cp, needed, seen = 0, 0, 0
			lower, upper = 0x80, 0xBF
			out.WriteRune(utf8.RuneError)
			i-- // the byte starts over as the first of a new sequence
			continue
		}
		lower, upper = 0x80, 0xBF
		cp = cp<<6 | rune(c&0x3F)
		seen++
		if seen == needed {
			out.WriteRune(cp)
			cp, needed, seen = 0, 0, 0
		}
	}
	if needed != 0 {
		out.WriteRune(utf8.RuneError)
	}
	return out.String()
}

// SpaceClass is the JS \s set as the body of a Go regexp character class: put it inside [...]
// (or [^...]) to get what \s (or \S) means in JS; Go's own \s is ASCII only.
const SpaceClass = `\x{09}\x{0A}\x{0B}\x{0C}\x{0D}\x{20}\x{A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}`

// ReplaceUnits is s.replace(/[^...]/g, repl) done per UTF-16 unit: keep decides on each unit
// (a character outside the BMP is two units, passed as its surrogates) and every unit not kept
// becomes repl, so one emoji gives repl twice.
func ReplaceUnits(s string, keep func(unit rune) bool, repl string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x10000 {
			v := r - 0x10000
			for _, u := range [2]rune{0xD800 + v>>10, 0xDC00 + v&0x3FF} {
				if keep(u) {
					b.WriteRune(utf8.RuneError) // a lone surrogate cannot be written; never kept in practice
				} else {
					b.WriteString(repl)
				}
			}
			continue
		}
		if keep(r) {
			b.WriteRune(r)
		} else {
			b.WriteString(repl)
		}
	}
	return b.String()
}
