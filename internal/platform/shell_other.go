//go:build !windows

package platform

import (
	"os/exec"
	"syscall"
)

func setCmdLine(*exec.Cmd, string) {}

// killTree puts the shell in its own process group and, when the context expires,
// kills the whole group so grandchildren do not outlive the Accept.
func killTree(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.Setpgid = true
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		if err := syscall.Kill(-c.Process.Pid, syscall.SIGKILL); err != nil {
			return c.Process.Kill()
		}
		return nil
	}
}
