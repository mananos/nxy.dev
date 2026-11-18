package plan

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

func proofPlan() domain.Plan {
	return domain.Plan{
		Criteria: []domain.Criterion{
			{ID: "R1", Test: domain.TestTDD},
			{ID: "R2", Test: domain.TestExisting},
			{ID: "R3", Test: domain.TestManual, Check: "look"},
		},
		Batches: []domain.Batch{
			{N: 1, Covers: []string{"R1"}, Test: domain.BatchTest{Kind: domain.BatchTestNew, Accept: "go test"}},
			{N: 2, Covers: []string{"R2"}, Test: domain.BatchTest{Kind: domain.BatchTestExisting, Accept: "go test"}},
			{N: 3, Covers: []string{"R3"}, Test: domain.BatchTest{Kind: domain.BatchTestNew}},
			{N: 4, Covers: []string{"R2"}, Test: domain.BatchTest{Kind: domain.BatchTestNew, Accept: "go build"}},
		},
	}
}

func TestNeedOf(t *testing.T) {
	p := proofPlan()
	for n, want := range map[int]Need{
		1: {Red: true, Green: true},
		2: {Green: true},
		3: {},
		4: {Green: true},
		0: {},
		9: {},
	} {
		if got := NeedOf(p, n); got != want {
			t.Errorf("batch %d: got %+v, want %+v", n, got, want)
		}
	}
}

func TestVerdict(t *testing.T) {
	tdd := Need{Red: true, Green: true}
	green := Need{Green: true}
	red := &domain.Run{Exit: 1}
	ok := &domain.Run{Exit: 0, Edits: 2}
	cases := []struct {
		name string
		need Need
		pr   *domain.Proof
		ok   bool
		why  string
	}{
		{"tdd red and green", tdd, &domain.Proof{Batch: 1, Red: red, Green: ok, Edits: 2}, true, ""},
		{"green without red", tdd, &domain.Proof{Batch: 1, Green: ok, Edits: 2}, false, "no red"},
		{"red that passed", tdd, &domain.Proof{Batch: 1, Red: &domain.Run{}, Green: ok, Edits: 2}, false, "red passed"},
		{"edit after green", green, &domain.Proof{Batch: 1, Green: ok, Edits: 3}, false, "written after"},
		{"existing green only", green, &domain.Proof{Batch: 1, Green: ok, Edits: 2}, true, ""},
		{"failed green carries tail", green, &domain.Proof{Batch: 1, Green: &domain.Run{Exit: 2, Tail: "boom"}}, false, "boom"},
		{"no green", green, &domain.Proof{Batch: 1}, false, "no green"},
		{"manual needs nothing", Need{}, nil, true, ""},
		{"proof of another batch", green, &domain.Proof{Batch: 2, Green: ok, Edits: 2}, false, "no green"},
		{"nil proof", green, nil, false, "no green"},
	}
	for _, c := range cases {
		gotOK, why := Verdict(c.need, c.pr, 1)
		if gotOK != c.ok || !strings.Contains(why, c.why) {
			t.Errorf("%s: got (%v, %q), want (%v, ~%q)", c.name, gotOK, why, c.ok, c.why)
		}
	}
}
