package platform

import (
	"path/filepath"
	"runtime"
	"strings"
)

// MergePath joins the PATH of the process with the user and machine Path (in that order),
// expanding %VAR% with expand (nil = as is) and dropping duplicates. On Windows duplicates ignore
// case and a trailing slash.
func MergePath(process, user, machine string, expand func(string) string, win bool) string {
	seen := map[string]bool{}
	var out []string
	add := func(list string, exp bool) {
		for _, d := range filepath.SplitList(list) {
			if exp && expand != nil {
				d = expand(d)
			}
			if d == "" {
				continue
			}
			k := d
			if win {
				k = strings.ToLower(strings.TrimRight(strings.ReplaceAll(d, "/", "\\"), "\\"))
			}
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, d)
		}
	}
	add(process, false)
	add(user, true)
	add(machine, true)
	return strings.Join(out, string(filepath.ListSeparator))
}

// FreshPath is the PATH of the process plus, on Windows, the user and system Path of the registry,
// so a Path changed after the process started is seen. Elsewhere it returns process as is.
func FreshPath(process string) string {
	if runtime.GOOS != "windows" {
		return process
	}
	user, machine := registryPaths()
	return MergePath(process, user, machine, expandEnv, true)
}
