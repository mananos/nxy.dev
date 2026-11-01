package platform

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var msysPath = regexp.MustCompile(`^/([A-Za-z])(/.*)?$`)

// NativePath turns a Git Bash style path (`/c/Users/x`) into `C:/Users/x` on Windows.
// Any other path, and every path on other systems, is returned unchanged.
func NativePath(p string) string { return nativePath(runtime.GOOS, p) }

func nativePath(goos, p string) string {
	if goos != "windows" {
		return p
	}
	if rest, ok := strings.CutPrefix(p, `\\?\UNC\`); ok {
		return `\\` + rest
	}
	if rest, ok := strings.CutPrefix(p, `\\?\`); ok {
		return rest
	}
	m := msysPath.FindStringSubmatch(p)
	if m == nil {
		return p
	}
	rest := m[2]
	if rest == "" {
		rest = "/"
	}
	return strings.ToUpper(m[1]) + ":" + rest
}

// UserDir is the user-level nxy directory: NXY_HOME when set, else ~/.nxy.
func UserDir() (string, error) { return UserDirEnv(os.Getenv) }

// UserDirEnv is UserDir reading NXY_HOME through getenv (the home still comes from the OS).
func UserDirEnv(getenv func(string) string) (string, error) {
	if h := getenv("NXY_HOME"); h != "" {
		return NativePath(h), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".nxy"), nil
}

// RuntimeDir is the per-checkout runtime state: <git root or dir>/.nxy/local (paths.mjs nxyRuntimeDir).
func RuntimeDir(dir string) string {
	root := dir
	if r, ok := GitRoot(dir); ok {
		root = r
	}
	return filepath.Join(root, ".nxy", "local")
}

// BinDir is where the binary of a given version lives.
func BinDir(version string) (string, error) {
	u, err := UserDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(u, "bin", version), nil
}

// DBPath is the SQLite database file.
func DBPath() (string, error) {
	u, err := UserDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(u, "nxy.db"), nil
}
