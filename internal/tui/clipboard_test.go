package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func lookIn(names ...string) func(string) (string, error) {
	return func(n string) (string, error) {
		for _, x := range names {
			if x == n {
				return n, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestClipboardWindows(t *testing.T) {
	n, a := clipTool("windows", envOf(nil), lookIn("clip.exe"))
	if n != "clip.exe" || a != nil {
		t.Fatalf("got %q %v", n, a)
	}
	st := newState(Options{GOOS: "windows", Getenv: envOf(nil), LookPath: lookIn("clip.exe")})
	cmd, toast := st.copyText("nxy setup")
	if cmd == nil || !strings.Contains(toast, "clip.exe") || !strings.Contains(toast, "nxy setup") {
		t.Fatalf("toast %q", toast)
	}
}

func TestClipboardWayland(t *testing.T) {
	n, _ := clipTool("linux", envOf(map[string]string{"WAYLAND_DISPLAY": "w0", "DISPLAY": ":0"}), lookIn("wl-copy", "xclip"))
	if n != "wl-copy" {
		t.Fatalf("got %q", n)
	}
	n, a := clipTool("linux", envOf(map[string]string{"DISPLAY": ":0"}), lookIn("wl-copy", "xclip"))
	if n != "xclip" || !reflect.DeepEqual(a, []string{"-selection", "clipboard"}) {
		t.Fatalf("got %q %v", n, a)
	}
}

func TestClipboardOSC52(t *testing.T) {
	st := newState(Options{GOOS: "linux", Getenv: envOf(nil), LookPath: lookIn()})
	cmd, toast := st.copyText("curl x")
	if cmd == nil || !strings.Contains(toast, "OSC 52") || !strings.Contains(toast, "curl x") {
		t.Fatalf("toast %q", toast)
	}
}
