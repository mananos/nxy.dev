package platform

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestShellArgv(t *testing.T) {
	found := func(m map[string]string) func(string) (string, error) {
		return func(n string) (string, error) {
			if p, ok := m[n]; ok {
				return p, nil
			}
			return "", errors.New("not found")
		}
	}
	cases := []struct {
		name    string
		goos    string
		paths   map[string]string
		argv    []string
		cmdLine string
	}{
		{"linux sh", "linux", map[string]string{"sh": "/bin/sh"}, []string{"/bin/sh", "-c", "a && b"}, ""},
		{"windows sh", "windows", map[string]string{"sh": `C:\Git\usr\bin\sh.exe`}, []string{`C:\Git\usr\bin\sh.exe`, "-c", "a && b"}, ""},
		{"windows bash after no sh", "windows", map[string]string{"bash": `C:\Git\bin\bash.exe`}, []string{`C:\Git\bin\bash.exe`, "-c", "a && b"}, ""},
		{"windows only System32 bash", "windows", map[string]string{"bash": `C:\Windows\System32\bash.exe`}, []string{"cmd.exe"}, `cmd.exe /d /s /c "a && b"`},
		{"windows nothing", "windows", nil, []string{"cmd.exe"}, `cmd.exe /d /s /c "a && b"`},
		{"linux nothing", "linux", nil, []string{"sh", "-c", "a && b"}, ""},
	}
	for _, c := range cases {
		argv, line := ShellArgv(c.goos, found(c.paths), "a && b")
		if !reflect.DeepEqual(argv, c.argv) || line != c.cmdLine {
			t.Errorf("%s: got %q %q, want %q %q", c.name, argv, line, c.argv, c.cmdLine)
		}
	}
}

// testShell uses the same shell as RunShell; it reports whether that is cmd.exe.
func testShell(t *testing.T) (cmd bool) {
	t.Helper()
	argv, _ := ShellArgv(runtime.GOOS, exec.LookPath, "")
	if argv[0] == "cmd.exe" {
		return true
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		t.Skip("no sh")
	}
	return false
}

func TestRunShellExit(t *testing.T) {
	testShell(t)
	exit, _, err := RunShell(context.Background(), t.TempDir(), "exit 3", 1024)
	if err != nil || exit != 3 {
		t.Fatalf("exit=%d err=%v", exit, err)
	}
}

func TestRunShellAnd(t *testing.T) {
	testShell(t)
	exit, out, err := RunShell(context.Background(), t.TempDir(), "echo a && echo b", 1024)
	if err != nil || exit != 0 || !strings.Contains(out, "a") || !strings.Contains(out, "b") {
		t.Fatalf("exit=%d out=%q err=%v", exit, out, err)
	}
}

func TestRunShellTimeoutKillsAndReturns(t *testing.T) {
	command := "echo start; sleep 30; echo late"
	if testShell(t) {
		command = "echo start && ping -n 30 127.0.0.1 >nul && echo late"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	exit, out, err := RunShell(ctx, t.TempDir(), command, 1024)
	if !errors.Is(err, context.DeadlineExceeded) || exit != -1 {
		t.Fatalf("exit=%d err=%v", exit, err)
	}
	if time.Since(start) > 15*time.Second || strings.Contains(out, "late") {
		t.Fatalf("not killed: %v out=%q", time.Since(start), out)
	}
}

func TestRunShellKeepsTail(t *testing.T) {
	testShell(t)
	_, out, err := RunShell(context.Background(), t.TempDir(), "echo aaaaaaaaaa && echo zzzz", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 8 || !strings.Contains(out, "zzzz") || strings.Contains(out, "aaaa") {
		t.Fatalf("out=%q", out)
	}
}
