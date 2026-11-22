//go:build windows

package platform

import (
	"golang.org/x/sys/windows/registry"
)

func readRegPath(root registry.Key, sub string) string {
	k, err := registry.OpenKey(root, sub, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Path")
	if err != nil {
		return ""
	}
	return v
}

// registryPaths reads the Path of HKCU\Environment and of the system Environment.
func registryPaths() (string, string) {
	return readRegPath(registry.CURRENT_USER, `Environment`),
		readRegPath(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`)
}

// expandEnv expands %VAR% as Windows does; it keeps the text when it cannot.
func expandEnv(s string) string {
	out, err := registry.ExpandString(s)
	if err != nil {
		return s
	}
	return out
}
