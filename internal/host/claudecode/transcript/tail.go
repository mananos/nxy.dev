package transcript

import (
	"bytes"

	"github.com/mananos/nxy.dev/v2/internal/jscompat"
	"github.com/mananos/nxy.dev/v2/internal/pricing"
)

// LastContextTokens is the context the main thread carries: input + cache read + cache creation of
// the most recent assistant message that reported usage (lastContextTokens of the 1.x
// hosts/claude-code/context.mjs). ok=false when the tail has none: the caller fails open.
// truncated says tail starts mid-file, so its partial first line is dropped.
func LastContextTokens(tail []byte, truncated bool) (tokens float64, ok bool) {
	if truncated {
		nl := bytes.IndexByte(tail, '\n')
		if nl == -1 {
			return 0, false
		}
		tail = tail[nl+1:]
	}
	num := func(v any) float64 {
		if f, isNum := v.(float64); isNum {
			return f
		}
		return 0
	}
	lines := bytes.Split(tail, []byte{'\n'})
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		e, ok := decodeLine(line)
		if !ok {
			continue
		}
		if jscompat.Get(e, "type") != "assistant" {
			continue
		}
		u := jscompat.Get(e, "message", "usage")
		if !jscompat.Truthy(u) {
			continue
		}
		total := num(jscompat.Get(u, "input_tokens")) + num(jscompat.Get(u, "cache_read_input_tokens")) + num(jscompat.Get(u, "cache_creation_input_tokens"))
		if total > 0 {
			return total, true
		}
	}
	return 0, false
}

// LastCacheTTL is the cache TTL the main thread writes with, read from the end of its transcript:
// the last response (not a sidechain) that wrote cache decides, "1h" if it wrote any 1h tokens,
// "5m" if only 5m tokens; ok=false (the 1.x null) when the tail has no cache write. truncated says
// tail starts mid-file, so its partial first line is dropped.
func LastCacheTTL(tail []byte, truncated bool) (ttl string, ok bool) {
	if truncated {
		nl := bytes.IndexByte(tail, '\n')
		if nl == -1 {
			return "", false
		}
		tail = tail[nl+1:]
	}
	lines := bytes.Split(tail, []byte{'\n'})
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if len(line) == 0 || !bytes.Contains(line, []byte(`"usage"`)) {
			continue
		}
		e, ok := decodeLine(line)
		if !ok {
			continue
		}
		if jscompat.Get(e, "type") != "assistant" || jscompat.Truthy(jscompat.Get(e, "isSidechain")) {
			continue
		}
		raw := jscompat.Get(e, "message", "usage")
		if !jscompat.Truthy(raw) {
			continue
		}
		u := pricing.ToUsage(raw)
		if u.CacheWrite1h > 0 {
			return "1h", true
		}
		if u.CacheWrite5m > 0 {
			return "5m", true
		}
	}
	return "", false
}
