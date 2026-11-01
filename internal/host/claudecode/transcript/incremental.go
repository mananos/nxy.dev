package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mananos/nxy.dev/internal/jscompat"
	"github.com/mananos/nxy.dev/internal/pricing"
)

// maxSeen bounds the Seen map: the oldest keys are dropped past it.
const maxSeen = 256

// Seen is requestId -> largest output seen. It keeps insertion order when written as JSON (a JS
// object), because the bound drops the oldest keys and the statusline persists it. Integer-like
// keys are not reordered as JS would.
type Seen struct {
	keys []string
	m    map[string]float64
}

// Keys is the key order, oldest first.
func (s *Seen) Keys() []string { return append([]string{}, s.keys...) }

// Get returns the output seen for key.
func (s *Seen) Get(key string) (float64, bool) {
	v, ok := s.m[key]
	return v, ok
}

func (s *Seen) set(key string, v float64) {
	if s.m == nil {
		s.m = map[string]float64{}
	}
	if _, ok := s.m[key]; !ok {
		s.keys = append(s.keys, key)
	}
	s.m[key] = v
}

func (s *Seen) trim() {
	if len(s.keys) <= maxSeen {
		return
	}
	drop := s.keys[:len(s.keys)-maxSeen]
	for _, k := range drop {
		delete(s.m, k)
	}
	s.keys = append([]string{}, s.keys[len(s.keys)-maxSeen:]...)
}

// MarshalJSON writes the object in insertion order.
func (s Seen) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range s.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kj, _ := json.Marshal(k)
		vj, err := json.Marshal(s.m[k])
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

// UnmarshalJSON reads an object keeping the key order of the text. null gives an empty map.
func (s *Seen) UnmarshalJSON(data []byte) error {
	*s = Seen{}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		return nil
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("seen: expected an object")
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		var v float64
		if err := dec.Decode(&v); err != nil {
			return err
		}
		s.set(kt.(string), v)
	}
	_, err = dec.Token()
	return err
}

// IncrementalState is what the statusline keeps between renders (JSON keys of the 1.x).
type IncrementalState struct {
	Offset         int64         `json:"offset"`  // bytes already folded
	Partial        string        `json:"partial"` // trailing incomplete line
	Seen           Seen          `json:"seen"`    // requestId -> largest output seen (bounded)
	Usage          pricing.Usage `json:"usage"`
	Usd            float64       `json:"usd"`        // priced part
	UsdUnknown     int           `json:"usdUnknown"` // calls without a price
	Model          *string       `json:"model"`
	Calls          int           `json:"calls"`
	Turns          int           `json:"turns"`          // human prompts seen (main transcript only)
	TurnUsd        float64       `json:"turnUsd"`        // USD of the current turn
	TurnUsdUnknown int           `json:"turnUsdUnknown"` // unpriced calls in the current turn
	LastTs         *int64        `json:"lastTs"`         // ms of the last API call seen
	TurnStartTs    *int64        `json:"turnStartTs"`    // ms of the prompt that opened the turn
	// UsdEstimated is last because the 1.x adds the key when it is first set, after the initial ones.
	UsdEstimated *int `json:"usdEstimated,omitempty"` // calls priced by family; absent until first set
}

// NewIncrementalState is a fresh state.
func NewIncrementalState() *IncrementalState {
	return &IncrementalState{Usage: pricing.EmptyUsage()}
}

// StartTurnAt applies the parent's turn (subagent transcripts): when it changed, the turn totals
// restart. nil leaves the state alone.
func (st *IncrementalState) StartTurnAt(since *int64) {
	if since == nil {
		return
	}
	if st.TurnStartTs == nil || *st.TurnStartTs != *since {
		v := *since
		st.TurnStartTs = &v
		st.TurnUsd = 0
		st.TurnUsdUnknown = 0
	}
}

// Rewind restarts the read when the file is now smaller than what was folded (it was rewritten).
func (st *IncrementalState) Rewind(size int64) {
	if size < st.Offset {
		st.Offset = 0
		st.Partial = ""
	}
}

