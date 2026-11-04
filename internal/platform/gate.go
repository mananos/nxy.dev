package platform

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Gate ledger and one-shot escapes, in the files and format of the 1.x
// (hosts/claude-code/gate-ledger.mjs and gate-state.mjs).

const (
	gateLedgerMaxBytes = 256 * 1024
	gateLedgerKeep     = 90 * 24 * time.Hour
)

// GateLedgerPath is <RuntimeDir>/metrics/gate.jsonl.
func GateLedgerPath(dir string) string {
	return filepath.Join(RuntimeDir(dir), "metrics", "gate.jsonl")
}

// GateRow is one ledger row. Kind is "read" for a search and empty for an edit.
type GateRow struct {
	Ts            int64    `json:"ts"`
	Tool          string   `json:"tool"`
	Allow         bool     `json:"allow"`
	Reason        string   `json:"reason"`
	ContextTokens *float64 `json:"contextTokens"`
	Threshold     float64  `json:"threshold"`
	Kind          string   `json:"kind,omitempty"`
}

// RecordGate appends a row and trims the file when it passes the cap. It never fails the caller.
func RecordGate(dir string, row GateRow) { recordGate(dir, row, gateLedgerMaxBytes, time.Now()) }

func recordGate(dir string, row GateRow, maxBytes int64, now time.Time) {
	path := GateLedgerPath(dir)
	line, err := json.Marshal(row)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, werr := f.Write(append(line, '\n'))
	f.Close()
	if werr != nil {
		return
	}
	if st, err := os.Stat(path); err == nil && st.Size() > maxBytes {
		trimGateLedger(path, maxBytes, now)
	}
}

// trimGateLedger drops rows older than 90 days; if it is still over the cap only the newest rows
// that fit in half of it survive.
func trimGateLedger(path string, maxBytes int64, now time.Time) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	cutoff := float64(now.Add(-gateLedgerKeep).UnixMilli())
	var kept [][]byte
	for _, l := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(l)) == 0 {
			continue
		}
		var r struct {
			Ts any `json:"ts"`
		}
		if json.Unmarshal(l, &r) != nil {
			continue
		}
		if ts, ok := r.Ts.(float64); ok && ts < cutoff {
			continue
		}
		kept = append(kept, l)
	}
	size := func(ls [][]byte) int64 {
		var n int64
		for _, l := range ls {
			n += int64(len(l)) + 1
		}
		return n
	}
	if size(kept) > maxBytes {
		half := maxBytes / 2
		var used int64
		i := len(kept)
		for i > 0 {
			n := int64(len(kept[i-1])) + 1
			if used+n > half {
				break
			}
			used += n
			i--
		}
		kept = kept[i:]
	}
	var out []byte
	for _, l := range kept {
		out = append(out, l...)
		out = append(out, '\n')
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, out, 0o644) != nil || os.Rename(tmp, path) != nil {
		os.Remove(tmp)
	}
}

// GateEscape names one of the two one-shot escapes.
type GateEscape string

const (
	EscapeEdit   GateEscape = "edit"
	EscapeSearch GateEscape = "search"
)

func gateEscapePath(dir string, kind GateEscape) string {
	name := "gate-escape.json"
	if kind == EscapeSearch {
		name = "gate-escape-search.json"
	}
	return filepath.Join(RuntimeDir(dir), name)
}

func escapeWindow(minutes int) time.Duration {
	if minutes < 1 {
		minutes = 1
	}
	return time.Duration(minutes) * time.Minute
}

// ArmGateEscape arms a single bypass and returns when it expires (epoch ms).
func ArmGateEscape(dir string, minutes int, kind GateEscape) (int64, error) {
	return armGateEscape(dir, minutes, kind, time.Now())
}

func armGateEscape(dir string, minutes int, kind GateEscape, now time.Time) (int64, error) {
	until := now.Add(escapeWindow(minutes)).UnixMilli()
	p := gateEscapePath(dir, kind)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return 0, err
	}
	data, _ := json.Marshal(map[string]int64{"until": until})
	return until, os.WriteFile(p, data, 0o644)
}

// GateEscapeUntil is when the armed escape expires (epoch ms), 0 if there is none or it is stale.
func GateEscapeUntil(dir string, minutes int, kind GateEscape) int64 {
	return gateEscapeUntil(dir, minutes, kind, time.Now())
}

func gateEscapeUntil(dir string, minutes int, kind GateEscape, now time.Time) int64 {
	data, err := os.ReadFile(gateEscapePath(dir, kind))
	if err != nil {
		return 0
	}
	var v struct {
		Until any `json:"until"`
	}
	if json.Unmarshal(data, &v) != nil {
		return 0
	}
	until, ok := v.Until.(float64)
	if !ok || until < float64(now.UnixMilli()) {
		return 0
	}
	// An escape can never outlive its own window, whatever the file claims.
	max := now.Add(escapeWindow(minutes)).UnixMilli()
	if int64(until) > max {
		return max
	}
	return int64(until)
}

// ConsumeGateEscape uses up the escape if one is valid; true means "let this one through".
func ConsumeGateEscape(dir string, minutes int, kind GateEscape) bool {
	return consumeGateEscape(dir, minutes, kind, time.Now())
}

// afterEscapeCheck is a test seam between the validity check and the remove.
var afterEscapeCheck func()

func consumeGateEscape(dir string, minutes int, kind GateEscape, now time.Time) bool {
	if gateEscapeUntil(dir, minutes, kind, now) == 0 {
		return false
	}
	if afterEscapeCheck != nil {
		afterEscapeCheck()
	}
	// Only a successful remove lets the edit through. If the file is already gone
	// (another consumer won the race) or cannot be removed, deny: the one-shot
	// escape must never pass twice nor leave the gate open forever.
	return os.Remove(gateEscapePath(dir, kind)) == nil
}
