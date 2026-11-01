package platform

import (
	"fmt"
	"os"
)

// WriteAtomic writes data to a temp file named with the pid and renames it over path, so a reader
// never sees a half-written file (statusline.mjs:109). The directory must exist.
func WriteAtomic(path string, data []byte) error {
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
