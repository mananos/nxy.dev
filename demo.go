package nxydev

import "embed"

// DemoViews holds the sample data of `nxy tui --demo`: the JSON files of the views contract.
// Embedded so the demo runs from any folder and the dump is deterministic.
//
//go:embed testdata/views/*.json
var DemoViews embed.FS
