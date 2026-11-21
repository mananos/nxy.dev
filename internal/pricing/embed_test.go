package pricing

import "testing"

func TestDefaultKeepsFileOrder(t *testing.T) {
	d := Default()
	if len(d.Keys) == 0 || d.Keys[0] != "claude-fable-5-2" || len(d.Keys) != len(d.Models) {
		t.Fatalf("keys: %v", d.Keys)
	}
}
