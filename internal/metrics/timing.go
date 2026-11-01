package metrics

import (
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/mananos/nxy.dev/internal/jscompat"
)

// Where a subagent run's time goes: model / Bash / other tools / waiting, with usage-limit gaps
// excluded and counted apart (core/metrics/timing.mjs). Works on parsed transcript lines.

// locateNudgeMark is the fixed first words of the planner nudge (core/search-scope.mjs
// LOCATE_NUDGE_MARK); F3.3 ports search-scope and replaces this copy.
const locateNudgeMark = "nxy locate nudge:"

// isLocateCommand is true when the command runs the locate entry (search-scope.mjs copy).
func isLocateCommand(cmd string) bool { return strings.Contains(cmd, "locate.mjs") }

// JS \s: the ASCII set plus \v and the Unicode spaces (RE2 \s lacks them).
const jsSpaceClass = `[\s\x{000B}\x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}]+`

var limitText = regexp.MustCompile(`(?i)(usage|session|rate)` + jsSpaceClass + `limit|limit reached|limit will reset`)

func isShell(name any) bool {
	s, ok := name.(string)
	return ok && (s == "Bash" || s == "PowerShell")
}

func isString(v any, want string) bool {
	s, ok := v.(string)
	return ok && s == want
}

func blocks(ev any) []any {
	a, _ := jscompat.Get(ev, "message", "content").([]any)
	return a
}

func stringOrEmpty(v any) string {
	s, _ := v.(string)
	return s
}

