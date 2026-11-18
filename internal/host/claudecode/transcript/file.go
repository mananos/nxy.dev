package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/jscompat"
	"github.com/mananos/nxy.dev/v2/internal/metrics"
	"github.com/mananos/nxy.dev/v2/internal/pricing"
)

const idleGapMs = 5 * 60 * 1000 // gaps longer than this do not count as "active" time

// UUIDSet is the set of line uuids already counted, shared by the files of one session so a line
// copied between files is counted once.
type UUIDSet struct{ m map[string]struct{} }

// NewUUIDSet returns an empty set (the zero value works too).
func NewUUIDSet() *UUIDSet { return &UUIDSet{m: map[string]struct{}{}} }

// Has reports whether u was added.
func (s *UUIDSet) Has(u string) bool {
	_, ok := s.m[u]
	return ok
}

// Add puts u in the set.
func (s *UUIDSet) Add(u string) {
	if s.m == nil {
		s.m = map[string]struct{}{}
	}
	s.m[u] = struct{}{}
}

// FileOpts are the options of ParseFile.
type FileOpts struct {
	// AgentID is set when the file is a subagent transcript (no prompts are collected then).
	AgentID *string
	// Since drops lines older than this epoch ms; 0 keeps everything.
	Since int64
	// Seen is shared between the files of a session; nil means a private set.
	Seen *UUIDSet
}

// AgentLink is a subagent run the thread waited for.
type AgentLink struct {
	AgentType   string `json:"agentType"`
	Description any    `json:"description"`
	Model       any    `json:"model"`
	ToolUseID   string `json:"toolUseId"`
}

// AgentLinks is an agentId -> link map that keeps insertion order (a JS Map).
type AgentLinks struct {
	Keys  []string
	Links map[string]AgentLink
}

func (a *AgentLinks) set(id string, l AgentLink) {
	if _, ok := a.Links[id]; !ok {
		a.Keys = append(a.Keys, id)
	}
	a.Links[id] = l
}

// MarshalJSON writes an object in insertion order.
func (a AgentLinks) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range a.Keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kj, _ := json.Marshal(k)
		vj, err := json.Marshal(a.Links[k])
		if err != nil {
			return nil, err
		}
		b.Write(kj)
		b.WriteByte(':')
		b.Write(vj)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Bash is what the Bash tool results of a file add up to.
type Bash struct {
	Calls       int `json:"calls"`
	ResultLines int `json:"resultLines"`
	ResultChars int `json:"resultChars"`
	Interrupted int `json:"interrupted"`
}

// Tools is the tool activity of a file.
type Tools struct {
	Calls       OrderedMap[int] `json:"calls"` // tool name -> uses, in first-use order
	FilesRead   []string        `json:"filesRead"`
	FilesEdited []string        `json:"filesEdited"`
	Bash        Bash            `json:"bash"`
}

// Prompt is one human prompt of the main thread.
type Prompt struct {
	Key     string  `json:"key"`
	Ts      *int64  `json:"ts"`
	Text    string  `json:"text"`
	Command *string `json:"command"`
}

// Meta is what the first lines say about where the session ran.
type Meta struct {
	Cwd       any `json:"cwd"`
	Version   any `json:"version"`
	GitBranch any `json:"gitBranch"`
	Model     any `json:"model"`
}

// FileResult is what one transcript file holds.
type FileResult struct {
	Calls      []metrics.Call `json:"calls"`
	AgentLinks AgentLinks     `json:"agentLinks"`
	Tools      Tools          `json:"tools"`
	Prompts    []Prompt       `json:"prompts"`
	FirstTs    *int64         `json:"firstTs"`
	LastTs     *int64         `json:"lastTs"`
	ActiveMs   int64          `json:"activeMs"`
	Turns      int            `json:"turns"`
	Meta       Meta           `json:"meta"`
}

func emptyResult() FileResult {
	return FileResult{
		Calls:      []metrics.Call{},
		AgentLinks: AgentLinks{Links: map[string]AgentLink{}},
		Tools:      Tools{FilesRead: []string{}, FilesEdited: []string{}},
		Prompts:    []Prompt{},
	}
}

// orderedSet is a JS Set of strings.
type orderedSet struct {
	list []string
	seen map[string]bool
}

func (s *orderedSet) add(v string) {
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	if !s.seen[v] {
		s.seen[v] = true
		s.list = append(s.list, v)
	}
}

func strPtr(s string) *string { return &s }

func copyPtr(p *string) *string {
	if p == nil {
		return nil
	}
	return strPtr(*p)
}

