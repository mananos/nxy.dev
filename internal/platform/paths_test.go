package platform

import (
	"path/filepath"
	"testing"
)

func TestNativePath(t *testing.T) {
	cases := []struct{ goos, in, want string }{
		{"windows", "/c/Users/x", "C:/Users/x"},
		{"windows", "/d", "D:/"},
		{"windows", "/d/", "D:/"},
		{"windows", "C:/Users/x", "C:/Users/x"},
		{"windows", "/usr/bin", "/usr/bin"},
		{"windows", "", ""},
		{"windows", `\\?\C:\Repo\a.go`, `C:\Repo\a.go`},
		{"windows", `\\?\UNC\srv\share\a.go`, `\\srv\share\a.go`},
		{"windows", `\\srv\share\a.go`, `\\srv\share\a.go`},
		{"linux", `\\?\C:\Repo`, `\\?\C:\Repo`},
		{"linux", "/c/Users/x", "/c/Users/x"},
		{"linux", "/home/x", "/home/x"},
	}
	for _, c := range cases {
		if got := nativePath(c.goos, c.in); got != c.want {
			t.Errorf("nativePath(%q,%q) = %q, want %q", c.goos, c.in, got, c.want)
		}
	}
}

func TestUserDirPaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NXY_HOME", dir)
	u, err := UserDir()
	if err != nil || u != NativePath(dir) {
		t.Fatalf("UserDir = %q, %v", u, err)
	}
	b, _ := BinDir("1.2.3")
	if b != filepath.Join(u, "bin", "1.2.3") {
		t.Errorf("BinDir = %q", b)
	}
	d, _ := DBPath()
	if d != filepath.Join(u, "nxy.db") {
		t.Errorf("DBPath = %q", d)
	}
}
