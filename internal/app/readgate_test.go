package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/gate"
)

type fakeGate struct {
	cfg     gate.Config
	armed   map[GateEscape]int
	records []GateRecord
}

func newFakeGate() *fakeGate {
	return &fakeGate{cfg: gate.Config{Enabled: true, ContextTokens: 100000, EscapeMinutes: 5, Reads: true}, armed: map[GateEscape]int{}}
}

func (g *fakeGate) Settings(string) gate.Config { return g.cfg }
func (g *fakeGate) Arm(_ string, k GateEscape) (int64, error) {
	g.armed[k]++
	return 1, nil
}
func (g *fakeGate) Until(string, GateEscape) int64 { return 0 }
func (g *fakeGate) Consume(_ string, k GateEscape) bool {
	if g.armed[k] > 0 {
		g.armed[k]--
		return true
	}
	return false
}
func (g *fakeGate) Record(_ string, r GateRecord) { g.records = append(g.records, r) }
func (g *fakeGate) Rows(string) []gate.Row        { return nil }

type fakeMeter struct {
	tokens float64
	ok     bool
}

func (m fakeMeter) MainContextTokens(string) (float64, bool) { return m.tokens, m.ok }

type hintSessions struct {
	*fakeSessions
	hints int
}

func (s *hintSessions) MarkReadHint(context.Context, string, int64) (bool, error) {
	s.hints++
	return s.hints == 1, nil
}

func gateHooks(t *testing.T) (Hooks, *fakeGate, *memStore, string) {
	t.Helper()
	root := t.TempDir()
	h, st := flowHooks(false)
	g := newFakeGate()
	h.Gate = g
	h.Context = fakeMeter{tokens: 150000, ok: true}
	h.Sessions = &hintSessions{fakeSessions: newFakeSessions()}
	return h, g, st, root
}

func writeEv(root, file, agent string) HookEvent {
	return HookEvent{Name: "PreToolUse", Tool: "Write", FilePath: file, AgentID: agent, ProjectDir: root, Cwd: root,
		SessionID: "s1", TranscriptPath: "t.jsonl"}
}

func TestWriteGateWithoutFeature(t *testing.T) {
	ctx := context.Background()
	h, g, _, root := gateHooks(t)
	code := filepath.Join(root, "a", "b.go")

	r := h.Handle(ctx, writeEv(root, code, ""))
	if r.Deny == "" || !strings.Contains(r.Deny, "/nxy:gate once") || !strings.Contains(r.Deny, "`/nxy:feature`") {
		t.Fatalf("code file: deny = %q", r.Deny)
	}
	if r := h.Handle(ctx, writeEv(root, filepath.Join(root, "README.md"), "")); r.Deny != "" {
		t.Errorf("markdown stopped: %q", r.Deny)
	}
	if r := h.Handle(ctx, writeEv(root, filepath.Join(t.TempDir(), "x.go"), "")); r.Deny != "" {
		t.Errorf("outside the repo stopped: %q", r.Deny)
	}
	if r := h.Handle(ctx, writeEv(root, code, "agent-1")); r.Deny != "" {
		t.Errorf("subagent stopped: %q", r.Deny)
	}

	// the escape lets exactly one through
	g.Arm("", GateEscapeEdit)
	if r := h.Handle(ctx, writeEv(root, code, "")); r.Deny != "" {
		t.Errorf("escape: %q", r.Deny)
	}
	if r := h.Handle(ctx, writeEv(root, code, "")); r.Deny == "" {
		t.Errorf("second edit after the escape went through")
	}

	// ledger: code(deny), text, outside is not recorded, escape, code(deny)
	var reasons []string
	for _, rec := range g.records {
		reasons = append(reasons, rec.Reason)
		if rec.ContextTokens == nil || *rec.ContextTokens != 150000 {
			t.Errorf("context not measured: %+v", rec)
		}
	}
	if got := strings.Join(reasons, ","); got != "code,text,escape,code" {
		t.Errorf("ledger reasons = %s", got)
	}
	if g.records[0].Allow || !g.records[1].Allow || !g.records[2].Allow {
		t.Errorf("ledger allow flags = %+v", g.records)
	}
}

