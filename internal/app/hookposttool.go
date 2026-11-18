package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/filter"
	"github.com/mananos/nxy.dev/v2/internal/jscompat"
	"github.com/mananos/nxy.dev/v2/internal/report"
)

// bashResponseText is the output of a Bash call: stdout and stderr when not empty, joined by a
// newline; older versions passed a string.
func bashResponseText(raw json.RawMessage) string {
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return ""
	}
	switch r := v.(type) {
	case string:
		return r
	case map[string]any:
		var parts []string
		for _, k := range []string{"stdout", "stderr"} {
			if s, ok := r[k].(string); ok && s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// postToolBash adds one Bash call to the filter summary of its session in nxy.db (what the 1.x
// wrote as a row of filter.jsonl). It never answers and never stops anything.
func (h Hooks) postToolBash(ctx context.Context, ev HookEvent) {
	if h.LegacyStore == nil || h.LegacyData == nil || h.Config == nil || !ev.HasCommand || ev.SessionID == "" {
		return
	}
	s := h.Config.Filter(ev.ProjectDir)
	if !s.Metrics {
		return
	}
	g := report.FilterGroup{Engine: "off", Kind: filter.CommandKind(ev.Command), Rows: 1}
	if s.Enabled {
		if filter.UsesRtk(ev.Command) {
			g.Engine = "rtk"
		} else {
			var info filter.EngineInfo
			if h.Engine != nil {
				info = h.Engine.Info(s, ev.ProjectDir)
			}
			// Replay the decision without the engine to learn why the command was not rewritten.
			d := filter.Decide(ev.Command, filter.Context{
				Tool:            ev.Tool,
				Engine:          info.Engine,
				RtkHookDetected: info.RtkHookDetected,
				ExcludeCommands: s.ExcludeCommands,
				OnlyCommands:    s.OnlyCommands,
				Rewrite:         func(string) (filter.RewriteResult, error) { return filter.RewriteResult{Code: 1}, nil },
			})
			g.Engine, g.Reason = "skip", d.Reason
		}
	}
	chars := jscompat.UTF16Len(bashResponseText(ev.ToolResponse))
	g.Chars = float64(chars)
	g.Tokens = float64((chars + 3) / 4)
	_ = h.LegacyStore.AddFilterGroup(ctx, h.LegacyData.Checkout(ev.ProjectDir), ev.SessionID, g)
}
