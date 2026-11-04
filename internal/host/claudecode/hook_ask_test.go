package claudecode

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/app"
)

func TestEncodeAskWritesTheAskUserQuestionInput(t *testing.T) {
	ask := &app.AskSpec{Question: "¿Con cuál seguimos?", Header: "Notas", Options: []app.AskOption{
		{Label: "N1 · uno", Description: "d"}, {Label: "Ninguna", Description: "x"},
	}}
	m := encode(t, app.HookEvent{Name: "UserPromptSubmit"}, app.HookResponse{Ask: ask})
	h := m["hookSpecificOutput"].(map[string]any)
	ctx := h["additionalContext"].(string)
	for _, want := range []string{"AskUserQuestion", `"question":"¿Con cuál seguimos?"`, `"header":"Notas"`, `"multiSelect":false`, `"label":"N1 · uno"`} {
		if !strings.Contains(ctx, want) {
			t.Errorf("context lacks %q: %s", want, ctx)
		}
	}
}

func TestEncodeStopBlockIsADecisionBlock(t *testing.T) {
	m := encode(t, app.HookEvent{Name: "Stop"}, app.HookResponse{Block: "save a note"})
	if m["decision"] != "block" || m["reason"] != "save a note" {
		t.Errorf("out = %v", m)
	}
}

func TestDecodeSessionEndReason(t *testing.T) {
	ev, err := Decode(strings.NewReader(`{"hook_event_name":"SessionEnd","session_id":"s1","reason":"clear","cwd":"/p"}`))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Name != "SessionEnd" || ev.Reason != "clear" || ev.SessionID != "s1" {
		t.Errorf("event = %+v", ev)
	}
}
