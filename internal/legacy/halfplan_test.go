package legacy

import (
	"reflect"
	"strings"
	"testing"
)

const halfPlanText = "## Plan\nGoal: hacer algo\n### Batch 1 — a: b\n- `a.go:1` — c\nAccept: `go test`\n### Batch 2 — d\nAccept: manual — ver"

// Values computed with the 1.x (core/plan.mjs planHash(extractPlan(body))).
const halfPlanHash = "f129d311"

func halfBody() string {
	return "# h\n## Next\nx\n\n" + halfPlanText + "\n## Otra\nz"
}

func TestPlanHashAndExtract(t *testing.T) {
	if got := ExtractPlan(halfBody()); got != halfPlanText {
		t.Fatalf("ExtractPlan = %q", got)
	}
	crlf := strings.ReplaceAll(halfBody(), "\n", "\r\n")
	if got := ExtractPlan(crlf); PlanHash(got) != halfPlanHash {
		t.Fatalf("crlf hash = %s", PlanHash(got))
	}
	var ind []string
	for _, l := range strings.Split(halfPlanText, "\n") {
		ind = append(ind, "  "+l)
	}
	if got := PlanHash(strings.Join(ind, "\n")); got != halfPlanHash {
		t.Fatalf("indented hash = %s", got)
	}
	if PlanHash(halfPlanText) != halfPlanHash {
		t.Fatalf("hash = %s", PlanHash(halfPlanText))
	}
	if ExtractPlan("sin plan") != "" {
		t.Fatal("no plan must give empty")
	}
	if PlanGoal(halfPlanText) != "hacer algo" {
		t.Fatalf("goal = %q", PlanGoal(halfPlanText))
	}
	if got := PlanBatches(halfPlanText + "\n### Questions\n- Q: x\n### Repos\n- `.`"); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("batches = %v", got)
	}
}

func handoff(id, body string, updated int64) Memory {
	return Memory{ID: id, Scope: "project", Type: "handoff", Title: "handoff: titulo", Body: body, Updated: updated}
}

func TestFindHalfPlans(t *testing.T) {
	h := halfPlanHash
	cases := []struct {
		name    string
		mems    []Memory
		files   PlanFiles
		open    []int
		started bool
		isOpen  bool
	}{
		{"approved, no verify", []Memory{handoff("handoff-a-1", halfBody(), 5)}, PlanFiles{Approved: []string{h}}, []int{1, 2}, false, true},
		{"batch 1 pass, 2 fail", []Memory{handoff("handoff-a-1", halfBody(), 5)},
			PlanFiles{Verify: map[string]map[int]string{h: {1: "pass", 2: "fail"}}}, []int{2}, true, true},
		{"batch 2 running", []Memory{handoff("handoff-a-1", halfBody(), 5)},
			PlanFiles{Verify: map[string]map[int]string{h: {1: "pass", 2: "running"}}}, []int{2}, true, true},
		{"batch 2 missing", []Memory{handoff("handoff-a-1", halfBody(), 5)},
			PlanFiles{Verify: map[string]map[int]string{h: {1: "pass"}}}, []int{2}, true, true},
		{"private live handoff", []Memory{{ID: "handoff-a-1", Scope: "area", Type: "handoff", Body: halfBody(), Private: true}},
			PlanFiles{Approved: []string{h}}, []int{1, 2}, false, true},
		{"all done", []Memory{handoff("handoff-a-1", halfBody(), 5)},
			PlanFiles{Verify: map[string]map[int]string{h: {1: "pass", 2: "manual"}}}, nil, false, false},
		{"all none", []Memory{handoff("handoff-a-1", halfBody(), 5)},
			PlanFiles{Verify: map[string]map[int]string{h: {1: "none", 2: "pass"}}}, nil, false, false},
		{"archived", []Memory{handoff("handoff-a-1-done-zz", halfBody(), 5)}, PlanFiles{Approved: []string{h}}, nil, false, false},
		{"plan revised", []Memory{handoff("handoff-a-1", strings.Replace(halfBody(), "algo", "otra cosa", 1), 5)},
			PlanFiles{Approved: []string{h}}, nil, false, false},
	}
	for _, c := range cases {
		cands := Candidates(c.files, nil)
		open, closed := FindHalfPlans(c.mems, c.files, cands)
		if c.isOpen {
			if len(open) != 1 || len(closed) != 0 {
				t.Fatalf("%s: open=%v closed=%v", c.name, open, closed)
			}
			if !reflect.DeepEqual(open[0].Open, c.open) || open[0].Started != c.started || open[0].Hash != h {
				t.Fatalf("%s: %+v", c.name, open[0])
			}
			if open[0].Goal != "hacer algo" {
				t.Fatalf("%s: goal %q", c.name, open[0].Goal)
			}
		} else if len(open) != 0 || len(closed) != 1 || closed[0] != h {
			t.Fatalf("%s: open=%v closed=%v", c.name, open, closed)
		}
	}
}

func TestCandidatesAnswered(t *testing.T) {
	f := PlanFiles{Approved: []string{"bb", "aa"}, Verify: map[string]map[int]string{"cc": {1: "pass"}, "aa": {1: "pass"}}}
	got := Candidates(f, func(h string) bool { return h == "bb" })
	if !reflect.DeepEqual(got, []string{"aa", "cc"}) {
		t.Fatalf("got %v", got)
	}
}

func TestHalfPlanTexts(t *testing.T) {
	// no goal in the plan: falls back to the handoff title, then to the hash
	open, _ := FindHalfPlans([]Memory{handoff("handoff-a-1", "## Plan\n### Batch 1 — x\nAccept: `t`", 1)}, PlanFiles{}, []string{PlanHash("## Plan\n### Batch 1 — x\nAccept: `t`")})
	if len(open) != 1 || open[0].Goal != "titulo" {
		t.Fatalf("fallback goal: %+v", open)
	}

	hp := HalfPlan{Hash: halfPlanHash, Goal: "hacer algo", Plan: halfPlanText, Open: []int{2, 3}, Started: true}
	if n := HalfPlanNotice(hp); !strings.Contains(n, "«hacer algo»") || !strings.Contains(n, "faltan los lotes 2 y 3") {
		t.Fatalf("notice = %q", n)
	}
	hp.Started = false
	if n := HalfPlanNotice(hp); !strings.Contains(n, "aprobada, sin lotes corridos") {
		t.Fatalf("notice = %q", n)
	}
	got, ok := ParseHalfPlanHash(HalfPlanQuestion(hp))
	if !ok || got != halfPlanHash {
		t.Fatalf("round trip = %q %v", got, ok)
	}
	hp.Goal = "con » (plan x) raro"
	if got, ok := ParseHalfPlanHash(HalfPlanQuestion(hp)); !ok || got != halfPlanHash {
		t.Fatalf("round trip with odd goal = %q %v", got, ok)
	}
	if _, ok := ParseHalfPlanHash("otra pregunta"); ok {
		t.Fatal("must not parse an unrelated question")
	}
	req := hp.Request()
	if !strings.HasPrefix(req, hp.Goal+"\n\n") || !strings.Contains(req, "    ### Batch 1") {
		t.Fatalf("request = %q", req)
	}
	for _, l := range strings.Split(req, "\n") {
		if strings.HasPrefix(l, "#") {
			t.Fatalf("line starts with #: %q", l)
		}
	}
}
