package views

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

func TestParseAtShapes(t *testing.T) {
	names := []string{"api", "web"}
	cases := []struct {
		at, repo, path string
		line           int
		isNew          bool
	}{
		{"src/A.java:12", "", "src/A.java", 12, false},
		{"src/A.java:3-9", "", "src/A.java", 3, false},
		{"src/A.java (new)", "", "src/A.java", 0, true},
		{`src\A.java:4`, "", "src/A.java", 4, false},
		{"api/src/A.java:7", "api", "src/A.java", 7, false},
		{"web/src/B.ts (new)", "web", "src/B.ts", 0, true},
	}
	for _, c := range cases {
		n := names
		if c.repo == "" {
			n = nil
		}
		repo, path, line, isNew := ParseAt(c.at, n)
		if repo != c.repo || path != c.path || line != c.line || isNew != c.isNew {
			t.Errorf("ParseAt(%q) = %q %q %d %v, want %q %q %d %v", c.at, repo, path, line, isNew, c.repo, c.path, c.line, c.isNew)
		}
	}
}

func planFeature(phase domain.Phase) domain.Feature {
	f := baseFeature(phase)
	f.Plan = &domain.Plan{
		Title:    "Export command",
		Criteria: []domain.Criterion{{ID: "R1", Text: "exports", Test: domain.TestTDD}},
		Decisions: []domain.Decision{
			{ID: "D1", Scope: domain.Scope("task"), Text: "plain", Question: "q"},
			{ID: "D2", Scope: domain.Scope("task"), Text: "from fork", Fork: "B1"},
		},
		Batches: []domain.Batch{
			{N: 1, Title: "one", Change: []domain.Change{{At: "src/A.java:12", What: "edit"}, {At: "src/A.java:30-40", What: "more"}, {At: "src/New.java (new)", What: "add"}}, Test: domain.BatchTest{Accept: "go test"}},
			{N: 2, Title: "two", Depends: []int{1}},
		},
	}
	return f
}

func TestBuildPlanNoPlan(t *testing.T) {
	f := baseFeature(domain.PhasePlanning)
	f.Plan = nil
	if _, ok := BuildPlan(PlanIn{FeatureIn: FeatureIn{Feature: f, Now: testNow}}); ok {
		t.Fatal("a feature without a plan must give false")
	}
}

