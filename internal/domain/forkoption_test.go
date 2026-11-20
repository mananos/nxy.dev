package domain

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestForkOptionsReadOldStrings(t *testing.T) {
	var d Design
	if err := json.Unmarshal([]byte(`{"agenda":[{"id":"B1","title":"t","status":"open","options":["uno","dos"]}]}`), &d); err != nil {
		t.Fatal(err)
	}
	got := d.Agenda[0].Options
	if len(got) != 2 || got[0].Label != "uno" || got[1].Label != "dos" || got[0].Key != "" {
		t.Fatalf("options = %+v", got)
	}
}

func TestForkOptionsRoundTrip(t *testing.T) {
	in := Design{Current: "B1", Agenda: []Fork{{ID: "B1", Title: "t", Status: ForkOpen, Question: "q?", Opinion: "A",
		Options: []ForkOption{{Key: "A", Label: "a", Summary: "s", Pros: []string{"p"}, Cons: []string{"c"}, Touches: "x.go", D2: "a -> b"}, {Key: "B", Label: "b"}}}}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Design
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip:\n%+v\n%+v", in, out)
	}
}
