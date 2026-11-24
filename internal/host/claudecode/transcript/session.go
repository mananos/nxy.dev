package transcript

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strconv"

	"github.com/mananos/nxy.dev/v2/internal/jscompat"
	"github.com/mananos/nxy.dev/v2/internal/metrics"
	"github.com/mananos/nxy.dev/v2/internal/pricing"
)

const defaultCacheBreakThreshold = 100000

// SessionRef is one session transcript found on disk.
type SessionRef struct {
	SessionID string  `json:"sessionId"`
	Path      string  `json:"path"`      // main transcript
	Project   string  `json:"project"`   // project slug
	AgentsDir string  `json:"agentsDir"` // <session>/subagents
	MtimeMs   float64 `json:"mtimeMs"`
	Size      int64   `json:"size"`
}

// Subagent is a subagent transcript of a session with its decoded .meta.json (nil when absent
// or malformed).
type Subagent struct {
	Path    string `json:"path"`
	AgentID string `json:"agentId"`
	Meta    any    `json:"meta"`
}

// SessionOpts are the options of ParseSession. Nil pointers mean the 1.x defaults
// (subagents included, cache-break threshold 100000 uncached tokens).
type SessionOpts struct {
	IncludeSubagents    *bool    `json:"includeSubagents"`
	Since               int64    `json:"since"`
	CacheBreakThreshold *float64 `json:"cacheBreakThreshold"`
	Timing              bool     `json:"timing"`
}

// OrderedMap is a string-keyed object that keeps insertion order when written as JSON (a JS
// object). Integer-like keys are not reordered as JS would.
type OrderedMap[V any] struct {
	Keys   []string
	Values map[string]V
}

func (o *OrderedMap[V]) set(k string, v V) {
	if o.Values == nil {
		o.Values = map[string]V{}
	}
	if _, ok := o.Values[k]; !ok {
		o.Keys = append(o.Keys, k)
	}
	o.Values[k] = v
}

// addCount increases the counter at k by n (a new key goes last, as `obj[k] = (obj[k] || 0) + n`).
func addCount(o *OrderedMap[int], k string, n int) {
	o.set(k, o.Values[k]+n)
}

// MarshalJSON writes the object in insertion order.
func (o OrderedMap[V]) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.Keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kj, _ := json.Marshal(k)
		vj, err := json.Marshal(o.Values[k])
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

// UnmarshalJSON reads an object keeping the order of its keys; an empty object leaves nil.
func (o *OrderedMap[V]) UnmarshalJSON(data []byte) error {
	*o = OrderedMap[V]{}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		return nil
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return errors.New("transcript: expected a JSON object")
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		var v V
		if err := dec.Decode(&v); err != nil {
			return err
		}
		o.set(kt.(string), v)
	}
	_, err = dec.Token()
	return err
}

// AgentTypeBucket is the finished bucket of one agent type plus how many agents ran.
type AgentTypeBucket struct {
	metrics.Finished
	Agents int `json:"agents"`
}

// MarshalJSON writes the 1.x key order: "agents" sits between contextPeak and usdPartial.
func (t AgentTypeBucket) MarshalJSON() ([]byte, error) {
	raw, err := json.Marshal(t.Finished)
	if err != nil {
		return nil, err
	}
	const at = `,"usdPartial":`
	i := bytes.Index(raw, []byte(at))
	if i < 0 {
		return nil, errors.New("transcript: unexpected bucket JSON")
	}
	out := append([]byte{}, raw[:i]...)
	out = append(out, `,"agents":`...)
	out = strconv.AppendInt(out, int64(t.Agents), 10)
	return append(out, raw[i:]...), nil
}

// Agent is one subagent run of the session.
type Agent struct {
	AgentID     string `json:"agentId"`
	AgentType   string `json:"agentType"`
	Description any    `json:"description"`
	Model       any    `json:"model"`
	metrics.Finished
	DurationMs *int64          `json:"durationMs"`
	Timing     *metrics.Timing `json:"timing,omitempty"`
}

// SessionTools merges the tool activity of the main thread and its subagents.
type SessionTools struct {
	Calls           OrderedMap[int] `json:"calls"` // tool name -> uses, in first-use order
	FilesRead       []string        `json:"filesRead"`
	FilesEdited     []string        `json:"filesEdited"`
	FilesEditedMain []string        `json:"filesEditedMain"` // what the main agent wrote itself
	Bash            Bash            `json:"bash"`
}

