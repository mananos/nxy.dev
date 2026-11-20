package views

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// Views lists the ids of the views of the contract (the screens live in internal/tui).
var Views = []string{"cockpit", "agents", "plan", "decision", "memory", "file", "stats", "trend", "config", "engineer"}

// New returns a pointer to the struct of a view, or nil when unknown.
func New(id string) any {
	switch id {
	case "cockpit":
		return &Cockpit{}
	case "agents":
		return &Agents{}
	case "plan":
		return &Plan{}
	case "decision":
		return &Decision{}
	case "memory":
		return &Memory{}
	case "file":
		return &File{}
	case "stats":
		return &Stats{}
	case "trend":
		return &Trend{}
	case "config":
		return &Config{}
	case "engineer":
		return &Engineer{}
	}
	return nil
}

// Decode reads exactly one JSON object into v, rejecting unknown fields and
// trailing data.
func Decode(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("views: unexpected data after the JSON object")
	}
	return nil
}
