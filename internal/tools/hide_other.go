//go:build !windows

package tools

import "os/exec"

func hideWindow(*exec.Cmd) {}
