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

func TestDecodeAskedQuestions(t *testing.T) {
	in := `{"hook_event_name":"PreToolUse","tool_name":"AskUserQuestion","cwd":"/p","tool_input":{"questions":[
	  {"question":"¿Uno?","header":"H","multiSelect":true,"options":[{"label":"A","description":"da"},{"label":"B"}]}]}}`
	ev, err := Decode(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.Asked) != 1 || ev.Asked[0].Question != "¿Uno?" || ev.Asked[0].Header != "H" || !ev.Asked[0].MultiSelect ||
		len(ev.Asked[0].Options) != 2 || ev.Asked[0].Options[1] != "B" || len(ev.Questions) != 1 {
		t.Errorf("asked = %+v", ev.Asked)
	}
}

func TestEncodeAnswersKeepTheOriginalQuestions(t *testing.T) {
	input := `{"questions":[{"question":"¿Uno?","options":[{"label":"A","description":"da"}]}]}`
	ev := app.HookEvent{Name: "PreToolUse", Tool: "AskUserQuestion", ToolInput: []byte(input)}
	m := encode(t, ev, app.HookResponse{Answers: &app.AskAnswers{ByQuestion: map[string]string{"¿Uno?": "A"}}})
	h := m["hookSpecificOutput"].(map[string]any)
	if h["permissionDecision"] != "allow" || h["permissionDecisionReason"] != "nxy: contestada en la TUI" {
		t.Errorf("out = %v", h)
	}
	upd := h["updatedInput"].(map[string]any)
	if upd["answers"].(map[string]any)["¿Uno?"] != "A" {
		t.Errorf("answers = %v", upd["answers"])
	}
	qs := upd["questions"].([]any)
	opt := qs[0].(map[string]any)["options"].([]any)[0].(map[string]any)
	if opt["description"] != "da" {
		t.Errorf("questions lost their descriptions: %v", qs)
	}
}

func TestEncodeTitleIsATerminalSequenceEvenAlone(t *testing.T) {
	m := encode(t, app.HookEvent{Name: "PostToolUse", Tool: "AskUserQuestion"}, app.HookResponse{Title: app.TitlePending})
	if m["terminalSequence"] != "\x1b]2;● nxy: decisión pendiente\x07" {
		t.Errorf("pending = %q", m["terminalSequence"])
	}
	m = encode(t, app.HookEvent{Name: "PostToolUse", Tool: "AskUserQuestion"}, app.HookResponse{Title: app.TitleRestore})
	if m["terminalSequence"] != "\x1b]2;\x07" {
		t.Errorf("restore = %q", m["terminalSequence"])
	}
}

func TestEncodeTitleRidesWithTheContext(t *testing.T) {
	m := encode(t, app.HookEvent{Name: "PostToolUse", Tool: "AskUserQuestion"}, app.HookResponse{Context: "x", Title: app.TitleRestore})
	if m["terminalSequence"] == nil || m["hookSpecificOutput"] == nil {
		t.Errorf("out = %v", m)
	}
}

func TestEncodeNoTitleNoOutput(t *testing.T) {
	var b strings.Builder
	if err := Encode(&b, app.HookEvent{Name: "PostToolUse"}, app.HookResponse{}); err != nil || b.Len() != 0 {
		t.Errorf("out = %q, err = %v", b.String(), err)
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
