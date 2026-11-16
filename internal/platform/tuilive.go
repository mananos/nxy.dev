package platform

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A live TUI touches <UserDir>/run/tui-<pid> every heartbeatEvery; it counts as alive while the
// newest file is younger than aliveWithin. Fixed by nxy, not configurable.
const (
	heartbeatEvery = 2 * time.Second
	aliveWithin    = 5 * time.Second
	tuiPrefix      = "tui-"
)

// RunDir is the folder of the live-TUI marks: <UserDir>/run.
func RunDir() (string, error) {
	u, err := UserDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(u, "run"), nil
}

// Heartbeat writes tui-<pid> in dir now and every 2 s until stop is called, which removes it.
func Heartbeat(dir string) (stop func()) {
	_, stop = HeartbeatNonce(dir)
	return stop
}

// NonceEnv is the variable the TUI sets on the subprocesses of its buttons: the proof that
// `nxy action decide` comes from a live TUI and not from the model.
const NonceEnv = "NXY_TUI_NONCE"

// HeartbeatNonce is Heartbeat that also returns the random nonce written in the mark
// ("<pid> <unix> <nonce>").
func HeartbeatNonce(dir string) (nonce string, stop func()) {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	nonce = hex.EncodeToString(b)
	path := filepath.Join(dir, fmt.Sprintf("%s%d", tuiPrefix, os.Getpid()))
	write := func() {
		if os.MkdirAll(dir, 0o700) == nil {
			_ = os.WriteFile(path, []byte(fmt.Sprintf("%d %d %s\n", os.Getpid(), time.Now().Unix(), nonce)), 0o600)
		}
	}
	write()
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		t := time.NewTicker(heartbeatEvery)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				write()
			}
		}
	}()
	var once sync.Once
	return nonce, func() {
		once.Do(func() {
			close(done)
			<-finished
			_ = os.Remove(path)
		})
	}
}

// TUINonceValid reports whether nonce is the one of some TUI alive at now.
func TUINonceValid(dir, nonce string, now time.Time) bool {
	if nonce == "" {
		return false
	}
	ok := false
	eachHeartbeat(dir, func(path string, mt time.Time) {
		if now.Sub(mt) >= aliveWithin {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		f := strings.Fields(string(data))
		if len(f) >= 3 && subtle.ConstantTimeCompare([]byte(f[2]), []byte(nonce)) == 1 {
			ok = true
		}
	})
	return ok
}

// TUIAlive reports whether some tui-* file in dir was touched less than 5 s before now.
func TUIAlive(dir string, now time.Time) bool {
	alive := false
	eachHeartbeat(dir, func(_ string, mt time.Time) {
		if now.Sub(mt) < aliveWithin {
			alive = true
		}
	})
	return alive
}

// SweepHeartbeats removes the tui-* files that are no longer fresh (a TUI that died).
func SweepHeartbeats(dir string, now time.Time) {
	eachHeartbeat(dir, func(path string, mt time.Time) {
		if now.Sub(mt) >= aliveWithin {
			_ = os.Remove(path)
		}
	})
}

func eachHeartbeat(dir string, fn func(path string, mtime time.Time)) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), tuiPrefix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fn(filepath.Join(dir, e.Name()), info.ModTime())
	}
}
