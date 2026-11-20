package sqlite

import (
	"io"
	"time"
)

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// newULID builds a 26-char ULID: 48-bit unix ms timestamp + 80 bits of entropy, Crockford base32.
func newULID(t time.Time, entropy io.Reader) string {
	var b [16]byte
	ms := uint64(t.UnixMilli())
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
	if _, err := io.ReadFull(entropy, b[6:]); err != nil {
		panic("sqlite: entropy: " + err.Error())
	}
	out := make([]byte, 26)
	// 128 bits -> 26 groups of 5 bits (first group holds the top 3 bits).
	var acc uint
	bits := 0
	idx := 25
	for i := 15; i >= 0; i-- {
		acc |= uint(b[i]) << bits
		bits += 8
		for bits >= 5 {
			out[idx] = crockford[acc&31]
			idx--
			acc >>= 5
			bits -= 5
		}
	}
	if bits > 0 {
		out[idx] = crockford[acc&31]
	}
	return string(out)
}
