package platform

import (
	"bytes"
	"encoding/json"
	"os"

	"github.com/mananos/nxy.dev/internal/jscompat"
)

// ReadJSONL reads every parseable line of a JSONL file as decoded JSON (core/jsonl.mjs readJsonl).
// A missing file gives no rows; blank and unparsable lines are skipped.
func ReadJSONL(path string) []any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []any
	for _, line := range bytes.Split(data, []byte("\n")) {
		if jscompat.Trim(string(line)) == "" {
			continue
		}
		var v any
		if json.Unmarshal(line, &v) != nil {
			continue
		}
		out = append(out, v)
	}
	return out
}
