// Package nxydev embeds the polyglot shim (bin/nxy.cmd), so the binary can keep the stable copy
// the statusline runs (~/.nxy/bin/nxy.cmd) equal to the shim of its own version.
package nxydev

import _ "embed"

// Shim is bin/nxy.cmd as built.
//
//go:embed bin/nxy.cmd
var Shim []byte
