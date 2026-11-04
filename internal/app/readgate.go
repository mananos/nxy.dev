package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mananos/nxy.dev/internal/gate"
	"github.com/mananos/nxy.dev/internal/searchscope"
)

// preToolSearch is the read gate for the Grep and Glob tools of the main thread.
func (h Hooks) preToolSearch(ctx context.Context, ev HookEvent) HookResponse {
	if h.Gate == nil || ev.AgentID != "" || len(ev.ToolInput) == 0 {
		return HookResponse{}
	}
	var input map[string]any
	if json.Unmarshal(ev.ToolInput, &input) != nil {
		return HookResponse{}
	}
	if !searchscope.IsBroadMainSearch(ev.Tool, input) {
		return HookResponse{}
	}
	r, _ := h.readGate(ctx, ev, ev.Tool, input)
	return r
}

// readGate is mainThreadSearch of the 1.x: a broad search past the context threshold is refused,
// unless the one-shot search escape is armed. Anything unknown passes. denied is true when r refuses.
func (h Hooks) readGate(ctx context.Context, ev HookEvent, tool string, input map[string]any) (r HookResponse, denied bool) {
	if h.Gate == nil || ev.AgentID != "" {
		return HookResponse{}, false
	}
	dir := ev.ProjectDir
	cfg := h.Gate.Settings(dir)
	if !cfg.Enabled || !cfg.Reads {
		return HookResponse{}, false
	}
	isFile := func(p string) bool {
		if h.Files == nil {
			return searchscope.LooksLikeFile(p)
		}
		p = h.Files.NativePath(p)
		return h.Files.Exists(p) && !h.Files.IsDir(p)
	}
	cls := searchscope.Classify(tool, input, isFile)
	kind, _ := cls.Kind.(string)
	if !cls.Broad {
		return HookResponse{}, false
	}
	tokens := h.mainContext(ev)
	in := gate.ReadInput{ContextTokens: tokens, Kind: kind, Broad: cls.Broad}
	if gate.DecideRead(in, cfg).Allow {
		return HookResponse{}, false
	}
	escaped := h.Gate.Consume(dir, GateEscapeSearch)
	reason := "over-threshold"
	if escaped {
		reason = "escape"
	}
	h.Gate.Record(dir, GateRecord{
		Ts: h.now().UnixMilli(), Tool: tool, Allow: escaped, Reason: reason,
		ContextTokens: tokens, Threshold: cfg.ContextTokens, Kind: "read",
	})
	if escaped {
		return HookResponse{}, false
	}
	first := false
	if h.Sessions != nil && ev.SessionID != "" {
		first, _ = h.Sessions.MarkReadHint(ctx, ev.SessionID, h.now().UnixMilli())
	}
	pattern := ""
	switch p := cls.Pattern.(type) {
	case string:
		pattern = p
	case nil:
	default:
		pattern = fmt.Sprint(p)
	}
	msg := gate.ReadGateMessage(gate.Search{Kind: kind, Pattern: pattern}, *tokens, cfg.ContextTokens, first)
	return HookResponse{Deny: msg}, true
}
