package platform

import (
	"testing"
	"time"
)

func TestMtimeMsKeepsFraction(t *testing.T) {
	got := MtimeMs(time.Unix(1700000000, 123456789))
	if want := 1700000000123.456789; got != want {
		t.Fatalf("MtimeMs = %v, want %v", got, want)
	}
	if got := MtimeMs(time.Unix(5, 0)); got != 5000 {
		t.Fatalf("whole second = %v", got)
	}
}
