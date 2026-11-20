package jscompat

import (
	"testing"
	"time"
)

// setLocal swaps time.Local for the test and restores it afterwards.
func setLocal(t *testing.T, loc *time.Location) {
	t.Helper()
	old := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = old })
}

func TestParseDateLocalFixedZone(t *testing.T) {
	setLocal(t, time.FixedZone("m3", -3*3600))
	cases := []struct {
		in   string
		want string // the same instant, written with an explicit offset
	}{
		{"2026-10-31T12:00:00", "2026-10-31T12:00:00-03:00"},
		{"2026-10-31T12:00", "2026-10-31T12:00:00-03:00"},
		{"2026-10-31T12:00:00.123", "2026-10-31T12:00:00.123-03:00"},
		{"2026-10-31T00:00:00.9999", "2026-10-31T00:00:00.999-03:00"},
	}
	for _, c := range cases {
		got, ok := ParseDate(c.in)
		want, ok2 := ParseDate(c.want)
		if !ok || !ok2 || got != want {
			t.Errorf("ParseDate(%q) = %d,%v; want %d,%v", c.in, got, ok, want, ok2)
		}
	}
	// date-only stays UTC even with a local zone
	got, _ := ParseDate("2026-10-31")
	want, _ := ParseDate("2026-10-31T00:00:00Z")
	if got != want {
		t.Errorf("date-only = %d, want UTC %d", got, want)
	}
}

func TestParseDateLocalDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata missing: %v", err)
	}
	setLocal(t, loc)
	cases := []struct{ in, want string }{
		{"2026-07-01T12:00:00", "2026-07-01T12:00:00-04:00"}, // EDT
		{"2026-01-15T12:00:00", "2026-01-15T12:00:00-05:00"}, // EST
	}
	for _, c := range cases {
		got, ok := ParseDate(c.in)
		want, _ := ParseDate(c.want)
		if !ok || got != want {
			t.Errorf("ParseDate(%q) = %d,%v; want %d", c.in, got, ok, want)
		}
	}
}