// Fold folds the bytes appended after Offset (the caller advances Offset): complete assistant
// lines go into Usage, a streamed message that grows counts once (by output delta), human prompts
// start a new turn. With since (subagent transcripts) only calls at or after it count for the turn.
func (st *IncrementalState) Fold(chunk []byte, since *int64) {
	text := st.Partial + string(chunk)
	lines := strings.Split(text, "\n")
	st.Partial = lines[len(lines)-1]
	lines = lines[:len(lines)-1]
	for _, line := range lines {
		isUser := strings.Contains(line, `"type":"user"`)
		if !isUser && !strings.Contains(line, `"usage"`) {
			continue
		}
		e, ok := decodeLine([]byte(line))
		if !ok {
			continue
		}
		etype := jscompat.Get(e, "type")
		if etype == "user" {
			// a plain human prompt on the main transcript starts a new turn; tool results and
			// subagent sidechains never do
			if jscompat.Truthy(jscompat.Get(e, "isSidechain")) || jscompat.Truthy(jscompat.Get(e, "toolUseResult")) {
				continue
			}
			if _, human := humanText(e); !human {
				continue
			}
			st.Turns++
			st.TurnUsd = 0
			st.TurnUsdUnknown = 0
			if pts, valid := jscompat.ParseDate(jscompat.Get(e, "timestamp")); valid {
				st.TurnStartTs = &pts
			} else {
				st.TurnStartTs = nil
			}
			continue
		}
		if etype != "assistant" || !jscompat.Truthy(jscompat.Get(e, "message", "usage")) {
			continue
		}
		key := jscompat.String(jscompat.Or(jscompat.Get(e, "requestId"), jscompat.Get(e, "message", "id"), jscompat.Get(e, "uuid")))
		usage := pricing.ToUsage(jscompat.Get(e, "message", "usage"))
		model := jscompat.String(jscompat.Or(jscompat.Get(e, "message", "model"), "unknown"))
		synthetic := pricing.IsSyntheticModel(model)
		if synthetic && pricing.IsZeroUsage(usage) {
			continue // an internal message, not a call
		}
		ts, tsOk := jscompat.ParseDate(jscompat.Get(e, "timestamp"))
		// a call belongs to the current turn unless it predates the parent's prompt
		inTurn := since == nil || !tsOk || ts >= *since
		prevOut, seenBefore := st.Seen.Get(key)
		var usd float64
		haveUsd := false
		if !seenBefore {
			pricing.AddUsage(&st.Usage, usage)
			if c, priced := pricing.CostFor(model, usage, nil); priced {
				usd, haveUsd = c, true
			} else {
				st.UsdUnknown++
				if inTurn {
					st.TurnUsdUnknown++
				}
			}
			if haveUsd && usd > 0 {
				if _, est := pricing.EstimateBasis(model); est {
					n := 1
					if st.UsdEstimated != nil {
						n = *st.UsdEstimated + 1
					}
					st.UsdEstimated = &n
				}
			}
			st.Calls++
		} else if usage.Output > prevOut {
			delta := pricing.EmptyUsage()
			delta.Output = usage.Output - prevOut
			pricing.AddUsage(&st.Usage, delta)
			// the delta carries no input: the tier belongs to the request's whole prompt
			prompt := pricing.TotalInput(usage)
			if c, priced := pricing.CostFor(model, delta, &prompt); priced {
				usd, haveUsd = c, true
			}
		}
		if haveUsd {
			st.Usd += usd
			if inTurn {
				st.TurnUsd += usd
			}
		}
		keep := usage.Output
		if seenBefore && prevOut > keep {
			keep = prevOut
		} else if !seenBefore && keep < 0 {
			keep = 0
		}
		st.Seen.set(key, keep)
		if !synthetic {
			m := model
			st.Model = &m
		}
		if tsOk && (st.LastTs == nil || ts > *st.LastTs) {
			v := ts
			st.LastTs = &v
		}
	}
	st.Seen.trim()
}
