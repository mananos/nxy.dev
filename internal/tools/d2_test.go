package tools

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestD2CandidatesProgramFiles(t *testing.T) {
	pf := filepath.Join(t.TempDir(), "Program Files")
	getenv := func(k string) string {
		if k == "ProgramFiles" {
			return pf
		}
		return ""
	}
	want := filepath.Join(pf, "D2", "d2.exe")
	found := false
	for _, c := range d2Candidates("windows", getenv) {
		found = found || c == want
	}
	if !found {
		t.Errorf("windows candidates miss %s: %v", want, d2Candidates("windows", getenv))
	}
	for _, c := range d2Candidates("linux", getenv) {
		if c == want {
			t.Errorf("linux must not include Program Files")
		}
	}
}

func TestKnownD2Locations(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, "AppData", "Local")
	pkg := filepath.Join(local, "Microsoft", "WinGet", "Packages", "Terrastruct.D2_Microsoft.Winget.Source_x")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	getenv := func(string) string { return "" }

	win := KnownD2Locations("windows", home, getenv)
	wantWin := []string{
		filepath.Join(local, "Microsoft", "WinGet", "Links", "d2.exe"),
		filepath.Join(pkg, "d2.exe"),
		filepath.Join(home, ".local", "bin", "d2.exe"),
		filepath.Join(home, "go", "bin", "d2.exe"),
	}
	if !reflect.DeepEqual(win, wantWin) {
		t.Errorf("windows: %v", win)
	}
	for _, goos := range []string{"linux", "darwin"} {
		got := KnownD2Locations(goos, home, getenv)
		want := []string{
			filepath.Join(home, ".local", "bin", "d2"), filepath.Join(home, "go", "bin", "d2"),
			"/usr/local/bin/d2", "/opt/homebrew/bin/d2",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v", goos, got)
		}
	}
}

// fakeD2 writes a script that prints body (shell syntax on Unix, batch on Windows) and exits with code.
func fakeD2(t *testing.T, unix, win string) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		p := filepath.Join(dir, "d2.cmd")
		if err := os.WriteFile(p, []byte("@echo off\r\n"+win), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := filepath.Join(dir, "d2")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+unix), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRenderD2(t *testing.T) {
	bin := fakeD2t(t)
	lines, err := RenderD2(context.Background(), bin, "a -> b")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lines, []string{"+---+", "| a |", "+---+"}) {
		t.Errorf("lines %q", lines)
	}

	fail := fakeD2(t, "echo boom >&2\nexit 3\n", "echo boom 1>&2\r\nexit /b 3\r\n")
	if _, err := RenderD2(context.Background(), fail, "x"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("a non-zero exit must be an error with stderr: %v", err)
	}
}

func fakeD2t(t *testing.T) string {
	return fakeD2(t,
		"echo '+---+   '\necho '| a |'\necho '+---+  '\necho\n",
		"echo +---+   \r\necho ^| a ^|\r\necho +---+  \r\n")
}

func TestRenderD2Timeout(t *testing.T) {
	old := renderD2Timeout
	renderD2Timeout = 200 * time.Millisecond
	t.Cleanup(func() { renderD2Timeout = old })
	bin := fakeD2(t, "sleep 5\n", "ping -n 6 127.0.0.1 >nul\r\n")
	start := time.Now()
	if _, err := RenderD2(context.Background(), bin, "x"); err == nil {
		t.Error("a timeout must be an error")
	}
	if time.Since(start) > 4*time.Second {
		t.Errorf("did not stop at the timeout: %v", time.Since(start))
	}
}
