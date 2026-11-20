package claudecode

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/app"
)

func TestEncodeAllowReason(t *testing.T) {
	var buf bytes.Buffer
	ev := app.HookEvent{Name: "PreToolUse", Tool: "Bash"}
	if err := Encode(&buf, ev, app.HookResponse{AllowReason: "nxy: its own step for feature f1"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{`"permissionDecision":"allow"`, `"hookEventName":"PreToolUse"`, `its own step for feature f1`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in %s", want, out)
		}
	}
	if strings.Contains(out, "updatedInput") {
		t.Errorf("allow must not rewrite: %s", out)
	}
}
