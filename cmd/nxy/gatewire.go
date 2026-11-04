package main

import (
	"github.com/mananos/nxy.dev/internal/app"
	"github.com/mananos/nxy.dev/internal/gate"
	"github.com/mananos/nxy.dev/internal/host/claudecode"
	"github.com/mananos/nxy.dev/internal/platform"
)

// gateFiles is the gate's config, escapes and ledger on disk (per checkout, files of the 1.x).
type gateFiles struct{ getenv func(string) string }

func (g gateFiles) Settings(dir string) gate.Config {
	return gateConfigFrom(platform.LoadConfig(dir, g.getenv))
}

// gateConfigFrom reads the gate section: enabled and reads are on unless set to false.
func gateConfigFrom(cfg map[string]any) gate.Config {
	m, _ := cfg["gate"].(map[string]any)
	out := gate.Config{Enabled: true, ContextTokens: 100000, EscapeMinutes: 5, Reads: true}
	if v, ok := m["enabled"].(bool); ok {
		out.Enabled = v
	}
	if v, ok := m["reads"].(bool); ok {
		out.Reads = v
	}
	if v, ok := m["contextTokens"].(float64); ok {
		out.ContextTokens = v
	}
	if v, ok := m["escapeMinutes"].(float64); ok && v >= 1 {
		out.EscapeMinutes = int(v)
	}
	return out
}

func gateKind(k app.GateEscape) platform.GateEscape {
	if k == app.GateEscapeSearch {
		return platform.EscapeSearch
	}
	return platform.EscapeEdit
}

func (g gateFiles) Arm(dir string, kind app.GateEscape) (int64, error) {
	return platform.ArmGateEscape(dir, g.Settings(dir).EscapeMinutes, gateKind(kind))
}

func (g gateFiles) Until(dir string, kind app.GateEscape) int64 {
	return platform.GateEscapeUntil(dir, g.Settings(dir).EscapeMinutes, gateKind(kind))
}

func (g gateFiles) Consume(dir string, kind app.GateEscape) bool {
	return platform.ConsumeGateEscape(dir, g.Settings(dir).EscapeMinutes, gateKind(kind))
}


func (gateFiles) Record(dir string, r app.GateRecord) {
	platform.RecordGate(dir, platform.GateRow{
		Ts: r.Ts, Tool: r.Tool, Allow: r.Allow, Reason: r.Reason,
		ContextTokens: r.ContextTokens, Threshold: r.Threshold, Kind: r.Kind,
	})
}

func (gateFiles) Rows(dir string) []gate.Row {
	var out []gate.Row
	for _, v := range platform.ReadJSONL(platform.GateLedgerPath(dir)) {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		r := gate.Row{}
		r.Kind, _ = m["kind"].(string)
		r.Allow, _ = m["allow"].(bool)
		if c, ok := m["contextTokens"].(float64); ok {
			r.ContextTokens = &c
		}
		out = append(out, r)
	}
	return out
}

// claudeContext measures the main context from the Claude Code transcript.
type claudeContext struct{}

func (claudeContext) MainContextTokens(path string) (float64, bool) {
	return claudecode.MainContextTokens(path)
}
