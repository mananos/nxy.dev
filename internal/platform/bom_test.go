package platform

import (
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestSkipBOM(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"with BOM":  {"\xef\xbb\xbf{\"a\":1}", "{\"a\":1}"},
		"without":   {"{\"a\":1}", "{\"a\":1}"},
		"empty":     {"", ""},
		"one byte":  {"{", "{"},
		"two bytes": {"\xef\xbb", "\xef\xbb"},
		"diverges":  {"\xefa{\"a\":1}", "\xefa{\"a\":1}"},
		"diverges2": {"\xef\xbbx", "\xef\xbbx"},
		"only BOM":  {"\xef\xbb\xbf", ""},
	} {
		for _, slow := range []bool{false, true} {
			var r io.Reader = strings.NewReader(tc.in)
			if slow {
				r = iotest.OneByteReader(r)
			}
			b, err := io.ReadAll(SkipBOM(r))
			if err != nil || string(b) != tc.want {
				t.Errorf("%s (slow=%v): got %q, %v; want %q", name, slow, b, err, tc.want)
			}
		}
	}
}

type spyReader struct {
	r     io.Reader
	reads int
}

func (s *spyReader) Read(p []byte) (int, error) {
	s.reads++
	return s.r.Read(p)
}

func TestSkipBOMLazy(t *testing.T) {
	spy := &spyReader{r: strings.NewReader("hello")}
	r := SkipBOM(spy)
	if spy.reads != 0 {
		t.Fatalf("wrapping read %d times", spy.reads)
	}
	if _, err := r.Read(make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	if spy.reads == 0 {
		t.Fatal("first Read did not reach the reader")
	}
}

func TestSkipBOMLazyDoesNotWait(t *testing.T) {
	// a source that would block after one byte: the first Read must return with it.
	pr, pw := io.Pipe()
	defer pr.Close()
	go func() { _, _ = pw.Write([]byte("x")) }()
	n, err := SkipBOM(pr).Read(make([]byte, 16))
	if err != nil || n != 1 {
		t.Fatalf("got %d, %v; want 1 byte without waiting", n, err)
	}
}
