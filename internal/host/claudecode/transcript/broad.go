package transcript

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/mananos/nxy.dev/internal/jscompat"
	"github.com/mananos/nxy.dev/internal/searchscope"
)

// broadTools are the tools whose calls BroadSearches classifies.
var broadTools = map[string]bool{"Grep": true, "Glob": true, "Read": true, "Bash": true, "PowerShell": true}

// BroadSearch is one broad search of the main thread made at or above the gate threshold.
// Kind is the classifier's kind (a string or nil).
type BroadSearch struct {
	Ts            int64
	Tool          string
	Kind          any
	ContextTokens float64
}

// idKey is a Set key for a tool_use id. Objects and arrays never match in JS (distinct references),
// so they have no key.
func idKey(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return "s" + x, true
	case float64:
		return "n" + jscompat.String(x), true
	case bool:
		return "b" + strconv.FormatBool(x), true
	}
	return "", false
}

// tokens is `x || 0` for a usage field, counting non-numbers as 0.
func tokens(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

// BroadSearches is broadSearchesInTranscript of hosts/claude-code/search-history.mjs over the text of
// a main transcript: whole-file Read, Grep, Glob and shell searches made while the context was at or
// above threshold, minus the ones the read gate refused. A threshold that is not above zero gives
// nil. isFile is told the cwd of the line the call came from.
func BroadSearches(text string, threshold, sinceMs float64, isFile func(cwd, p string) bool) []BroadSearch {
	if !(threshold > 0) {
		return nil
	}
	var rows []BroadSearch
	seen := map[string]bool{}
	stopped := map[string]bool{}
	lines := strings.Split(text, "\n")
	if strings.Contains(text, searchscope.ReadGateMark) {
		for _, line := range lines {
			if !strings.Contains(line, searchscope.ReadGateMark) || !strings.Contains(line, "tool_result") {
				continue
			}
			var e any
			if json.Unmarshal([]byte(line), &e) != nil {
				continue
			}
			c, ok := jscompat.Get(e, "message", "content").([]any)
			if !ok {
				continue
			}
			for _, b := range c {
				if jscompat.Get(b, "type") != "tool_result" {
					continue
				}
				id := jscompat.Get(b, "tool_use_id")
				if !jscompat.Truthy(id) {
					continue
				}
				content := jscompat.Get(b, "content")
				if jscompat.IsUndefined(content) || content == nil {
					content = ""
				}
				raw, err := json.Marshal(content)
				if err != nil || !strings.Contains(string(raw), searchscope.ReadGateMark) {
					continue
				}
				if k, ok := idKey(id); ok {
					stopped[k] = true
				}
			}
		}
	}
	lastCtx := map[string]float64{}
	for _, line := range lines {
		if !strings.Contains(line, `"tool_use"`) {
			continue
		}
		var e any
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if jscompat.Get(e, "type") != "assistant" || jscompat.Truthy(jscompat.Get(e, "isSidechain")) {
			continue
		}
		ts, tsOK := jscompat.ParseDate(jscompat.Get(e, "timestamp"))
		if tsOK && float64(ts) < sinceMs {
			continue
		}
		ctx := 0.0
		if u := jscompat.Get(e, "message", "usage"); jscompat.Truthy(u) {
			ctx = tokens(jscompat.Get(u, "input_tokens")) + tokens(jscompat.Get(u, "cache_read_input_tokens")) + tokens(jscompat.Get(u, "cache_creation_input_tokens"))
		}
		// JS keeps a requestId only when it is a string, and "" is falsy there: no carry-over slot.
		rid, hasRid := jscompat.Get(e, "requestId").(string)
		hasRid = hasRid && rid != ""
		if ctx > 0 && hasRid {
			lastCtx[rid] = ctx
		} else if hasRid {
			if v, ok := lastCtx[rid]; ok {
				ctx = v
			}
		}
		if !(ctx >= threshold) {
			continue
		}
		content, ok := jscompat.Get(e, "message", "content").([]any)
		if !ok {
			continue
		}
		cwd, isStr := jscompat.Get(e, "cwd").(string)
		if !isStr {
			cwd = "."
		}
		check := func(p string) bool { return isFile(cwd, p) }
		for _, b := range content {
			if jscompat.Get(b, "type") != "tool_use" {
				continue
			}
			name, _ := jscompat.Get(b, "name").(string)
			if !broadTools[name] {
				continue
			}
			if id := jscompat.Get(b, "id"); jscompat.Truthy(id) {
				if k, ok := idKey(id); ok {
					if stopped[k] || seen[k] {
						continue
					}
					seen[k] = true
				}
			}
			input := jscompat.Get(b, "input")
			if jscompat.IsUndefined(input) {
				input = nil
			}
			cls := searchscope.Classify(name, input, check)
			if cls.Broad {
				var t int64
				if tsOK {
					t = ts
				}
				rows = append(rows, BroadSearch{Ts: t, Tool: name, Kind: cls.Kind, ContextTokens: ctx})
			}
		}
	}
	return rows
}
