package jscompat

import (
	"strings"
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
