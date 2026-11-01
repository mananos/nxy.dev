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
