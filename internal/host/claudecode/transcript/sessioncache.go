package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Agents is the per-subagent incremental state of a SessionCache, keyed by transcript file name.
// It keeps insertion order: the statusline sums the states in that order and float sums depend
// on it (a JS object keeps it too).
type Agents struct {
	keys []string
	m    map[string]*IncrementalState
}

// Keys is the key order, oldest first.
func (a *Agents) Keys() []string { return append([]string{}, a.keys...) }

// Len is the number of agents.
func (a *Agents) Len() int { return len(a.keys) }

// Get returns the state of name.
func (a *Agents) Get(name string) (*IncrementalState, bool) {
	st, ok := a.m[name]
	return st, ok
}

// Ensure returns the state of name, adding a fresh one (`cache.agents[n] ||= new`) when absent.
func (a *Agents) Ensure(name string) *IncrementalState {
	if st, ok := a.m[name]; ok {
		return st
	}
	if a.m == nil {
		a.m = map[string]*IncrementalState{}
	}
	st := NewIncrementalState()
	a.keys = append(a.keys, name)
	a.m[name] = st
	return st
}

// Each calls fn for every agent in order.
func (a *Agents) Each(fn func(name string, st *IncrementalState)) {
	for _, k := range a.keys {
		fn(k, a.m[k])
	}
}

// SessionCache is what the statusline persists per session: the main transcript's state and one
// per subagent. JSON is {"main": ..., "agents": {...}}, as the 1.x writes it.
type SessionCache struct {
	Main   *IncrementalState `json:"main"`
	Agents Agents            `json:"agents"`
}

// NewSessionCache is a fresh cache.
func NewSessionCache() SessionCache {
	return SessionCache{Main: NewIncrementalState()}
}

// MarshalJSON writes the agents object in insertion order.
func (a Agents) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range a.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kj, _ := json.Marshal(k)
		vj, err := json.Marshal(a.m[k])
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

// UnmarshalJSON reads an object keeping the key order of the text; every state starts from a
// fresh one, so fields an older version did not write keep their defaults. null gives no agents.
func (a *Agents) UnmarshalJSON(data []byte) error {
	*a = Agents{}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		return nil
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("agents: expected an object")
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		st := NewIncrementalState()
		if err := dec.Decode(st); err != nil {
			return err
		}
		name := kt.(string)
		if _, dup := a.m[name]; dup {
			a.m[name] = st // a repeated key keeps its first position
			continue
		}
		if a.m == nil {
			a.m = map[string]*IncrementalState{}
		}
		a.keys = append(a.keys, name)
		a.m[name] = st
	}
	_, err = dec.Token()
	return err
}

// UnmarshalJSON fills the main state over a fresh one (as loadCache does at statusline.mjs:60).
func (c *SessionCache) UnmarshalJSON(data []byte) error {
	out := NewSessionCache()
	var raw struct {
		Main   json.RawMessage `json:"main"`
		Agents Agents          `json:"agents"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw.Main) > 0 && string(raw.Main) != "null" {
		if err := json.Unmarshal(raw.Main, out.Main); err != nil {
			return err
		}
	}
	out.Agents = raw.Agents
	*c = out
	return nil
}

// ParseSessionCache reads a saved cache; unreadable text gives a fresh cache (the 1.x "start fresh").
func ParseSessionCache(data []byte) SessionCache {
	var c SessionCache
	if err := json.Unmarshal(data, &c); err != nil {
		return NewSessionCache()
	}
	return c
}
