package views

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/memory"
)

func sp(s string) *string { return &s }

func fileIn(f domain.Feature) FileIn {
	return FileIn{FeatureIn: FeatureIn{Feature: f, Now: testNow}, Repo: "api", Path: "src/Nota.java", Source: "codegraph"}
}

func TestBuildFileHunksOfAnEdit(t *testing.T) {
	before := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n"
	after := "a\nb\nc\nd\nE\nf\ng\nh\ni\nj\n"
	in := fileIn(planFeature(domain.PhaseRunningBatch))
	in.Before, in.After = sp(before), sp(after)
	out := BuildFile(in)
	if len(out.Hunks) != 1 {
		t.Fatalf("hunks = %+v", out.Hunks)
	}
	h := out.Hunks[0]
	if h.OldStart != 2 || h.NewStart != 2 || len(h.Lines) != 8 {
		t.Fatalf("hunk = %+v", h)
	}
	if l := h.Lines[0]; l.Kind != DiffContext || l.Old != 2 || l.New != 2 || l.Text != "b" {
		t.Errorf("first line = %+v", l)
	}
	if l := h.Lines[3]; l.Kind != DiffDel || l.Old != 5 || l.New != 0 || l.Text != "e" {
		t.Errorf("del = %+v", l)
	}
	if l := h.Lines[4]; l.Kind != DiffAdd || l.Old != 0 || l.New != 5 || l.Text != "E" {
		t.Errorf("add = %+v", l)
	}
	if len(out.Content) != 10 || out.Content[4] != "E" || out.Language != "java" || out.DiffLabel != "diff de la feature" {
		t.Errorf("content/language/label = %d %q %q", len(out.Content), out.Language, out.DiffLabel)
	}
}

func TestBuildFileNewAndDeleted(t *testing.T) {
	in := fileIn(planFeature(domain.PhaseRunningBatch))
	in.After = sp("x\ny\n")
	out := BuildFile(in)
	if len(out.Hunks) != 1 || out.Hunks[0].OldStart != 0 || out.Hunks[0].NewStart != 1 {
		t.Fatalf("new file hunks = %+v", out.Hunks)
	}
	if l := out.Hunks[0].Lines[1]; l.Kind != DiffAdd || l.Old != 0 || l.New != 2 {
		t.Errorf("new line = %+v", l)
	}
	in.Before, in.After = sp("x\ny\n"), nil
	out = BuildFile(in)
	if len(out.Hunks) != 1 || out.Hunks[0].NewStart != 0 || out.Hunks[0].OldStart != 1 || len(out.Content) != 0 {
		t.Fatalf("deleted file = %+v content %v", out.Hunks, out.Content)
	}
	if l := out.Hunks[0].Lines[0]; l.Kind != DiffDel || l.Old != 1 || l.New != 0 {
		t.Errorf("del line = %+v", l)
	}
}

func TestBuildFileCRLFIsNotADiff(t *testing.T) {
	in := fileIn(planFeature(domain.PhaseRunningBatch))
	in.Before, in.After = sp("a\nb\n"), sp("a\r\nb\r\n")
	out := BuildFile(in)
	if len(out.Hunks) != 0 || out.Hunks == nil {
		t.Errorf("hunks = %+v", out.Hunks)
	}
	if len(out.Content) != 2 || out.Content[0] != "a" {
		t.Errorf("content = %q", out.Content)
	}
}

func TestBuildFileBinaryHasNoText(t *testing.T) {
	in := fileIn(planFeature(domain.PhaseRunningBatch))
	in.Before, in.After, in.Binary = sp("a\n"), sp("b\n"), true
	out := BuildFile(in)
	if len(out.Hunks) != 0 || len(out.Content) != 0 {
		t.Errorf("binary: %+v %v", out.Hunks, out.Content)
	}
}

func TestBuildFileEditedBy(t *testing.T) {
	f := planFeature(domain.PhaseRunningBatch)
	f.Plan.Batches = []domain.Batch{
		{N: 1, Change: []domain.Change{{At: "src/Nota.java:3", What: "x"}}},
		{N: 2, Change: []domain.Change{{At: "src/Other.java:3", What: "x"}}},
		{N: 3, Change: []domain.Change{{At: "src/Nota.java:9", What: "x"}}},
	}
	if got := BuildFile(fileIn(f)).EditedBy; got != "lo editan los lotes 1 y 3" {
		t.Errorf("edited by = %q", got)
	}
	f.Plan.Batches = f.Plan.Batches[:2]
	if got := BuildFile(fileIn(f)).EditedBy; got != "lo edita el lote 1" {
		t.Errorf("edited by = %q", got)
	}
	f.Plan.Batches = f.Plan.Batches[1:]
	if got := BuildFile(fileIn(f)).EditedBy; got != "" {
		t.Errorf("edited by = %q, want empty", got)
	}
}

func TestBuildFileFailNote(t *testing.T) {
	f := planFeature(domain.PhaseBatchFailed)
	f.State.Steps = []domain.Step{{N: 1, Status: domain.StepFailed}}
	f.Proofs = []domain.Proof{{Batch: 1, Failure: "--- FAIL: TestNota\n    /work/api/src/Nota.java:12: expected 3, got 4\n    src/Other.java:5: nope\n"}}
	f.Affected = &domain.AffectedRun{Failure: "src/Nota.java:30: " + strings.Repeat("x", 300)}
	out := BuildFile(fileIn(f))
	if len(out.Notes) != 2 {
		t.Fatalf("notes = %+v", out.Notes)
	}
	if n := out.Notes[0]; n.Line != 12 || n.Kind != "fail" || n.Text != "/work/api/src/Nota.java:12: expected 3, got 4" {
		t.Errorf("note 0 = %+v", n)
	}
	if n := out.Notes[1]; n.Line != 30 || len([]rune(n.Text)) != 200 {
		t.Errorf("note 1 = line %d len %d", n.Line, len([]rune(n.Text)))
	}
}

