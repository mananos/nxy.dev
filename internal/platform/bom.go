package platform

import (
	"bufio"
	"bytes"
	"io"
)

// SkipBOM drops a leading UTF-8 BOM (PowerShell 5.1 prepends one when it pipes text).
func SkipBOM(r io.Reader) io.Reader {
	br := bufio.NewReader(r)
	if head, _ := br.Peek(3); bytes.Equal(head, []byte("\xef\xbb\xbf")) {
		_, _ = br.Discard(3)
	}
	return br
}
