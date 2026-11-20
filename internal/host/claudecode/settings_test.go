package claudecode

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsFiles(t *testing.T) {
	got := SettingsFiles("H", "P")
	want := []string{
		filepath.Join("H", ".claude", "settings.json"),
		filepath.Join("P", ".claude", "settings.json"),
		filepath.Join("P", ".claude", "settings.local.json"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestLoadPermissionRules(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.json")
	b := filepath.Join(dir, "b.json")
	c := filepath.Join(dir, "broken.json")
	write(t, a, `{"permissions":{"allow":["Bash(git status)","Read"],"deny":["Bash(rm:*)"]}}`)
	write(t, b, `{"permissions":{"allow":["Bash"]}}`)
	write(t, c, `{oops`)
	got := LoadPermissionRules([]string{a, filepath.Join(dir, "missing.json"), c, b})
	if !reflect.DeepEqual(got.Allow, []string{"Bash(git status)", "Read", "Bash"}) || !reflect.DeepEqual(got.Deny, []string{"Bash(rm:*)"}) {
		t.Fatalf("rules: %+v", got)
	}
}

func TestPreToolUseHookCommands(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.json")
	write(t, a, `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"rtk hook claude"},{"type":"command"}]},{"hooks":[{"command":"node x.mjs"}]}],"PostToolUse":[{"hooks":[{"command":"other"}]}]}}`)
	got := PreToolUseHookCommands([]string{a, filepath.Join(dir, "none.json")})
	if !reflect.DeepEqual(got, []string{"rtk hook claude", "node x.mjs"}) {
		t.Fatalf("commands: %v", got)
	}
}