// Session is everything nxy knows about one session (keys of the 1.x parseSession).
type Session struct {
	SessionID     string                       `json:"sessionId"`
	Project       string                       `json:"project"`
	Cwd           any                          `json:"cwd"`
	Version       any                          `json:"version"`
	GitBranch     any                          `json:"gitBranch"`
	FirstTs       *int64                       `json:"firstTs"`
	LastTs        *int64                       `json:"lastTs"`
	WallMs        int64                        `json:"wallMs"`
	ActiveMs      int64                        `json:"activeMs"`
	Turns         int                          `json:"turns"`
	Prompts       []Prompt                     `json:"prompts"`
	Usage         metrics.Finished             `json:"usage"`
	Main          metrics.Finished             `json:"main"`
	Subagents     metrics.Finished             `json:"subagents"`
	ByModel       OrderedMap[metrics.Finished] `json:"byModel"`
	ByAgentType   OrderedMap[AgentTypeBucket]  `json:"byAgentType"`
	BySkill       OrderedMap[metrics.Finished] `json:"bySkill"`
	Agents        []Agent                      `json:"agents"`
	Tools         SessionTools                 `json:"tools"`
	CacheBreaks   []metrics.Break              `json:"cacheBreaks"`
	TTLWhatIf     metrics.TTLWhatIf            `json:"ttlWhatIf"`
	UnknownModels []string                     `json:"unknownModels"`
	AgentTiming   *metrics.TimingSummary       `json:"agentTiming,omitempty"`
}

// ProjectSlug is the per-project folder name Claude Code derives from the working directory:
// every non-alphanumeric UTF-16 unit becomes "-" (an emoji gives two).
func ProjectSlug(cwd string) string {
	return jscompat.ReplaceUnits(cwd, func(u rune) bool {
		return (u >= 'A' && u <= 'Z') || (u >= 'a' && u <= 'z') || (u >= '0' && u <= '9')
	}, "-")
}

type bucketSet struct {
	keys    []string
	buckets map[string]*metrics.Bucket
}

func (s *bucketSet) get(k string) *metrics.Bucket {
	if s.buckets == nil {
		s.buckets = map[string]*metrics.Bucket{}
	}
	b, ok := s.buckets[k]
	if !ok {
		b = metrics.NewBucket()
		s.buckets[k] = b
		s.keys = append(s.keys, k)
	}
	return b
}

func finishAll(s *bucketSet) OrderedMap[metrics.Finished] {
	var out OrderedMap[metrics.Finished]
	for _, k := range s.keys {
		out.set(k, s.buckets[k].Finish())
	}
	return out
}

