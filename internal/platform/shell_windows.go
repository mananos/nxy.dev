//go:build windows

package platform

import (
	"os/exec"
	"strconv"
	"syscall"
)

// setCmdLine makes Windows run line verbatim, with no argv quoting.
func setCmdLine(c *exec.Cmd, line string) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CmdLine: line}
}

// killTree kills the shell and its children with `taskkill /T /F` when the context
// expires (no job object: taskkill ships with Windows and is enough here).
func killTree(c *exec.Cmd) {
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		k := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(c.Process.Pid))
		hideWindow(k)
		_ = k.Run()
		return c.Process.Kill()
	}
}