func TestWriteGateDisabledOrAbsent(t *testing.T) {
	ctx := context.Background()
	h, g, _, root := gateHooks(t)
	g.cfg.Enabled = false
	if r := h.Handle(ctx, writeEv(root, filepath.Join(root, "a.go"), "")); r.Deny != "" || len(g.records) != 0 {
		t.Errorf("disabled gate acted: %q %v", r.Deny, g.records)
	}
	h.Gate = nil
	if r := h.Handle(ctx, writeEv(root, filepath.Join(root, "a.go"), "")); r.Deny != "" {
		t.Errorf("nil gate acted: %q", r.Deny)
	}
}

func TestFeatureFreezeUnchangedAndLedgered(t *testing.T) {
	ctx := context.Background()
	h, g, st, root := gateHooks(t)
	seed(t, h, st, root, approvalState, approvalNext)
	r := h.Handle(ctx, writeEv(root, filepath.Join(root, "a.md"), ""))
	if !strings.Contains(r.Deny, "`nxy feature stop`") {
		t.Fatalf("feature freeze changed: %q", r.Deny)
	}
	if len(g.records) != 1 || g.records[0].Allow || g.records[0].Reason != "feature" {
		t.Errorf("ledger = %+v", g.records)
	}
	b := HookEvent{Name: "PreToolUse", Tool: "Bash", Command: "echo x > a.go", HasCommand: true, ProjectDir: root, Cwd: root}
	if r := h.Handle(ctx, b); !strings.Contains(r.Deny, "`nxy feature stop`") {
		t.Errorf("shell freeze changed: %q", r.Deny)
	}
}

func TestShellWriteToCodeIsGated(t *testing.T) {
	ctx := context.Background()
	h, g, _, root := gateHooks(t)
	h.Config = fakeConfig{}
	bash := func(cmd, agent string) HookEvent {
		return HookEvent{Name: "PreToolUse", Tool: "Bash", Command: cmd, HasCommand: true, ProjectDir: root, Cwd: root, AgentID: agent, SessionID: "s1"}
	}
	if r := h.Handle(ctx, bash("echo x > a.go", "")); r.Deny == "" {
		t.Errorf("shell write to .go not stopped")
	}
	if r := h.Handle(ctx, bash("echo x > notes.txt", "")); r.Deny != "" {
		t.Errorf("shell write to text stopped: %q", r.Deny)
	}
	g.Arm("", GateEscapeEdit)
	if r := h.Handle(ctx, bash("echo x > a.go", "")); r.Deny != "" {
		t.Errorf("escape did not let the shell write through: %q", r.Deny)
	}
	if r := h.Handle(ctx, bash("echo x > a.go", "agent-1")); r.Deny != "" {
		t.Errorf("subagent shell write stopped: %q", r.Deny)
	}
}

func TestShellEscapeCoversAllCodeTargets(t *testing.T) {
	ctx := context.Background()
	h, g, _, root := gateHooks(t)
	h.Config = fakeConfig{}
	bash := func(cmd string) HookEvent {
		return HookEvent{Name: "PreToolUse", Tool: "Bash", Command: cmd, HasCommand: true, ProjectDir: root, Cwd: root, SessionID: "s1"}
	}
	g.Arm("", GateEscapeEdit)
	if r := h.Handle(ctx, bash("echo x > a.go && echo y > b.go")); r.Deny != "" {
		t.Errorf("one escape must let a two-file command through: %q", r.Deny)
	}
	if r := h.Handle(ctx, bash("echo x > c.go")); r.Deny == "" {
		t.Errorf("escape should be spent after the command")
	}
	g.Arm("", GateEscapeEdit)
	if r := h.Handle(ctx, bash("echo x > notes.txt")); r.Deny != "" {
		t.Errorf("text write stopped: %q", r.Deny)
	}
	if r := h.Handle(ctx, bash("echo x > d.go")); r.Deny != "" {
		t.Errorf("a text-only command must not spend the escape: %q", r.Deny)
	}
}