func resultText(b any) string {
	switch c := jscompat.Get(b, "content").(type) {
	case string:
		return c
	case []any:
		parts := make([]string, len(c))
		for i, x := range c {
			parts[i] = stringOrEmpty(jscompat.Get(x, "text"))
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func isLimitEvent(ev any) bool {
	if isString(jscompat.Get(ev, "apiError"), "usage_limit_reached") || isString(jscompat.Get(ev, "error"), "rate_limit") {
		return true
	}
	if isString(jscompat.Get(ev, "type"), "assistant") && isString(jscompat.Get(ev, "message", "model"), "<synthetic>") {
		bl := blocks(ev)
		parts := make([]string, len(bl))
		for i, b := range bl {
			parts[i] = stringOrEmpty(jscompat.Get(b, "text"))
		}
		return limitText.MatchString(strings.Join(parts, "\n"))
	}
	return false
}

// nullish is JS `v == null` (null or undefined).
func nullish(v any) bool { return v == nil || jscompat.IsUndefined(v) }

// firstNonNullish is a ?? b ?? c ...
func firstNonNullish(vs ...any) any {
	for _, v := range vs {
		if !nullish(v) {
			return v
		}
	}
	return vs[len(vs)-1]
}

func commandText(name, input any) string {
	if isShell(name) {
		v := firstNonNullish(jscompat.Get(input, "command"), "")
		return jscompat.String(v)
	}
	arg := firstNonNullish(jscompat.Get(input, "file_path"), jscompat.Get(input, "pattern"), jscompat.Get(input, "path"), "")
	if jscompat.Truthy(arg) {
		return jscompat.String(name) + " " + jscompat.String(arg)
	}
	return jscompat.String(name)
}

// keyOf maps a JS Map key to a Go string: strings and other values never collide.
func keyOf(v any) string {
	if s, ok := v.(string); ok {
		return "s:" + s
	}
	return "o:" + jscompat.String(v)
}

// Slow is one of the slowest commands of a run.
type Slow struct {
	Tool any    `json:"tool"`
	Text string `json:"text"`
	Ms   int64  `json:"ms"`
}

// Timing is where one subagent run's time went.
type Timing struct {
	WallMs      int64  `json:"wallMs"`
	ModelMs     int64  `json:"modelMs"`
	BashMs      int64  `json:"bashMs"`
	ToolMs      int64  `json:"toolMs"`
	WaitMs      int64  `json:"waitMs"`
	LimitStops  int    `json:"limitStops"`
	LimitMs     int64  `json:"limitMs"`
	Slowest     []Slow `json:"slowest"`
	LocateCalls int    `json:"locateCalls"`
	Nudges      int    `json:"nudges"`
}

type timedEvent struct {
	ev any
	ts int64
}

type interval struct{ start, end int64 }

type request struct{ start, end int64 }

type toolUse struct {
	ts          int64
	name, input any
}

type command struct {
	tool       any
	text       string
	start, end int64
}

const (
	kindLimit = iota
	kindBash
	kindTools
	kindModel
)

type mark struct {
	t    int64
	kind int
	d    int
}

func maxI(a, b int64) int64 {
	if b > a {
		return b
	}
	return a
}

func minI(a, b int64) int64 {
	if b < a {
		return b
	}
	return a
}

// AgentTiming splits one transcript's parsed lines (file order) into model / bash / tools /
// waiting time, excluding usage-limit stops.
func AgentTiming(events []any) Timing {
	var evs []timedEvent
	for _, ev := range events {
		if ts, ok := jscompat.ParseDate(jscompat.Get(ev, "timestamp")); ok {
			evs = append(evs, timedEvent{ev, ts})
		}
	}
	empty := Timing{Slowest: []Slow{}}
	if len(evs) == 0 {
		return empty
	}

	first, last := evs[0].ts, evs[0].ts
	for _, e := range evs {
		if e.ts < first {
			first = e.ts
		}
		if e.ts > last {
			last = e.ts
		}
	}

	// usage-limit stops: from the limit event to the next user/assistant event
	var limits []interval
	for i := range evs {
		if !isLimitEvent(evs[i].ev) {
			continue
		}
		end := evs[i].ts
		for j := i + 1; j < len(evs); j++ {
			t := jscompat.Get(evs[j].ev, "type")
			if isString(t, "user") || isString(t, "assistant") {
				end = evs[j].ts
				break
			}
		}
		limits = append(limits, interval{evs[i].ts, maxI(end, evs[i].ts)})
	}
	sort.SliceStable(limits, func(a, b int) bool { return limits[a].start < limits[b].start })
	var merged []interval
	for _, iv := range limits {
		if n := len(merged); n > 0 && iv.start <= merged[n-1].end {
			merged[n-1].end = maxI(merged[n-1].end, iv.end)
		} else {
			merged = append(merged, iv)
		}
	}
	limits = merged

	// model intervals per request
	var reqOrder []string
	reqs := map[string]*request{}
	var lastOther int64
	haveOther := false
	for _, e := range evs {
		typ := jscompat.Get(e.ev, "type")
		if isString(typ, "assistant") && !isString(jscompat.Get(e.ev, "message", "model"), "<synthetic>") {
			key := jscompat.Or(jscompat.Get(e.ev, "requestId"), jscompat.Get(e.ev, "message", "id"))
			if !jscompat.Truthy(key) {
				continue
			}
			k := keyOf(key)
			if r, ok := reqs[k]; ok {
				r.end = maxI(r.end, e.ts)
			} else {
				start := e.ts
				if haveOther {
					start = lastOther
				}
				reqs[k] = &request{start: start, end: e.ts}
				reqOrder = append(reqOrder, k)
			}
		} else if !isString(typ, "assistant") {
			lastOther, haveOther = e.ts, true
		}
	}
	var model []interval
	for _, k := range reqOrder {
		r := reqs[k]
		model = append(model, interval{minI(r.start, r.end), r.end})
	}

	// command intervals
	uses := map[string]toolUse{}
	var cmds []command
	locateCalls, nudges := 0, 0
	for _, e := range evs {
		for _, b := range blocks(e.ev) {
			id := jscompat.Get(b, "id")
			if isString(jscompat.Get(b, "type"), "tool_use") && jscompat.Truthy(id) {
				name, input := jscompat.Get(b, "name"), jscompat.Get(b, "input")
				uses[keyOf(id)] = toolUse{e.ts, name, input}
				if isShell(name) && isLocateCommand(jscompat.String(firstNonNullish(jscompat.Get(input, "command"), ""))) {
					locateCalls++
				}
			} else if isString(jscompat.Get(b, "type"), "tool_result") {
				if strings.Contains(resultText(b), locateNudgeMark) {
					nudges++
				}
				k := keyOf(jscompat.Get(b, "tool_use_id"))
				if u, ok := uses[k]; ok {
					delete(uses, k)
					cmds = append(cmds, command{u.name, commandText(u.name, u.input), u.ts, maxI(e.ts, u.ts)})
				}
			}
		}
	}

	// sweep: priority limit > bash > tools > model > waiting
	var marks []mark
	add := func(iv interval, kind int) {
		s := maxI(iv.start, first)
		e := minI(iv.end, last)
		if e > s {
			marks = append(marks, mark{s, kind, 1}, mark{e, kind, -1})
		}
	}
	for _, iv := range limits {
		add(iv, kindLimit)
	}
	for _, c := range cmds {
		kind := kindTools
		if isShell(c.tool) {
			kind = kindBash
		}
		add(interval{c.start, c.end}, kind)
	}
	for _, iv := range model {
		add(iv, kindModel)
	}
	sort.SliceStable(marks, func(a, b int) bool { return marks[a].t < marks[b].t })
	var count [4]int
	var sum [4]int64
	var wait int64
	cursor := first
	for i := 0; i < len(marks); {
		t := marks[i].t
		if t > cursor {
			switch {
			case count[kindLimit] > 0:
				sum[kindLimit] += t - cursor
			case count[kindBash] > 0:
				sum[kindBash] += t - cursor
			case count[kindTools] > 0:
				sum[kindTools] += t - cursor
			case count[kindModel] > 0:
				sum[kindModel] += t - cursor
			default:
				wait += t - cursor
			}
			cursor = t
		}
		for i < len(marks) && marks[i].t == t {
			count[marks[i].kind] += marks[i].d
			i++
		}
	}
	if last > cursor {
		wait += last - cursor // nothing open past the last mark
	}

	slowest := make([]Slow, 0, len(cmds))
	for _, c := range cmds {
		slowest = append(slowest, Slow{c.tool, c.text, c.end - c.start})
	}
	sort.SliceStable(slowest, func(a, b int) bool { return slowest[a].Ms > slowest[b].Ms })
	if len(slowest) > 5 {
		slowest = slowest[:5]
	}

	return Timing{
		WallMs:      last - first - sum[kindLimit],
		ModelMs:     sum[kindModel],
		BashMs:      sum[kindBash],
		ToolMs:      sum[kindTools],
		WaitMs:      wait,
		LimitStops:  len(limits),
		LimitMs:     sum[kindLimit],
		Slowest:     slowest,
		LocateCalls: locateCalls,
		Nudges:      nudges,
	}
}

// Quantile is the nearest rank on an ascending slice (same rule as entries/gate.mjs).
func Quantile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(math.Floor(float64(len(sorted)) * p))
	if i > len(sorted)-1 {
		i = len(sorted) - 1
	}
	return sorted[i]
}

// Run is one subagent run: its type and timing (nil timing is skipped).
type Run struct {
	AgentType string  `json:"agentType"`
	Timing    *Timing `json:"timing"`
}

// TypeSummary is the timing of all runs of one agent type.
type TypeSummary struct {
	AgentType  string `json:"agentType"`
	Runs       int    `json:"runs"`
	MedianMs   int64  `json:"medianMs"`
	P90Ms      int64  `json:"p90Ms"`
	MaxMs      int64  `json:"maxMs"`
	ModelMs    int64  `json:"modelMs"`
	BashMs     int64  `json:"bashMs"`
	ToolMs     int64  `json:"toolMs"`
	WaitMs     int64  `json:"waitMs"`
	LimitStops int    `json:"limitStops"`
	WithLocate int    `json:"withLocate"`
	Nudges     int    `json:"nudges"`
}

// SlowAgent is a slow command tagged with the agent type that ran it.
type SlowAgent struct {
	AgentType string `json:"agentType"`
	Slow
}

// TimingSummary is what SummarizeTiming returns.
type TimingSummary struct {
	Types      []TypeSummary `json:"types"`
	Slowest    []SlowAgent   `json:"slowest"`
	LimitStops int           `json:"limitStops"`
}

// SummarizeTiming groups runs by agent type; types sort by runs desc, then by name (localeCompare).
func SummarizeTiming(runs []Run) TimingSummary {
	var order []string
	groups := map[string][]*Timing{}
	var all []SlowAgent
	limitStops := 0
	for _, r := range runs {
		if r.Timing == nil {
			continue
		}
		typ := r.AgentType
		if typ == "" {
			typ = "unknown"
		}
		if _, ok := groups[typ]; !ok {
			order = append(order, typ)
		}
		groups[typ] = append(groups[typ], r.Timing)
		limitStops += r.Timing.LimitStops
		for _, s := range r.Timing.Slowest {
			all = append(all, SlowAgent{typ, s})
		}
	}
	types := []TypeSummary{}
	for _, typ := range order {
		ts := groups[typ]
		walls := make([]int64, len(ts))
		for i, t := range ts {
			walls[i] = t.WallMs
		}
		sort.SliceStable(walls, func(a, b int) bool { return walls[a] < walls[b] })
		s := TypeSummary{
			AgentType: typ,
			Runs:      len(ts),
			MedianMs:  Quantile(walls, 0.5),
			P90Ms:     Quantile(walls, 0.9),
			MaxMs:     walls[len(walls)-1],
		}
		for _, t := range ts {
			s.ModelMs += t.ModelMs
			s.BashMs += t.BashMs
			s.ToolMs += t.ToolMs
			s.WaitMs += t.WaitMs
			s.LimitStops += t.LimitStops
			s.Nudges += t.Nudges
			if t.LocateCalls > 0 {
				s.WithLocate++
			}
		}
		types = append(types, s)
	}
	sort.SliceStable(types, func(a, b int) bool {
		if types[a].Runs != types[b].Runs {
			return types[a].Runs > types[b].Runs
		}
		return jscompat.LocaleCompare(types[a].AgentType, types[b].AgentType) < 0
	})
	sort.SliceStable(all, func(a, b int) bool { return all[a].Ms > all[b].Ms })
	if len(all) > 5 {
		all = all[:5]
	}
	if all == nil {
		all = []SlowAgent{}
	}
	return TimingSummary{Types: types, Slowest: all, LimitStops: limitStops}
}
