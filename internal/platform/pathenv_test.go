package platform

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMergePath(t *testing.T) {
	sep := string(filepath.ListSeparator)
	expand := func(s string) string { return strings.ReplaceAll(s, "%X%", "xdir") }
	got := MergePath("a"+sep+"b", "c"+sep+"%X%"+sep+"a", "d"+sep+"c", expand, false)
	want := strings.Join([]string{"a", "b", "c", "xdir", "d"}, sep)
	if got != want {
		t.Fatalf("MergePath = %q, want %q", got, want)
	}
}

func TestMergePathWindowsCase(t *testing.T) {
	sep := string(filepath.ListSeparator)
	got := MergePath("C:\\Bin\\", "c:\\bin"+sep+"C:\\User", "", nil, true)
	want := "C:\\Bin\\" + sep + "C:\\User"
	if got != want {
		t.Fatalf("MergePath = %q, want %q", got, want)
	}
}

func TestFreshPathKeepsProcess(t *testing.T) {
	p := "x" + string(filepath.ListSeparator) + "y"
	if got := FreshPath(p); !strings.HasPrefix(got, p) {
		t.Fatalf("FreshPath dropped the process PATH: %q", got)
	}
}
