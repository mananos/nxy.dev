package tui

import (
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// clipDoneMsg is the end of a copy through an external command.
type clipDoneMsg struct {
	name string
	err  error
}

// clipTool is the command that copies on this system; empty name: none (use OSC 52).
func clipTool(goos string, getenv func(string) string, lookPath func(string) (string, error)) (string, []string) {
	has := func(n string) bool { _, err := lookPath(n); return err == nil }
	switch {
	case goos == "windows":
		if has("clip.exe") {
			return "clip.exe", nil
		}
	case getenv("WAYLAND_DISPLAY") != "" && has("wl-copy"):
		return "wl-copy", nil
	case getenv("DISPLAY") != "" && has("xclip"):
		return "xclip", []string{"-selection", "clipboard"}
	}
	return "", nil
}

// copyText returns the command that copies s and the toast to show (it always names how).
func (st *state) copyText(s string) (tea.Cmd, string) {
	name, args := clipTool(st.goos, st.getenv, st.lookPath)
	if name == "" {
		return tea.SetClipboard(s), "mandado a la terminal (OSC 52); si no se pegó, seleccioná: " + s
	}
	cmd := func() tea.Msg {
		c := exec.Command(name, args...)
		c.Stdin = strings.NewReader(s)
		return clipDoneMsg{name: name, err: c.Run()}
	}
	return cmd, "copiado con " + strings.Join(append([]string{name}, args...), " ") + " · " + s
}
