package platform

import (
	"io"
	"strings"
	"testing"
)

func TestSkipBOM(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"with BOM":  {"\xef\xbb\xbf{\"a\":1}", "{\"a\":1}"},
		"without":   {"{\"a\":1}", "{\"a\":1}"},
		"empty":     {"", ""},
		"one byte":  {"{", "{"},
		"two bytes": {"\xef\xbb", "\xef\xbb"},
	} {
		b, err := io.ReadAll(SkipBOM(strings.NewReader(tc.in)))
		if err != nil || string(b) != tc.want {
			t.Errorf("%s: got %q, %v; want %q", name, b, err, tc.want)
		}
	}
}
