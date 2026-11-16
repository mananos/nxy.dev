package platform

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestTUINonceValid(t *testing.T) {
	dir := t.TempDir()
	nonce, stop := HeartbeatNonce(dir)
	now := time.Now()
	if !TUINonceValid(dir, nonce, now) {
		t.Fatal("nonce of a live TUI rejected")
	}
	if TUINonceValid(dir, "", now) || TUINonceValid(dir, nonce+"x", now) || TUINonceValid(dir, nonce, now.Add(time.Minute)) {
		t.Error("bad, empty or stale nonce accepted")
	}
	stop()
	if TUINonceValid(dir, nonce, time.Now()) {
		t.Error("nonce accepted after the TUI stopped")
	}
}

func TestHeartbeatNonceStopRemovesMark(t *testing.T) {
	dir := t.TempDir()
	nonce, stop := HeartbeatNonce(dir)
	p := filepath.Join(dir, "tui-"+strconv.Itoa(os.Getpid()))
	if _, err := os.Stat(p); err != nil {
		t.Fatal("the mark is not there")
	}
	stop()
	stop()
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("the mark survived stop")
	}
	if TUIAlive(dir, time.Now()) || TUINonceValid(dir, nonce, time.Now()) {
		t.Fatal("a stopped heartbeat still counts as a live TUI")
	}
}

func TestTUIAlive(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if TUIAlive(dir, now) || TUIAlive(filepath.Join(dir, "nope"), now) {
		t.Fatal("sin archivos no hay TUI viva")
	}
	p := filepath.Join(dir, "tui-1")
	if err := os.WriteFile(p, []byte("1 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !TUIAlive(dir, now) {
		t.Fatal("un archivo recién escrito es una TUI viva")
	}
	old := now.Add(-10 * time.Second)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	if TUIAlive(dir, now) {
		t.Fatal("un archivo viejo no cuenta")
	}
	_ = os.WriteFile(filepath.Join(dir, "other"), nil, 0o644)
	if TUIAlive(dir, now) {
		t.Fatal("sólo cuentan los tui-*")
	}
}

func TestHeartbeatAndSweep(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	stop := Heartbeat(dir)
	if !TUIAlive(dir, time.Now()) {
		t.Fatal("el latido no se ve")
	}
	stop()
	stop()
	if TUIAlive(dir, time.Now()) {
		t.Fatal("stop debió borrar el archivo")
	}
	old := filepath.Join(dir, "tui-999999")
	fresh := filepath.Join(dir, "tui-1000000")
	_ = os.WriteFile(old, nil, 0o644)
	_ = os.WriteFile(fresh, nil, 0o644)
	past := time.Now().Add(-time.Minute)
	_ = os.Chtimes(old, past, past)
	SweepHeartbeats(dir, time.Now())
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("el viejo no se barrió")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("el fresco se barrió")
	}
}