// ParseFile parses one transcript file. Streaming duplicates (same requestId, growing output) are
// collapsed keeping the largest output count and the context of the first chunk. path only labels
// the fallback call keys; ok=false means the file could not be read (empty result).
func ParseFile(path string, data []byte, ok bool, opts FileOpts) FileResult {
	res := emptyResult()
	if !ok {
		return res
	}
	seen := opts.Seen
	if seen == nil {
		seen = NewUUIDSet()
	}
	var calls []metrics.Call
	callIdx := map[string]int{}
	type agentRef struct{ subagentType, description string }
	toolUseToAgent := map[string]agentRef{}
	var filesRead, filesEdited orderedSet
	var prevTs *int64
	var currentSkill, currentPrompt, pendingCause *string
	var prevCallTs *int64

	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		e, good := decodeLine(line)
		if !good {
			continue
		}
		uuid := jscompat.Get(e, "uuid")
		uuidOn := jscompat.Truthy(uuid)
		if uuidOn {
			u := jscompat.String(uuid)
			if seen.Has(u) {
				continue
			}
			seen.Add(u)
		}
		if jscompat.Truthy(jscompat.Get(e, "timestamp")) {
			if ts, valid := jscompat.ParseDate(jscompat.Get(e, "timestamp")); valid {
				if opts.Since != 0 && ts < opts.Since {
					continue
				}
				if res.FirstTs == nil {
					v := ts
					res.FirstTs = &v
				}
				if prevTs != nil {
					gap := ts - *prevTs
					if gap > 0 && gap < idleGapMs {
						res.ActiveMs += gap
					}
				}
				v := ts
				prevTs = &v
				last := ts
				res.LastTs = &last
			}
		}
		if cwd := jscompat.Get(e, "cwd"); !jscompat.Truthy(res.Meta.Cwd) && jscompat.Truthy(cwd) {
			res.Meta.Cwd = cwd
			res.Meta.Version = jscompat.Or(jscompat.Get(e, "version"), nil)
			res.Meta.GitBranch = jscompat.Or(jscompat.Get(e, "gitBranch"), nil)
		}
		etype := jscompat.Get(e, "type")
		if (etype == "system" && jscompat.Get(e, "subtype") == "compact_boundary") || jscompat.Truthy(jscompat.Get(e, "isCompactSummary")) {
			pendingCause = strPtr("compact")
		}

		if etype == "user" {
			tur := jscompat.Get(e, "toolUseResult")
			c := jscompat.Get(e, "message", "content")
			tuid := ""
			if arr, isArr := c.([]any); isArr && len(arr) > 0 {
				tuid, _ = jscompat.Get(arr[0], "tool_use_id").(string)
			}
			isObj := false
			switch tur.(type) {
			case map[string]any, []any:
				isObj = true
			}
			if isObj {
				if agentID := jscompat.Get(tur, "agentId"); jscompat.Truthy(agentID) && tuid != "" {
					if pendingCause == nil || *pendingCause != "compact" {
						pendingCause = strPtr("subagent")
					}
					link, has := toolUseToAgent[tuid]
					agentType := "unknown"
					if has && link.subagentType != "" {
						agentType = link.subagentType
					}
					var linkDesc any = ""
					if has {
						linkDesc = link.description
					}
					res.AgentLinks.set(jscompat.String(agentID), AgentLink{
						AgentType:   agentType,
						Description: jscompat.Or(jscompat.Get(tur, "description"), linkDesc, ""),
						Model:       jscompat.Or(jscompat.Get(tur, "resolvedModel"), nil),
						ToolUseID:   tuid,
					})
				} else if _, so := jscompat.Get(tur, "stdout").(string); so || isString(jscompat.Get(tur, "stderr")) {
					out := jscompat.String(jscompat.Or(jscompat.Get(tur, "stdout"), ""))
					if stderr := jscompat.Get(tur, "stderr"); jscompat.Truthy(stderr) {
						out += "\n" + jscompat.String(stderr)
					}
					res.Tools.Bash.Calls++
					res.Tools.Bash.ResultChars += jscompat.UTF16Len(out)
					if out != "" {
						res.Tools.Bash.ResultLines += strings.Count(out, "\n") + 1
					}
					if jscompat.Truthy(jscompat.Get(tur, "interrupted")) {
						res.Tools.Bash.Interrupted++
					}
				} else if fp, isStr := jscompat.Get(tur, "file", "filePath").(string); isStr && fp != "" {
					filesRead.add(fp)
				}
				continue
			}
			ht, human := humanText(e)
			if !human {
				continue
			}
			cmd, hasCmd := slashCommand(ht)
			if hasCmd {
				currentSkill = strPtr(cmd)
			} else {
				currentSkill = nil // a plain human message resets skill attribution
			}
			if !jscompat.Truthy(jscompat.Get(e, "isSidechain")) && opts.AgentID == nil {
				key := fmt.Sprintf("%s:%d", path, len(res.Prompts))
				if uuidOn {
					key = jscompat.String(uuid)
				}
				currentPrompt = strPtr(key)
				text := jscompat.UTF16Prefix(ht, 200)
				if hasCmd && cmd != "" {
					text = "/" + cmd
				}
				p := Prompt{Key: key, Text: text}
				if res.LastTs != nil {
					v := *res.LastTs
					p.Ts = &v
				}
				if hasCmd {
					p.Command = strPtr(cmd)
				}
				res.Prompts = append(res.Prompts, p)
			}
			continue
		}

		if etype != "assistant" {
			continue
		}
		if content, isArr := jscompat.Get(e, "message", "content").([]any); isArr {
			for _, b := range content {
				if jscompat.Get(b, "type") != "tool_use" {
					continue
				}
				name := jscompat.Get(b, "name")
				addCount(&res.Tools.Calls, jscompat.String(name), 1)
				if n, isStr := name.(string); isStr && editTools[n] {
					p := jscompat.Or(jscompat.Get(b, "input", "file_path"), jscompat.Get(b, "input", "notebook_path"))
					if s, isStr := p.(string); isStr && s != "" {
						filesEdited.add(s)
					}
				}
				if name == "Skill" {
					if sk := jscompat.Get(b, "input", "skill"); jscompat.Truthy(sk) {
						currentSkill = strPtr(jscompat.String(sk))
					}
				}
				if (name == "Agent" || name == "Task") && jscompat.Truthy(jscompat.Get(b, "input")) {
					toolUseToAgent[jscompat.String(jscompat.Get(b, "id"))] = agentRef{
						subagentType: jscompat.String(jscompat.Or(jscompat.Get(b, "input", "subagent_type"), "general-purpose")),
						description:  jscompat.String(jscompat.Or(jscompat.Get(b, "input", "description"), "")),
					}
				}
			}
		}
		if as, isStr := jscompat.Get(e, "attributionSkill").(string); isStr && as != "" {
			currentSkill = strPtr(as)
		}
		rawUsage := jscompat.Get(e, "message", "usage")
		if !jscompat.Truthy(rawUsage) {
			continue
		}
		usage := pricing.ToUsage(rawUsage)
		model := jscompat.Get(e, "message", "model")
		modelStr, _ := model.(string)
		synthetic := pricing.IsSyntheticModel(modelStr)
		// `<synthetic>` with zero usage is an internal Claude Code message, not an API call.
		if synthetic && pricing.IsZeroUsage(usage) {
			continue
		}
		if jscompat.Truthy(model) && !synthetic && !jscompat.Truthy(res.Meta.Model) {
			res.Meta.Model = model
		}
		var key string
		if rid := jscompat.Get(e, "requestId"); jscompat.Truthy(rid) {
			key = jscompat.String(rid)
		} else if id, isStr := jscompat.Get(e, "message", "id").(string); isStr && strings.HasPrefix(id, "msg_") {
			key = id
		} else if uuidOn {
			key = path + ":" + jscompat.String(uuid)
		} else {
			key = fmt.Sprintf("%s:%d", path, len(calls))
		}
		var effort *string
		if s, isStr := jscompat.Get(e, "effort").(string); isStr {
			effort = strPtr(s)
		}
		i, exists := callIdx[key]
		if !exists {
			// Streamed chunks of one request share the key: the context of the call is fixed at its first chunk.
			var ts int64
			if res.LastTs != nil {
				ts = *res.LastTs
			}
			c := metrics.Call{
				Key:       key,
				Ts:        ts,
				Model:     jscompat.String(jscompat.Or(model, "unknown")),
				Usage:     usage,
				Effort:    effort,
				Skill:     copyPtr(currentSkill),
				PromptKey: copyPtr(currentPrompt),
				AgentID:   copyPtr(opts.AgentID),
				After:     copyPtr(pendingCause),
			}
			if prevCallTs != nil {
				g := ts - *prevCallTs
				c.GapMs = &g
			}
			callIdx[key] = len(calls)
			calls = append(calls, c)
			pendingCause = nil
			v := ts
			prevCallTs = &v
		} else if prev := calls[i]; usage.Output >= prev.Usage.Output {
			prev.Usage = usage
			if effort != nil {
				prev.Effort = effort
			}
			calls[i] = prev
		}
	}
	if calls != nil {
		res.Calls = calls
	}
	res.Tools.FilesRead = nonNil(filesRead.list)
	res.Tools.FilesEdited = nonNil(filesEdited.list)
	res.Turns = len(res.Prompts)
	return res
}

func isString(v any) bool {
	_, ok := v.(string)
	return ok
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