func TestBuildFileRulesNeedPaths(t *testing.T) {
	in := fileIn(planFeature(domain.PhaseRunningBatch))
	in.Rules = []memory.Rule{
		{Short: "K1", Text: "area", Paths: []string{"src/**"}},
		{Short: "K2", Text: "general"},
		{Meta: memory.Meta{ID: "01X"}, Text: "other area", Paths: []string{"web/**"}},
	}
	in.Learnings = []memory.Learning{{Short: "A1", Text: "learnt"}}
	out := BuildFile(in)
	if len(out.Rules) != 1 || out.Rules[0].ID != "K1" || out.Rules[0].Kind != "rule" {
		t.Errorf("rules = %+v", out.Rules)
	}
	if len(out.Learnings) != 1 || out.Learnings[0].Kind != "learning" {
		t.Errorf("learnings = %+v", out.Learnings)
	}
	if out.Tests == nil || out.ImportedBy == nil || out.Features == nil || out.Notes == nil {
		t.Error("lists must not be nil")
	}
}

func TestBuildFileFeatures(t *testing.T) {
	cur := planFeature(domain.PhaseRunningBatch)
	cur.Plan.Batches = []domain.Batch{{N: 2, Change: []domain.Change{{At: "src/Nota.java:5", What: "x"}}}}
	cur.State.Steps = []domain.Step{{N: 2, Status: domain.StepFailed}}
	other := planFeature(domain.PhaseRunningBatch)
	other.ID = "F2"
	other.Plan.Batches = []domain.Batch{{N: 1, Change: []domain.Change{{At: "src/Nota.java:1", What: "x"}}}}
	other.State.Steps = []domain.Step{{N: 1, Status: domain.StepFailed}}
	in := fileIn(cur)
	in.Active = []domain.Feature{cur, other}
	in.Closed = []memory.ClosedFeature{{Meta: memory.Meta{ID: "C1", CreatedAt: 1700000000000}, Title: "old"}}
	out := BuildFile(in)
	if len(out.Features) != 3 {
		t.Fatalf("features = %+v", out.Features)
	}
	if f := out.Features[0]; f.ID != "F1" || f.State != "failed" || f.Batch != 2 {
		t.Errorf("viewed = %+v", f)
	}
	if f := out.Features[1]; f.ID != "F2" || f.State != "failed" || f.Batch != 1 {
		t.Errorf("active = %+v", f)
	}
	if f := out.Features[2]; f.State != "closed" || f.ClosedAt == "" {
		t.Errorf("closed = %+v", f)
	}
}

func TestBuildFileFeatureStates(t *testing.T) {
	two := func(steps ...domain.Step) domain.Feature {
		f := planFeature(domain.PhaseRunningBatch)
		f.Plan.Batches = []domain.Batch{
			{N: 1, Change: []domain.Change{{At: "src/Nota.java:1", What: "x"}}},
			{N: 2, Change: []domain.Change{{At: "src/Nota.java:5", What: "y"}}},
		}
		f.State.Steps = steps
		return f
	}
	state := func(f domain.Feature) string {
		out := BuildFile(fileIn(f))
		if len(out.Features) != 1 {
			t.Fatalf("features = %+v", out.Features)
		}
		return out.Features[0].State
	}
	if s := state(two(domain.Step{N: 1, Status: domain.StepGreen}, domain.Step{N: 2, Status: domain.StepFailed})); s != "failed" {
		t.Errorf("later batch failed = %q", s)
	}
	if s := state(two(domain.Step{N: 1, Status: domain.StepGreen}, domain.Step{N: 2})); s != "running" {
		t.Errorf("one green = %q", s)
	}
	if s := state(two(domain.Step{N: 1}, domain.Step{N: 2})); s != "pending" {
		t.Errorf("nothing launched = %q", s)
	}
	if s := state(two()); s != "pending" {
		t.Errorf("no steps = %q", s)
	}
}

func TestBuildFileMultiRepoBatchRepo(t *testing.T) {
	f := planFeature(domain.PhaseRunningBatch)
	f.Repos = []domain.FeatureRepo{{Name: "api"}, {Name: "web"}}
	f.Plan.Batches = []domain.Batch{
		{N: 1, Repo: "api", Change: []domain.Change{{At: "src/Nota.java:1", What: "x"}}},
		{N: 2, Repo: "web", Change: []domain.Change{{At: "src/Nota.java:2", What: "y"}}},
		{N: 3, Change: []domain.Change{{At: "src/Nota.java:3", What: "z"}}}, // no repo: the first one
	}
	in := fileIn(f)
	in.Repo = "web"
	if out := BuildFile(in); out.EditedBy != "lo edita el lote 2" {
		t.Errorf("web = %q", out.EditedBy)
	}
	in.Repo = "api"
	if out := BuildFile(in); out.EditedBy != "lo editan los lotes 1 y 3" {
		t.Errorf("api = %q", out.EditedBy)
	}
}
