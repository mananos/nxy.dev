package platform

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ShellArgv is how to run a shell command line on the given GOOS: sh -c (then
// bash) when found; on Windows without one, cmd.exe with a raw command line
// (cmdLine, to be set verbatim; it supports && where PowerShell 5.1 does not).
// The WSL launcher System32\bash.exe is not a usable shell and is skipped.
func ShellArgv(goos string, lookPath func(string) (string, error), command string) (argv []string, cmdLine string) {
	for _, name := range []string{"sh", "bash"} {
		p, err := lookPath(name)
		if err != nil || p == "" {
			continue
		}
		if goos == "windows" && strings.HasSuffix(strings.ToLower(strings.ReplaceAll(p, "/", `\`)), `system32\bash.exe`) {
			continue
		}
		return []string{p, "-c", command}, ""
	}
	if goos == "windows" {
		return []string{"cmd.exe"}, `cmd.exe /d /s /c "` + command + `"`
	}
	return []string{"sh", "-c", command}, ""
}

// RunShell runs command in dir through the system shell and returns its exit
// code and the last keep bytes of stdout+stderr combined. A non-zero exit is not
// an error: err is only for a shell that cannot start or an expired ctx.
func RunShell(ctx context.Context, dir, command string, keep int) (exit int, out string, err error) {
	argv, cmdLine := ShellArgv(runtime.GOOS, exec.LookPath, command)
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Dir = dir
	c.WaitDelay = 3 * time.Second // a grandchild holding the pipe must not hang Wait
	hideWindow(c)
	if cmdLine != "" {
		setCmdLine(c, cmdLine)
	}
	killTree(c)
	w := &tailWriter{keep: keep}
	c.Stdout, c.Stderr = w, w
	runErr := c.Run()
	out = w.String()
	if ctx.Err() != nil {
		return -1, out, ctx.Err()
	}
	var ee *exec.ExitError
	switch {
	case runErr == nil, errors.Is(runErr, exec.ErrWaitDelay) && c.ProcessState != nil:
		return c.ProcessState.ExitCode(), out, nil
	case errors.As(runErr, &ee):
		return ee.ExitCode(), out, nil
	}
	return -1, out, runErr
}

// tailWriter keeps only the last keep bytes written to it.
type tailWriter struct {
	keep int
	buf  []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	if w.keep >= 0 && len(w.buf) > w.keep {
		w.buf = append(w.buf[:0], w.buf[len(w.buf)-w.keep:]...)
	}
	return len(p), nil
}

func (w *tailWriter) String() string { return string(w.buf) }
