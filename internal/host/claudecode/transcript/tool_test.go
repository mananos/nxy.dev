package transcript

import (
	"strings"
	"testing"
)

func asst(blocks string) string {
	return `{"type":"assistant","message":{"content":[` + blocks + `]}}` + "\n"
}

func use(name, input string) string {
	return `{"type":"tool_use","id":"t","name":"` + name + `","input":` + input + `}`
}

func TestLastToolUse(t *testing.T) {
	long := strings.Repeat("x", 80)
	cases := []struct {
		name     string
		data     string
		wantName string
		wantArg  string
		wantOK   bool
	}{
		{"at the end", asst(use("Read", `{"file_path":"/a/b/Foo.java"}`)), "Read", "Foo.java", true},
		{"in the middle", asst(use("Edit", `{"file_path":"/a/Bar.go"}`)) +
			`{"type":"user","message":{"content":"ok"}}` + "\n" +
			asst(`{"type":"text","text":"done"}`), "Edit", "Bar.go", true},
		{"last block of a line", asst(use("Read", `{"file_path":"/a/One.go"}`) + "," + use("Grep", `{"pattern":"foo.*bar"}`)), "Grep", "foo.*bar", true},
		{"partial trailing line", asst(use("Read", `{"file_path":"/a/Ok.go"}`)) +
			`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/a/Cut`, "Read", "Ok.go", true},
		{"windows path", asst(use("Edit", `{"file_path":"C:\\Users\\dev\\src\\Note.java"}`)), "Edit", "Note.java", true},
		{"command cut", asst(use("Bash", `{"command":"`+long+`\nsecond"}`)), "Bash", strings.Repeat("x", 60), true},
		{"description", asst(use("Agent", `{"description":"revisar lote"}`)), "Agent", "revisar lote", true},
		{"no arg", asst(use("TodoWrite", `{}`)), "TodoWrite", "", true},
		{"no tool", asst(`{"type":"text","text":"hi"}`), "", "", false},
		{"empty", "", "", "", false},
	}
	for _, c := range cases {
		name, arg, ok := LastToolUse([]byte(c.data))
		if name != c.wantName || arg != c.wantArg || ok != c.wantOK {
			t.Errorf("%s: got (%q, %q, %v), want (%q, %q, %v)", c.name, name, arg, ok, c.wantName, c.wantArg, c.wantOK)
		}
	}
}

func TestFirstUserText(t *testing.T) {
	data := `{"type":"system"}` + "\n" +
		`{"type":"user","message":{"content":[{"type":"text","text":"feature X batch 2"},{"type":"text","text":"K1"}]}}` + "\n" +
		`{"type":"user","message":{"content":"second"}}` + "\n"
	if got := FirstUserText([]byte(data)); got != "feature X batch 2\nK1" {
		t.Fatalf("blocks: %q", got)
	}
	if got := FirstUserText([]byte(`{"type":"user","message":{"content":"plain"}}`)); got != "plain" {
		t.Fatalf("string: %q", got)
	}
	if got := FirstUserText([]byte(`{"type":"assistant"}`)); got != "" {
		t.Fatalf("none: %q", got)
	}
}

func TestLastAssistantText(t *testing.T) {
	data := asst(`{"type":"text","text":"first"}`) +
		asst(`{"type":"text","text":"final report"}`) +
		asst(use("Read", `{"file_path":"/a"}`)) +
		`{"type":"assistant","message":{"content":[{"type":"text","text":"cut`
	if got := LastAssistantText([]byte(data)); got != "final report" {
		t.Fatalf("got %q", got)
	}
	if got := LastAssistantText(nil); got != "" {
		t.Fatalf("empty: %q", got)
	}
}
