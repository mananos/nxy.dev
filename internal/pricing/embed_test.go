package pricing

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mananos/nxy.dev/internal/goldentest"
)

// internal/pricing/pricing.json is a copy of core/pricing.json (go:embed cannot reach core/);
// until the JS is gone, drift between the two is a bug.
func TestEmbeddedCopyMatchesCore(t *testing.T) {
	core, err := os.ReadFile(filepath.Join(goldentest.Root(t), "core", "pricing.json"))
	if err != nil {
		t.Fatal(err)
	}
	lf := func(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")) }
	if !bytes.Equal(lf(core), lf(embedded)) {
		t.Fatal("internal/pricing/pricing.json differs from core/pricing.json (node scripts/golden/pricing.mjs)")
	}
}

func TestDefaultKeepsFileOrder(t *testing.T) {
	d := Default()
	if len(d.Keys) == 0 || d.Keys[0] != "claude-fable-5-1" || len(d.Keys) != len(d.Models) {
		t.Fatalf("keys: %v", d.Keys)
	}
}
