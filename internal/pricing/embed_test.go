package pricing

import "testing"

func TestDefaultKeepsFileOrder(t *testing.T) {
	d := Default()
	if len(d.Keys) == 0 || d.Keys[0] != "claude-fable-5-1" || len(d.Keys) != len(d.Models) {
		t.Fatalf("keys: %v", d.Keys)
	}
}
