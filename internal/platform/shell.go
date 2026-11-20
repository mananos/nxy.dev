package platform

import (
	"context"
	"errors"
	"io"
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
	w := &tailWriter{keep: keep}
	exit, err = runProc(ctx, dir, argv, cmdLine, "", w, w)
	return exit, w.String(), err
}

// RunArgvSplit runs argv directly (no shell) in dir, feeding stdin when not empty,
// with stdout kept apart from stderr (for output that is parsed, such as JSON).
// Same error contract as RunShell. Returns the whole stdout, capped at keep bytes from its tail, and the stderr tail.
func RunArgvSplit(ctx context.Context, dir string, argv []string, stdin string, keep int) (exit int, stdout, stderr string, err error) {
	o, e := &tailWriter{keep: keep}, &tailWriter{keep: 4096}
	exit, err = runProc(ctx, dir, argv, "", stdin, o, e)
	return exit, o.String(), e.String(), err
}

// runProc is the exec core: hidden window, process tree killed on ctx expiry, and
// a WaitDelay so a grandchild holding the pipe cannot hang Wait.
func runProc(ctx context.Context, dir string, argv []string, cmdLine, stdin string, stdout, stderr io.Writer) (int, error) {
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Dir = dir
	c.WaitDelay = 3 * time.Second
	hideWindow(c)
	if cmdLine != "" {
		setCmdLine(c, cmdLine)
	}
	killTree(c)
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	c.Stdout, c.Stderr = stdout, stderr
	runErr := c.Run()
	if ctx.Err() != nil {
		return -1, ctx.Err()
	}
	var ee *exec.ExitError
	switch {
	case runErr == nil, errors.Is(runErr, exec.ErrWaitDelay) && c.ProcessState != nil:
		return c.ProcessState.ExitCode(), nil
	case errors.As(runErr, &ee):
		return ee.ExitCode(), nil
	}
	return -1, runErr
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
