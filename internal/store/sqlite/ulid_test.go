package sqlite

import (
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

func TestULID(t *testing.T) {
	t0 := time.UnixMilli(1_700_000_000_000)
	a := newULID(t0, rand.Reader)
	b := newULID(t0.Add(time.Second), rand.Reader)
	if len(a) != 26 || len(b) != 26 {
		t.Fatalf("length %d/%d", len(a), len(b))
	}
	for _, r := range a + b {
		if !strings.ContainsRune(crockford, r) {
			t.Fatalf("char %q outside alphabet", r)
		}
	}
	if !(a < b) {
		t.Errorf("not time ordered: %s !< %s", a, b)
	}
	if a == newULID(t0, rand.Reader) {
		t.Error("same ULID twice")
	}
}
