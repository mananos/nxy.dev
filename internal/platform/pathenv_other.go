//go:build !windows

package platform

import "os"

func registryPaths() (string, string) { return "", "" }

func expandEnv(s string) string { return os.ExpandEnv(s) }
