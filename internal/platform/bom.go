package platform

import "io"

var bom = []byte{0xef, 0xbb, 0xbf}

// SkipBOM drops a leading UTF-8 BOM (PowerShell 5.1 prepends one when it pipes text).
// It does not touch r until the first Read, so a command that never reads stdin
// never blocks on it.
func SkipBOM(r io.Reader) io.Reader { return &bomReader{r: r} }

type bomReader struct {
	r       io.Reader
	checked bool
	pending []byte // bytes read while checking that turned out not to be a BOM
	err     error  // error hit while checking, delivered after pending
}

func (b *bomReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if !b.checked {
		b.checked = true
		b.check()
	}
	if len(b.pending) > 0 {
		n := copy(p, b.pending)
		b.pending = b.pending[n:]
		return n, nil
	}
	if b.err != nil {
		return 0, b.err
	}
	return b.r.Read(p)
}

// check reads one byte at a time, and only while they match the BOM prefix.
func (b *bomReader) check() {
	var buf [1]byte
	var head []byte
	for len(head) < len(bom) {
		n, err := b.r.Read(buf[:])
		if n > 0 {
			head = append(head, buf[0])
			if buf[0] != bom[len(head)-1] {
				b.pending = head
				return
			}
		}
		if err != nil {
			if len(head) < len(bom) {
				b.pending = head
			}
			b.err = err
			return
		}
	}
	// full BOM: dropped
}
