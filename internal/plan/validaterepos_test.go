package plan

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

func multiPlan() domain.Plan {
	return domain.Plan{
		Repos: []domain.PlanRepo{{Path: "api"}, {Path: "web"}},
		Decisions: []domain.Decision{
			{Scope: domain.ScopeTask, Text: "x", Repo: "api"},
		},
		Batches: []domain.Batch{
			{Change: []domain.Change{{At: "api/a.go:1"}, {At: "api/b.go (new)"}}},
			{Change: []domain.Change{{At: "web/a.js:1"}}},
		},
	}
}

func TestValidateReposGood(t *testing.T) {
	if got := ValidateRepos(multiPlan(), []string{"api", "web"}); len(got) != 0 {
		t.Fatalf("problems: %v", got)
	}
}

func TestValidateReposProblems(t *testing.T) {
	p := multiPlan()
	p.Decisions = append(p.Decisions, domain.Decision{Scope: domain.ScopeTask, Text: "y"})
	p.Batches[1].Change = append(p.Batches[1].Change, domain.Change{At: "api/c.go:2"})
	p.Batches = append(p.Batches, domain.Batch{Change: []domain.Change{{At: "other/x.go:1"}}})
	var all []string
	for _, pr := range ValidateRepos(p, []string{"api", "web"}) {
		all = append(all, pr.String())
	}
	got := strings.Join(all, "\n")
	for _, want := range []string{"decision 2: needs repo", "touches repos web and api", "batch 3 change 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestValidateReposWithoutRepos(t *testing.T) {
	p := domain.Plan{Decisions: []domain.Decision{{Repo: "api"}}}
	if got := ValidateRepos(p, nil); len(got) != 1 {
		t.Fatalf("problems: %v", got)
	}
	if got := ValidateRepos(domain.Plan{}, nil); len(got) != 0 {
		t.Fatalf("problems: %v", got)
	}
}

func TestMultiRepoExampleIsValid(t *testing.T) {
	p, err := Decode(strings.NewReader(multiRepoExample))
	if err != nil {
		t.Fatal(err)
	}
	if ps := Validate(p); len(ps) != 0 {
		t.Fatalf("example has problems: %v", ps)
	}
	if ps := ValidateRepos(p, []string{"api", "web"}); len(ps) != 0 {
		t.Fatalf("example has repo problems: %v", ps)
	}
	for _, f := range []domain.Feature{{ID: "f"}, {ID: "f", Mode: domain.ModeEngineer}} {
		if out := PlannerPrompt(f, "nxy", ""); !strings.Contains(out, "Greeting endpoint and page") {
			t.Errorf("prompt (mode %q) lacks the multi-repo example", f.Mode)
		}
	}
}

func TestDecodeAcceptsRepos(t *testing.T) {
	p, err := Decode(strings.NewReader(`{"title":"t","repos":[{"path":".","role":"contract"}],"decisions":[{"scope":"task","text":"x","repo":"api"}]}`))
	if err != nil || len(p.Repos) != 1 || p.Decisions[0].Repo != "api" {
		t.Fatalf("%v %+v", err, p)
	}
}
