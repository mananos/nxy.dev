package platform

import (
	"errors"
	"os/exec"
	"runtime"
)

// OpenFile opens path with the system's default program and does not wait for it.
func OpenFile(path string) error {
	argv := openCommand(runtime.GOOS, path)
	if len(argv) == 0 || path == "" {
		return errors.New("nothing to open")
	}
	c := exec.Command(argv[0], argv[1:]...)
	hideWindow(c)
	if err := c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }() // reap the child; the opener usually exits at once
	return nil
}

// openCommand is the argv that opens path on the given GOOS.
func openCommand(goos, path string) []string {
	switch goos {
	case "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler", path}
	case "darwin":
		return []string{"open", path}
	default:
		return []string{"xdg-open", path}
	}
}