// ParseSession aggregates one session. read returns the bytes of a transcript file (an error
// means unreadable: the file counts as empty). The files of the session share one uuid set, so a
// line copied between them is counted once.
func ParseSession(ref SessionRef, subs []Subagent, read func(path string) ([]byte, error), opts SessionOpts) Session {
	includeSubagents := opts.IncludeSubagents == nil || *opts.IncludeSubagents
	threshold := float64(defaultCacheBreakThreshold)
	if opts.CacheBreakThreshold != nil {
		threshold = *opts.CacheBreakThreshold
	}
	readFile := func(p string) ([]byte, bool) {
		data, err := read(p)
		return data, err == nil
	}
	seen := NewUUIDSet()
	mainData, mainOk := readFile(ref.Path)
	main := ParseFile(ref.Path, mainData, mainOk, FileOpts{Since: opts.Since, Seen: seen})

	total := metrics.NewBucket()
	mainBucket := metrics.NewBucket()
	subBucket := metrics.NewBucket()
	var byModel, bySkill bucketSet
	type typeEntry struct {
		b      *metrics.Bucket
		agents int
	}
	var typeKeys []string
	byType := map[string]*typeEntry{}
	cacheBreaks := []metrics.Break{}
	agents := []Agent{}
	var timingRuns []metrics.Run

	var filesRead, filesEdited orderedSet
	for _, p := range main.Tools.FilesRead {
		filesRead.add(p)
	}
	for _, p := range main.Tools.FilesEdited {
		filesEdited.add(p)
	}
	var toolCalls OrderedMap[int]
	for _, k := range main.Tools.Calls.Keys {
		toolCalls.set(k, main.Tools.Calls.Values[k])
	}
	bash := main.Tools.Bash

	ingest := func(call metrics.Call, agentType *string) {
		total.Add(call)
		if agentType != nil {
			subBucket.Add(call)
		} else {
			mainBucket.Add(call)
		}
		byModel.get(call.Model).Add(call)
		if call.Skill != nil && *call.Skill != "" {
			bySkill.get(*call.Skill).Add(call)
		}
		u := call.Usage
		uncached := u.Input + u.CacheWrite5m + u.CacheWrite1h
		if uncached > threshold {
			written := pricing.EmptyUsage()
			written.CacheWrite5m, written.CacheWrite1h, written.Input = u.CacheWrite5m, u.CacheWrite1h, u.Input
			pt := pricing.TotalInput(u)
			ttl := "5m"
			if u.CacheWrite1h > 0 {
				ttl = "1h"
			}
			var usd *float64
			if v, ok := pricing.CostFor(call.Model, written, &pt); ok {
				usd = &v
			}
			cacheBreaks = append(cacheBreaks, metrics.Break{
				Ts:        call.Ts,
				Uncached:  uncached,
				Total:     pt,
				AgentType: copyPtr(agentType),
				Cause:     metrics.BreakCause(call),
				TTL:       ttl,
				USD:       usd,
			})
		}
	}

	for _, c := range main.Calls {
		ingest(c, nil)
	}

	firstTs, lastTs := main.FirstTs, main.LastTs
	if includeSubagents {
		for _, f := range subs {
			link, hasLink := main.AgentLinks.Links[f.AgentID]
			var linkType, linkDesc, linkModel any = jscompat.Undefined, jscompat.Undefined, jscompat.Undefined
			if hasLink {
				linkType, linkDesc, linkModel = link.AgentType, link.Description, link.Model
			}
			agentType := jscompat.String(jscompat.Or(jscompat.Get(f.Meta, "agentType"), linkType, "unknown"))
			data, ok := readFile(f.Path)
			id := f.AgentID
			parsed := ParseFile(f.Path, data, ok, FileOpts{AgentID: &id, Since: opts.Since, Seen: seen})
			if len(parsed.Calls) == 0 && parsed.FirstTs == nil {
				continue
			}
			b := metrics.NewBucket()
			for _, c := range parsed.Calls {
				ingest(c, &agentType)
				b.Add(c)
			}
			at := byType[agentType]
			if at == nil {
				at = &typeEntry{b: metrics.NewBucket()}
				byType[agentType] = at
				typeKeys = append(typeKeys, agentType)
			}
			at.agents++
			for _, c := range parsed.Calls {
				at.b.Add(c)
			}
			var timing *metrics.Timing
			if opts.Timing {
				t := metrics.AgentTiming(Events(data))
				timing = &t
				timingRuns = append(timingRuns, metrics.Run{AgentType: agentType, Timing: timing})
			}
			var duration *int64
			if parsed.FirstTs != nil && parsed.LastTs != nil {
				d := *parsed.LastTs - *parsed.FirstTs
				duration = &d
			}
			agents = append(agents, Agent{
				AgentID:     f.AgentID,
				AgentType:   agentType,
				Description: jscompat.Or(jscompat.Get(f.Meta, "description"), linkDesc, ""),
				Model:       jscompat.Or(parsed.Meta.Model, linkModel, nil),
				Finished:    b.Finish(),
				DurationMs:  duration,
				Timing:      timing,
			})
			for _, k := range parsed.Tools.Calls.Keys {
				addCount(&toolCalls, k, parsed.Tools.Calls.Values[k])
			}
			for _, p := range parsed.Tools.FilesRead {
				filesRead.add(p)
			}
			for _, p := range parsed.Tools.FilesEdited {
				filesEdited.add(p)
			}
			bash.Calls += parsed.Tools.Bash.Calls
			bash.ResultLines += parsed.Tools.Bash.ResultLines
			bash.ResultChars += parsed.Tools.Bash.ResultChars
			bash.Interrupted += parsed.Tools.Bash.Interrupted
			if parsed.FirstTs != nil && (firstTs == nil || *parsed.FirstTs < *firstTs) {
				v := *parsed.FirstTs
				firstTs = &v
			}
			if parsed.LastTs != nil && (lastTs == nil || *parsed.LastTs > *lastTs) {
				v := *parsed.LastTs
				lastTs = &v
			}
		}
	}

	var byAgentType OrderedMap[AgentTypeBucket]
	for _, k := range typeKeys {
		e := byType[k]
		byAgentType.set(k, AgentTypeBucket{Finished: e.b.Finish(), Agents: e.agents})
	}
	usage := total.Finish()
	var wall int64
	if firstTs != nil && lastTs != nil {
		wall = *lastTs - *firstTs
	}
	sort.SliceStable(agents, func(i, j int) bool {
		return agents[i].TotalInput+agents[i].Usage.Output > agents[j].TotalInput+agents[j].Usage.Output
	})
	s := Session{
		SessionID:   ref.SessionID,
		Project:     ref.Project,
		Cwd:         main.Meta.Cwd,
		Version:     main.Meta.Version,
		GitBranch:   main.Meta.GitBranch,
		FirstTs:     firstTs,
		LastTs:      lastTs,
		WallMs:      wall,
		ActiveMs:    main.ActiveMs,
		Turns:       main.Turns,
		Prompts:     main.Prompts,
		Usage:       usage,
		Main:        mainBucket.Finish(),
		Subagents:   subBucket.Finish(),
		ByModel:     finishAll(&byModel),
		ByAgentType: byAgentType,
		BySkill:     finishAll(&bySkill),
		Agents:      agents,
		Tools: SessionTools{
			Calls:           toolCalls,
			FilesRead:       nonNil(filesRead.list),
			FilesEdited:     nonNil(filesEdited.list),
			FilesEditedMain: append([]string{}, main.Tools.FilesEdited...),
			Bash:            bash,
		},
		CacheBreaks:   cacheBreaks,
		TTLWhatIf:     metrics.HourTTLWhatIf(main.Calls), // Claude Code picks the TTL per bucket; this is the main conversation's
		UnknownModels: usage.UnknownModels,
	}
	if opts.Timing {
		t := metrics.SummarizeTiming(timingRuns)
		s.AgentTiming = &t
	}
	return s
}