func TestBuildPlanStatesAndImpact(t *testing.T) {
	f := planFeature(domain.PhaseApproval)
	in := PlanIn{
		FeatureIn: FeatureIn{Feature: f, Now: testNow}, Source: "name",
		Files: map[string]FileImpact{
			FileKey("api", "src/A.java"): {Exists: true, ImportedBy: []string{"B"}, Tests: []TestRef{{Path: "ATest.java", Status: TestPass}}},
		},
	}
	// one repo: the name is the base name of the project dir
	in.Files = map[string]FileImpact{
		FileKey("api", "src/A.java"): {Exists: true, ImportedBy: []string{"B"}, Tests: []TestRef{{Path: "ATest.java", Status: TestPass}}},
	}
	p, ok := BuildPlan(in)
	if !ok {
		t.Fatal("no plan")
	}
	if p.State != PlanApproval || p.ApprovedAt != "" {
		t.Errorf("approval: state %q approved_at %q", p.State, p.ApprovedAt)
	}
	if p.Impact.Source != "name" || p.Goal != "Export command" {
		t.Errorf("source %q goal %q", p.Impact.Source, p.Goal)
	}
	if len(p.Actions) != 4 || p.Actions[1].ID != "approve" || p.Actions[2].ID != "change" || p.Actions[3].ID != "stop" {
		t.Fatalf("actions = %+v, want life, approve, change, stop", p.Actions)
	}
	if st := p.Actions[3]; strings.Join(st.Argv, " ") != "nxy feature stop --feature "+f.ID || st.Confirm == "" || len(p.Actions[1].Argv) != 0 {
		t.Errorf("stop %+v, approve %+v", st, p.Actions[1])
	}
	// with the approval question waiting, approve and change answer it
	f.Next = domain.Action{Kind: domain.AskApproval}
	in.Feature = f
	in.Waiting = waitingOf(t, f, "A1")
	if wp, _ := BuildPlan(in); strings.Join(wp.Actions[1].Argv, " ") != "nxy action decide --ask A1 --answer Approve" ||
		strings.Join(wp.Actions[2].Argv, " ") != "nxy action decide --ask A1 --answer Change" {
		t.Errorf("waiting actions = %+v", wp.Actions)
	}
	in.Waiting = nil
	if len(p.Decisions) != 2 || p.Decisions[0].FromDesign || !p.Decisions[1].FromDesign {
		t.Errorf("decisions = %+v", p.Decisions)
	}
	if p.Batches[1].Depends[0] != 1 || p.Batches[0].Depends == nil || p.Batches[0].Attempt != 0 {
		t.Errorf("batches = %+v", p.Batches)
	}
	if len(p.Impact.Files) != 2 {
		t.Fatalf("impact files = %+v", p.Impact.Files)
	}
	a, n := p.Impact.Files[0], p.Impact.Files[1]
	if a.Path != "src/A.java" || a.Kind != "edit" || len(a.Lines) != 2 || a.Lines[1].Line != 30 || a.Repo != "api" {
		t.Errorf("edit file = %+v", a)
	}
	if len(a.ImportedBy) != 1 || len(a.Tests) != 1 || a.Contract != nil {
		t.Errorf("edit file info = %+v", a)
	}
	if n.Kind != "new" || n.Tests == nil || n.ImportedBy == nil {
		t.Errorf("new file = %+v", n)
	}

	// a file that is not at the base is new even without the (new) mark
	in.Files[FileKey("api", "src/A.java")] = FileImpact{}
	p, _ = BuildPlan(in)
	if p.Impact.Files[0].Kind != "new" {
		t.Errorf("kind = %q, want new", p.Impact.Files[0].Kind)
	}
}

func TestBuildPlanRunningAndDone(t *testing.T) {
	f := planFeature(domain.PhaseRunningBatch)
	f.State.Steps = []domain.Step{
		{N: 1, Status: domain.StepGreen, Launched: true, StartedAt: 5000, EndedAt: 6000},
		{N: 2, Status: domain.StepRunning, Launched: true, Attempt: 1, StartedAt: 4000},
	}
	p, _ := BuildPlan(PlanIn{FeatureIn: FeatureIn{Feature: f, Now: testNow}})
	if p.State != PlanRunning || p.ApprovedAt != rfc(4000) {
		t.Errorf("running: state %q approved_at %q", p.State, p.ApprovedAt)
	}
	if p.Batches[0].Attempt != 1 || p.Batches[1].Attempt != 2 || p.Batches[1].Attempts != MaxAttempts {
		t.Errorf("attempts = %+v", p.Batches)
	}
	if p.Batches[0].Status != StepGreen || p.Impact.Source != "none" {
		t.Errorf("status %q source %q", p.Batches[0].Status, p.Impact.Source)
	}

	f.State.Phase = domain.PhaseDone
	p, _ = BuildPlan(PlanIn{FeatureIn: FeatureIn{Feature: f, Now: testNow}})
	if p.State != PlanDone || len(p.Actions) != 0 || p.Actions == nil {
		t.Errorf("done: state %q actions %v", p.State, p.Actions)
	}
}

func TestBuildPlanMultiRepo(t *testing.T) {
	f := planFeature(domain.PhaseApproval)
	f.Repos = []domain.FeatureRepo{{Name: "api", Root: "/srv/dev/api"}, {Name: "web", Root: "/srv/dev/web"}}
	f.Plan.Batches = []domain.Batch{{N: 1, Change: []domain.Change{{At: "web/src/B.ts:3", What: "x"}}}}
	p, _ := BuildPlan(PlanIn{FeatureIn: FeatureIn{Feature: f, Now: testNow}})
	if len(p.Impact.Files) != 1 || p.Impact.Files[0].Repo != "web" || p.Impact.Files[0].Path != "src/B.ts" {
		t.Errorf("files = %+v", p.Impact.Files)
	}
}
