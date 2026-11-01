package transcript

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSessionCacheKeepsAgentOrderAndFillsDefaults(t *testing.T) {
	text := `{"main":{"offset":12,"calls":2,"usd":0.5},"agents":{"agent-b.jsonl":{"offset":3},"agent-a.jsonl":{"offset":4,"model":"m"}}}`
	c := ParseSessionCache([]byte(text))
	if c.Main.Offset != 12 || c.Main.Calls != 2 || c.Main.Usd != 0.5 {
		t.Fatalf("main: %+v", c.Main)
	}
	if c.Main.Usage.Output != 0 || c.Main.Model != nil || c.Main.LastTs != nil {
		t.Errorf("missing fields should keep the fresh defaults: %+v", c.Main)
	}
	if want := []string{"agent-b.jsonl", "agent-a.jsonl"}; !reflect.DeepEqual(c.Agents.Keys(), want) {
		t.Fatalf("agent order: %v", c.Agents.Keys())
	}
	a, _ := c.Agents.Get("agent-a.jsonl")
	if a.Offset != 4 || a.Model == nil || *a.Model != "m" {
		t.Errorf("agent a: %+v", a)
	}
	out, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back SessionCache
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Agents.Keys(), c.Agents.Keys()) || back.Main.Offset != 12 {
		t.Errorf("round trip: %s", out)
	}
}

func TestSessionCacheStartsFresh(t *testing.T) {
	for _, text := range []string{"", "not json", "[1]", "5", `{"main":5}`} {
		c := ParseSessionCache([]byte(text))
		if c.Main == nil || c.Main.Offset != 0 || c.Agents.Len() != 0 {
			t.Errorf("%q: %+v", text, c)
		}
	}
	c := ParseSessionCache([]byte("null"))
	if c.Main == nil {
		t.Error("null must give a usable cache")
	}
}

func TestAgentsEnsure(t *testing.T) {
	var a Agents
	x := a.Ensure("one")
	x.Offset = 9
	if a.Ensure("one") != x || a.Len() != 1 {
		t.Error("Ensure must reuse the state")
	}
	a.Ensure("two")
	if want := []string{"one", "two"}; !reflect.DeepEqual(a.Keys(), want) {
		t.Errorf("keys: %v", a.Keys())
	}
}
