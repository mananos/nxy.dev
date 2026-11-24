package tui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// editorDoneMsg is the return from the editor.
type editorDoneMsg struct{ err error }

// editorCmd builds the command that opens file at line: $VISUAL, else $EDITOR (split on spaces, so
// `code --wait` works), else code -g if it is in the PATH, else the opener of the system.
// suspend: it is a terminal editor and the TUI must give it the terminal; false: it runs apart.
// ok false: nothing to open with.
func editorCmd(goos string, getenv func(string) string, lookPath func(string) (string, error), file string, line int) (c *exec.Cmd, suspend bool, ok bool) {
	line = max(line, 1)
	code := func(name string, extra []string) *exec.Cmd {
		return exec.Command(name, append(extra, "-g", fmt.Sprintf("%s:%d", file, line))...)
	}
	spec := strings.TrimSpace(getenv("VISUAL"))
	if spec == "" {
		spec = strings.TrimSpace(getenv("EDITOR"))
	}
	if f := strings.Fields(spec); len(f) > 0 {
		base := strings.ToLower(filepath.Base(f[0]))
		base = strings.TrimSuffix(base, filepath.Ext(base))
		if base == "code" {
			return code(f[0], append([]string{}, f[1:]...)), true, true
		}
		return exec.Command(f[0], append(append([]string{}, f[1:]...), fmt.Sprintf("+%d", line), file)...), true, true
	}
	if _, err := lookPath("code"); err == nil {
		return code("code", nil), false, true
	}
	var argv []string
	switch goos {
	case "windows":
		argv = []string{"rundll32", "url.dll,FileProtocolHandler", file}
	case "darwin":
		argv = []string{"open", file}
	default:
		argv = []string{"xdg-open", file}
	}
	if goos != "windows" {
		if _, err := lookPath(argv[0]); err != nil {
			return nil, false, false
		}
	}
	return exec.Command(argv[0], argv[1:]...), false, true
}

// openEditor opens the file: a terminal editor takes the terminal, code and the opener run apart.
// Without any it says where the file is and how to set EDITOR.
func (st *state) openEditor(file string, line int) string {
	where := fmt.Sprintf("%s:%d", file, line)
	c, suspend, ok := editorCmd(st.goos, st.getenv, st.lookPath, file, line)
	if !ok {
		return where + " · definí EDITOR (por ejemplo export EDITOR=vim)"
	}
	if suspend {
		st.cmd = tea.Batch(st.cmd, tea.ExecProcess(c, func(err error) tea.Msg { return editorDoneMsg{err: err} }))
		return ""
	}
	prog := filepath.Base(c.Args[0])
	run := func() tea.Msg {
		return editorDoneMsg{err: c.Run()}
	}
	st.cmd = tea.Batch(st.cmd, run)
	return "abriendo " + filepath.Base(file) + " con " + prog
}

// absFile is the path on disk of a file of a feature: the path of its repo plus the file path.
func (st *state) absFile(feature, repo, path string) string {
	if st.data.Cockpit != nil {
		for _, f := range st.data.Cockpit.Features {
			if f.ID != feature {
				continue
			}
			for _, rp := range f.Repos {
				if rp.Name == repo || (repo == "" && len(f.Repos) == 1) {
					if rp.Path != "" {
						return filepath.Join(rp.Path, filepath.FromSlash(path))
					}
				}
			}
		}
	}
	return path
}
