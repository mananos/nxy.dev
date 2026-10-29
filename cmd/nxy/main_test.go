package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/version"
)

// isolate keeps the real user config, settings and rtk out of a test.
func isolate(t *testing.T) {
	t.Helper()
	d := t.TempDir()
	t.Setenv("NXY_HOME", d)
	t.Setenv("HOME", d)
	t.Setenv("USERPROFILE", d)
	t.Setenv("NXY_RTK_PATH", "")
}

func TestHookEvents(t *testing.T) {
	isolate(t)
	cases := []struct{ name, tool string }{
		{"SessionStart", ""},
		{"PreToolUse", "Bash"},
		{"PreToolUse", "Edit"},
		{"PreToolUse", "Grep"},
		{"PreToolUse", "Agent"},
		{"PreToolUse", "AskUserQuestion"},
		{"PostToolUse", "Bash"},
		{"PostToolUse", "Agent"},
		{"UserPromptSubmit", ""},
		{"SubagentStop", ""},
		{"Stop", ""},
	}
	for _, c := range cases {
		payload := `{"hook_event_name":"` + c.name + `","tool_name":"` + c.tool + `","session_id":"s1","cwd":"/tmp"}`
		var out, errb bytes.Buffer
		if code := run([]string{"hook"}, strings.NewReader(payload), &out, &errb); code != 0 {
			t.Errorf("%s/%s: exit %d", c.name, c.tool, code)
		}
		if out.Len() != 0 {
			t.Errorf("%s/%s: stdout %q", c.name, c.tool, out.String())
		}
	}
}

func TestHookGarbage(t *testing.T) {
	isolate(t)
	var out, errb bytes.Buffer
	if code := run([]string{"hook"}, strings.NewReader("garbage"), &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout %q", out.String())
	}
	if errb.Len() == 0 {
		t.Fatal("expected a stderr line")
	}
}

func TestVersion(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"version"}, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.TrimSpace(out.String()) != version.Version {
		t.Fatalf("got %q", out.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"nope"}, strings.NewReader(""), &out, &errb); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if code := run(nil, strings.NewReader(""), &out, &errb); code != 1 {
		t.Fatalf("no args: exit %d", code)
	}
}