func grepEv(root string, in map[string]any) HookEvent {
	raw, _ := json.Marshal(in)
	return HookEvent{Name: "PreToolUse", Tool: "Grep", ToolInput: raw, ProjectDir: root, Cwd: root, SessionID: "s1", TranscriptPath: "t.jsonl"}
}

func TestReadGate(t *testing.T) {
	ctx := context.Background()
	h, g, _, root := gateHooks(t)
	broad := map[string]any{"pattern": "TODO"}

	r := h.Handle(ctx, grepEv(root, broad))
	if !strings.HasPrefix(r.Deny, "nxy read gate:") || !strings.Contains(r.Deny, "/nxy:gate reads off") || !strings.Contains(r.Deny, "`TODO`") {
		t.Fatalf("first refusal = %q", r.Deny)
	}
	r = h.Handle(ctx, grepEv(root, broad))
	if r.Deny == "" || strings.Contains(r.Deny, "reads off") {
		t.Errorf("second refusal must not repeat the hint: %q", r.Deny)
	}
	if rec := g.records[0]; rec.Kind != "read" || rec.Allow || rec.Reason != "over-threshold" || rec.Threshold != 100000 {
		t.Errorf("ledger row = %+v", rec)
	}

	// one-shot search escape
	g.Arm("", GateEscapeSearch)
	if r := h.Handle(ctx, grepEv(root, broad)); r.Deny != "" {
		t.Errorf("escape: %q", r.Deny)
	}
	if last := g.records[len(g.records)-1]; !last.Allow || last.Reason != "escape" {
		t.Errorf("escape row = %+v", last)
	}
	if r := h.Handle(ctx, grepEv(root, broad)); r.Deny == "" {
		t.Errorf("escape lasted for more than one search")
	}

	// a Glob is broad too; a Grep of one file is not
	raw, _ := json.Marshal(map[string]any{"pattern": "**/*.go"})
	gl := HookEvent{Name: "PreToolUse", Tool: "Glob", ToolInput: raw, ProjectDir: root, Cwd: root, TranscriptPath: "t"}
	if r := h.Handle(ctx, gl); r.Deny == "" {
		t.Errorf("glob not stopped")
	}
	if r := h.Handle(ctx, grepEv(root, map[string]any{"pattern": "x", "path": "main.go"})); r.Deny != "" {
		t.Errorf("one-file grep stopped: %q", r.Deny)
	}

	// subagents are never stopped
	ev := grepEv(root, broad)
	ev.AgentID = "a1"
	if r := h.Handle(ctx, ev); r.Deny != "" {
		t.Errorf("subagent stopped: %q", r.Deny)
	}

	// under the threshold, unknown context, reads off
	h.Context = fakeMeter{tokens: 50000, ok: true}
	if r := h.Handle(ctx, grepEv(root, broad)); r.Deny != "" {
		t.Errorf("under threshold stopped")
	}
	h.Context = fakeMeter{ok: false}
	if r := h.Handle(ctx, grepEv(root, broad)); r.Deny != "" {
		t.Errorf("unknown context must fail open")
	}
	h.Context = nil
	if r := h.Handle(ctx, grepEv(root, broad)); r.Deny != "" {
		t.Errorf("no meter must fail open")
	}
	h.Context = fakeMeter{tokens: 150000, ok: true}
	g.cfg.Reads = false
	if r := h.Handle(ctx, grepEv(root, broad)); r.Deny != "" {
		t.Errorf("reads off stopped")
	}
}

func TestReadGateOnBroadShellSearch(t *testing.T) {
	ctx := context.Background()
	h, _, _, root := gateHooks(t)
	h.Config = fakeConfig{}
	bash := func(cmd string) HookEvent {
		return HookEvent{Name: "PreToolUse", Tool: "Bash", Command: cmd, HasCommand: true, ProjectDir: root, Cwd: root, TranscriptPath: "t"}
	}
	if r := h.Handle(ctx, bash("grep -rn TODO .")); !strings.HasPrefix(r.Deny, "nxy read gate:") {
		t.Errorf("broad grep not stopped: %q", r.Deny)
	}
	if r := h.Handle(ctx, bash("go test ./...")); r.Deny != "" {
		t.Errorf("plain command stopped: %q", r.Deny)
	}
}
