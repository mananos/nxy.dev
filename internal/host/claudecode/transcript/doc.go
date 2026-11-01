// Package transcript is the pure part of the Claude Code transcript format: it turns the bytes of
// one transcript file (JSON lines) into per-call usage records, tool counts and prompts, with the
// results of the 1.x JS (hosts/claude-code/transcripts.mjs). It does no I/O: the claudecode
// adapter reads the files.
//
// Lines are decoded into any and read with the jscompat accessors, so a missing or mistyped field
// behaves as in JS. Edges the port does not reproduce (the 1.x would behave differently or
// crash): a `null` line (JS throws), a non-string uuid / tool_use_id / filePath / model / agentId
// (treated as absent or as their String() form), a non-string timestamp (JS would parse its
// String()), a 200-unit prompt cut that splits a surrogate pair (Go cannot hold half a pair; the
// cut stops before it), and integer-like object keys (JS reorders them).
package transcript
