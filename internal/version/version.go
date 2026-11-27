// Package version holds the build version. GoReleaser overrides Version
// with -ldflags -X.
package version

// Version is the nxy version; the shim pins the same string.
var Version = "2.3.0"
