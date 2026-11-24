package main

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/version"
)

// TestVersionsAgree: todos los archivos que llevan la versión dicen lo mismo.
func TestVersionsAgree(t *testing.T) {
	want := version.Version

	cmd, err := os.ReadFile("../../bin/nxy.cmd")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^(?:set ")?NXY_VERSION=([^"\r\n]*)"?\s*$`)
	ms := re.FindAllStringSubmatch(string(cmd), -1)
	if len(ms) != 2 {
		t.Fatalf("bin/nxy.cmd: se esperaban 2 líneas NXY_VERSION y hay %d", len(ms))
	}
	for i, m := range ms {
		if m[1] != want {
			t.Errorf("bin/nxy.cmd (NXY_VERSION n.º %d) dice %q y internal/version/version.go dice %q", i+1, m[1], want)
		}
	}

	plug, err := os.ReadFile("../../.claude-plugin/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(plug, &p); err != nil {
		t.Fatal(err)
	}
	if p.Version != want {
		t.Errorf(".claude-plugin/plugin.json dice %q y internal/version/version.go dice %q", p.Version, want)
	}

	mk, err := os.ReadFile("../../.claude-plugin/marketplace.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Plugins []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(mk, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Plugins) == 0 {
		t.Error(".claude-plugin/marketplace.json no tiene plugins")
	}
	for _, pl := range m.Plugins {
		if pl.Version != want {
			t.Errorf(".claude-plugin/marketplace.json (plugin %s) dice %q y internal/version/version.go dice %q", pl.Name, pl.Version, want)
		}
	}
}
