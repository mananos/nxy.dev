package app

import "context"

// HookEvent is a host hook call, already decoded by an adapter.
type HookEvent struct {
	Name           string
	Tool           string
	SessionID      string
	Cwd            string
	TranscriptPath string
}

// HookResponse is what a hook answers. The zero value says nothing.
type HookResponse struct{}

// HandleHook answers a hook event. In F0 it answers nothing for every event.
func HandleHook(ctx context.Context, ev HookEvent) HookResponse {
	return HookResponse{}
}
