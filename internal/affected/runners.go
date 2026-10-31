// Package affected holds the data and the pure rules to turn changed files
// into the test commands to run (F2.3): a table of runners (embedded plus the
// repo's .nxy/runners.json), a selection of tests by name and the grouping of
// tests into commands. It never touches the disk: the tree is a port.
package affected

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

//go:embed runners.json
var builtinJSON []byte

// Detect is one marker file of a stack; Contains (optional) must appear in it.
type Detect struct {
	File     string `json:"file"`
	Contains string `json:"contains,omitempty"`
}

// Runner is one row of the table.
type Runner struct {
	Name    string   `json:"name"`
	Detect  []Detect `json:"detect"`
	Tests   []string `json:"tests"` // globs over the file name
	Stems   []string `json:"stems"` // patterns with {name}, over the file name without extension
	SameDir bool     `json:"same_dir,omitempty"`
	Command string   `json:"command"` // contains {tests}
	Item    string   `json:"item"`    // path | dir | class
	Prefix  string   `json:"prefix,omitempty"`
	Sep     string   `json:"sep,omitempty"` // default one space
	// WrapperOf names the plain row this one is the wrapper of: when a test
	// matched that row, a wrapper found in an ancestor dir wins and runs from there.
	WrapperOf string `json:"wrapper_of,omitempty"`
	// Repo marks a row that came from the repo's .nxy/runners.json (set by Merge, never read).
	Repo bool `json:"-"`
}

// extOf reports whether ext is the extension of any test glob of r.
func extOf(r Runner, ext string) bool {
	for _, g := range r.Tests {
		if path.Ext(g) == ext {
			return true
		}
	}
	return false
}

// Parse decodes a table strictly: unknown fields and malformed rows fail.
func Parse(data []byte) ([]Runner, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var rows []Runner
	if err := dec.Decode(&rows); err != nil {
		return nil, err
	}
	for i, r := range rows {
		if r.Name == "" {
			return nil, fmt.Errorf("row %d: name is empty", i+1)
		}
		if len(r.Detect) == 0 {
			return nil, fmt.Errorf("%s: detect is empty", r.Name)
		}
		for _, d := range r.Detect {
			if d.File == "" {
				return nil, fmt.Errorf("%s: detect.file is empty", r.Name)
			}
		}
		if len(r.Tests) == 0 {
			return nil, fmt.Errorf("%s: tests is empty", r.Name)
		}
		for _, g := range r.Tests {
			if _, err := path.Match(g, "x"); err != nil {
				return nil, fmt.Errorf("%s: bad glob %q", r.Name, g)
			}
		}
		if !strings.Contains(r.Command, "{tests}") {
			return nil, fmt.Errorf("%s: command has no {tests}", r.Name)
		}
		switch r.Item {
		case "path", "dir", "class":
		default:
			return nil, fmt.Errorf("%s: item must be path, dir or class", r.Name)
		}
	}
	return rows, nil
}

// Builtin returns the embedded table.
func Builtin() []Runner {
	rows, err := Parse(builtinJSON)
	if err != nil {
		panic("affected: embedded runners.json: " + err.Error())
	}
	return rows
}

// Merge puts the repo rows first; a builtin row with the same name as a repo
// row is dropped (the repo row replaces it).
func Merge(repo, builtin []Runner) []Runner {
	names := map[string]bool{}
	out := make([]Runner, 0, len(repo)+len(builtin))
	for _, r := range repo {
		names[r.Name] = true
		r.Repo = true
		out = append(out, r)
	}
	for _, r := range builtin {
		if !names[r.Name] {
			out = append(out, r)
		}
	}
	return out
}

// isTestRow reports whether the file name of p matches a test glob of r.
func isTestRow(r Runner, p string) bool {
	base := path.Base(p)
	for _, g := range r.Tests {
		if ok, _ := path.Match(g, base); ok {
			return true
		}
	}
	return false
}

// IsTest reports whether p looks like a test file for any row.
func IsTest(rows []Runner, p string) bool {
	for _, r := range rows {
		if isTestRow(r, p) {
			return true
		}
	}
	return false
}
