package gate

import (
	"strings"
	"testing"
)

func TestWriteGateMessageWording(t *testing.T) {
	cases := []struct{ tool, want string }{
		{"Bash", "does not write code"},
		{"PowerShell", "does not write code"},
		{"Edit", "does not Edit code"},
		{"", "does not edit code"},
	}
	for _, c := range cases {
		got := WriteGateMessage(c.tool, "a.go")
		if !strings.Contains(got, c.want) {
			t.Errorf("tool %q: %q missing %q", c.tool, got, c.want)
		}
	}
}
