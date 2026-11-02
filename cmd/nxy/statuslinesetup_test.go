package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	nxydev "github.com/mananos/nxy.dev"
)

func testSetup(t *testing.T, goos string) statuslineSetup {
	t.Helper()
	root := t.TempDir()
	return statuslineSetup{home: filepath.Join(root, "home"), userDir: filepath.Join(root, "nxy"), goos: goos, now: func() time.Time { return time.UnixMilli(5) }}
}

func readSettings(t *testing.T, s statuslineSetup) map[string]any {
	t.Helper()
	b, err := os.ReadFile(s.settingsPath())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestStatuslineCommandPerOS(t *testing.T) {
	w := testSetup(t, "windows")
	if c := w.command(); !strings.HasPrefix(c, `. "`) || !strings.HasSuffix(c, `/bin/nxy.cmd" statusline; exit $LASTEXITCODE`) || strings.Contains(c, `\`) {
		t.Fatalf("windows command %q", c)
	}
	l := testSetup(t, "linux")
	if c := l.command(); !strings.HasPrefix(c, `sh "`) || !strings.HasSuffix(c, `/bin/nxy.cmd" statusline`) {
		t.Fatalf("linux command %q", c)
	}
}

func TestStatuslineInstallKeepsOtherKeysAndBacksUp(t *testing.T) {
	s := testSetup(t, "linux")
	os.MkdirAll(filepath.Dir(s.settingsPath()), 0o755)
	orig := "{\n  \"model\": \"opus\",\n  \"statusLine\": {\n    \"type\": \"command\",\n    \"command\": \"node old.mjs\"\n  },\n  \"z\": 1\n}\n"
	os.WriteFile(s.settingsPath(), []byte(orig), 0o644)
	var out bytes.Buffer
	if code := s.install(&out); code != 0 {
		t.Fatalf("code=%d out=%s", code, out.String())
	}
	got, _ := os.ReadFile(s.settingsPath())
	want := "{\n  \"model\": \"opus\",\n  \"statusLine\": {\n    \"type\": \"command\",\n    \"command\": " + oneLineJSON(s.command()) + ",\n    \"padding\": 0,\n    \"refreshInterval\": 30\n  },\n  \"z\": 1\n}\n"
	if string(got) != want {
		t.Fatalf("settings\n%s\nwant\n%s", got, want)
	}
	shim, err := os.ReadFile(stableShimPath(s.userDir))
	if err != nil || !bytes.Equal(shim, nxydev.Shim) {
		t.Fatalf("stable shim not written: %v", err)
	}
	backups, _ := filepath.Glob(s.settingsPath() + ".bak-nxy-*")
	if len(backups) != 1 {
		t.Fatalf("backups = %v", backups)
	}
	if b, _ := os.ReadFile(backups[0]); string(b) != orig {
		t.Fatalf("backup content %q", b)
	}
	out.Reset()
	s.show(&out)
	if !strings.Contains(out.String(), "Ya está instalada") {
		t.Fatalf("show after install: %s", out.String())
	}
}

func TestStatuslineInstallWithoutSettings(t *testing.T) {
	s := testSetup(t, "windows")
	var out bytes.Buffer
	if code := s.install(&out); code != 0 {
		t.Fatalf("code=%d out=%s", code, out.String())
	}
	if !strings.Contains(out.String(), "backup: ninguno") {
		t.Fatalf("out: %s", out.String())
	}
	sl, _ := readSettings(t, s)["statusLine"].(map[string]any)
	if sl["command"] != s.command() {
		t.Fatalf("statusLine %v", sl)
	}
}

func TestStatuslineBrokenSettingsUntouched(t *testing.T) {
	s := testSetup(t, "linux")
	os.MkdirAll(filepath.Dir(s.settingsPath()), 0o755)
	os.WriteFile(s.settingsPath(), []byte("{broken"), 0o644)
	var out bytes.Buffer
	if code := s.install(&out); code != 1 {
		t.Fatalf("code=%d", code)
	}
	if b, _ := os.ReadFile(s.settingsPath()); string(b) != "{broken" {
		t.Fatalf("broken settings rewritten: %q", b)
	}
	if _, err := os.Stat(stableShimPath(s.userDir)); err == nil {
		t.Fatal("shim written although settings are broken")
	}
}

func TestStatuslineRemove(t *testing.T) {
	s := testSetup(t, "linux")
	var out bytes.Buffer
	if code := s.remove(&out); code != 1 {
		t.Fatalf("remove without settings: code=%d", code)
	}
	os.MkdirAll(filepath.Dir(s.settingsPath()), 0o755)
	os.WriteFile(s.settingsPath(), []byte(`{"statusLine":{"type":"command","command":"node mine.mjs"}}`), 0o644)
	out.Reset()
	if code := s.remove(&out); code != 1 || !strings.Contains(out.String(), "no es la de nxy") {
		t.Fatalf("foreign statusline: code=%d out=%s", code, out.String())
	}
	out.Reset()
	if code := s.install(&out); code != 0 {
		t.Fatal(out.String())
	}
	out.Reset()
	if code := s.remove(&out); code != 0 {
		t.Fatalf("code=%d out=%s", code, out.String())
	}
	if _, ok := readSettings(t, s)["statusLine"]; ok {
		t.Fatal("statusLine still there")
	}
	if _, err := os.Stat(stableShimPath(s.userDir)); err == nil {
		t.Fatal("stable shim not removed")
	}
}

func TestRefreshStableShim(t *testing.T) {
	dir := t.TempDir()
	refreshStableShim(dir) // no statusline installed: nothing written
	if _, err := os.Stat(stableShimPath(dir)); err == nil {
		t.Fatal("shim created without an install")
	}
	os.MkdirAll(filepath.Dir(stableShimPath(dir)), 0o755)
	os.WriteFile(stableShimPath(dir), []byte("old shim"), 0o644)
	refreshStableShim(dir)
	if b, _ := os.ReadFile(stableShimPath(dir)); !bytes.Equal(b, nxydev.Shim) {
		t.Fatal("old shim not refreshed")
	}
}

// The embedded shim is the file in bin/: the statusline copy is the shim of this version.
func TestEmbeddedShimIsBinShim(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "bin", "nxy.cmd"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, nxydev.Shim) {
		t.Fatal("embedded shim differs from bin/nxy.cmd")
	}
}
