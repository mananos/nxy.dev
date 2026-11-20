package platform

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func gateRowAt(ts int64) GateRow {
	return GateRow{Ts: ts, Tool: "Edit", Allow: true, Reason: "text", Threshold: 100000}
}

func ledgerLines(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(GateLedgerPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

func TestGateLedgerAppendsRows(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	recordGate(dir, gateRowAt(now.UnixMilli()), 1<<20, now)
	recordGate(dir, GateRow{Ts: now.UnixMilli(), Tool: "Grep", Kind: "read"}, 1<<20, now)
	lines := ledgerLines(t, dir)
	if len(lines) != 2 || !strings.Contains(lines[1], `"kind":"read"`) || strings.Contains(lines[0], `"kind"`) {
		t.Fatalf("lines = %v", lines)
	}
}

func TestGateLedgerPrunesOldRowsWhenOverCap(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	old := now.Add(-100 * 24 * time.Hour).UnixMilli()
	recordGate(dir, gateRowAt(old), 1<<20, now)
	recordGate(dir, gateRowAt(now.UnixMilli()), 1<<20, now)
	// A tiny cap forces the trim; the recent row still fits in it.
	recordGate(dir, gateRowAt(now.UnixMilli()), 250, now)
	if n := len(ledgerLines(t, dir)); n != 2 {
		t.Fatalf("rows = %d, want the 2 recent ones", n)
	}
	for _, l := range ledgerLines(t, dir) {
		if strings.Contains(l, `"ts":`+itoa(old)) {
			t.Fatalf("old row survived: %v", l)
		}
	}
}

func TestGateLedgerKeepsNewestHalfWhenStillTooBig(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for i := 0; i < 20; i++ {
		recordGate(dir, gateRowAt(now.UnixMilli()+int64(i)), 1<<20, now)
	}
	one := int64(len(ledgerLines(t, dir)[0]) + 1)
	maxBytes := one * 10
	last := gateRowAt(now.UnixMilli() + 99)
	last.Reason = "newest"
	recordGate(dir, last, maxBytes, now)
	lines := ledgerLines(t, dir)
	if int64(len(lines))*one > maxBytes/2+one || len(lines) < 3 {
		t.Fatalf("kept %d rows of %d bytes with cap %d", len(lines), one, maxBytes)
	}
	if !strings.Contains(lines[len(lines)-1], "newest") {
		t.Fatalf("newest row lost: %v", lines[len(lines)-1])
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

func TestGateEscapeArmConsumeOnce(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	until, err := armGateEscape(dir, 5, EscapeEdit, now)
	if err != nil || until != now.Add(5*time.Minute).UnixMilli() {
		t.Fatalf("until = %d, err = %v", until, err)
	}
	if gateEscapeUntil(dir, 5, EscapeSearch, now) != 0 {
		t.Fatal("the search escape must be independent")
	}
	if !consumeGateEscape(dir, 5, EscapeEdit, now) {
		t.Fatal("first consume should pass")
	}
	if consumeGateEscape(dir, 5, EscapeEdit, now) {
		t.Fatal("second consume should not pass")
	}
}

func TestGateEscapeLostRaceDenies(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if _, err := armGateEscape(dir, 5, EscapeEdit, now); err != nil {
		t.Fatal(err)
	}
	// Another consumer removes the file after our validity check.
	afterEscapeCheck = func() { _ = os.Remove(gateEscapePath(dir, EscapeEdit)) }
	defer func() { afterEscapeCheck = nil }()
	if consumeGateEscape(dir, 5, EscapeEdit, now) {
		t.Fatal("a consumer that lost the race must be denied")
	}
}

func TestGateEscapeExpiresAndIsCappedByWindow(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if _, err := armGateEscape(dir, 5, EscapeSearch, now); err != nil {
		t.Fatal(err)
	}
	if consumeGateEscape(dir, 5, EscapeSearch, now.Add(6*time.Minute)) {
		t.Fatal("expired escape must not pass")
	}
	if _, err := armGateEscape(dir, 60, EscapeEdit, now); err != nil {
		t.Fatal(err)
	}
	if got := gateEscapeUntil(dir, 5, EscapeEdit, now); got != now.Add(5*time.Minute).UnixMilli() {
		t.Fatalf("until = %d, want capped to the 5 min window", got)
	}
}
