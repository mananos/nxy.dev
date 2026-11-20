package transcript

import (
	"bytes"
	"encoding/json"
)

// decodeLine decodes one line; a malformed line is skipped by the callers. A trailing CR is
// whitespace for the decoder, as for JSON.parse.
func decodeLine(line []byte) (any, bool) {
	var e any
	if err := json.Unmarshal(line, &e); err != nil {
		return nil, false
	}
	return e, true
}

// Events is every parseable line of a transcript, in file order (readEvents). Malformed lines
// are skipped.
func Events(data []byte) []any {
	out := []any{}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		if e, ok := decodeLine(line); ok {
			out = append(out, e)
		}
	}
	return out
}
