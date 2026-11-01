package transcript

import (
	"bytes"

	"github.com/mananos/nxy.dev/internal/jscompat"
	"github.com/mananos/nxy.dev/internal/pricing"
)

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
