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

// editorCmd builds the command that opens file at line in the editor of the user: $VISUAL, else
// $EDITOR (split on spaces, so `code --wait` works). code takes -g file:N; the rest take +N file.
// false: no editor set.
func editorCmd(getenv func(string) string, file string, line int) (*exec.Cmd, bool) {
	spec := strings.TrimSpace(getenv("VISUAL"))
	if spec == "" {
		spec = strings.TrimSpace(getenv("EDITOR"))
	}
	f := strings.Fields(spec)
	if len(f) == 0 {
		return nil, false
	}
	line = max(line, 1)
	base := strings.ToLower(filepath.Base(f[0]))
	base = strings.TrimSuffix(base, filepath.Ext(base))
	args := append([]string{}, f[1:]...)
	if base == "code" {
		args = append(args, "-g", fmt.Sprintf("%s:%d", file, line))
	} else {
		args = append(args, fmt.Sprintf("+%d", line), file)
	}
	return exec.Command(f[0], args...), true
}

// openEditor suspends the TUI and opens the file; without an editor it only says where it is.
func (st *state) openEditor(file string, line int) string {
	where := fmt.Sprintf("%s:%d", file, line)
	c, ok := editorCmd(st.getenv, file, line)
	if !ok {
		return where
	}
	st.cmd = tea.Batch(st.cmd, tea.ExecProcess(c, func(err error) tea.Msg { return editorDoneMsg{err: err} }))
	return ""
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
