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
func UserDir() (string, error) {
	if h := os.Getenv("NXY_HOME"); h != "" {
		return NativePath(h), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".nxy"), nil
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
