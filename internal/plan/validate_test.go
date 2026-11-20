package plan

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

func valid() domain.Plan {
	return domain.Plan{
		Title: "Add thing",
		Criteria: []domain.Criterion{
			{ID: "R1", Text: "does it", Test: domain.TestTDD},
			{ID: "R2", Text: "looks ok", Test: domain.TestManual, Check: "the button"},
		},
		Batches: []domain.Batch{
			{N: 1, Title: "core", Covers: []string{"R1"},
				Change: []domain.Change{{At: "a/b.go:10", What: "add it"}},
				Test:   domain.BatchTest{Kind: domain.BatchTestNew, Accept: "go test ./a"},
				Risk:   domain.RiskLow},
			{N: 2, Title: "ui", Covers: []string{"R2"},
				Change:  []domain.Change{{At: "ui.go (new)", What: "button"}},
				Test:    domain.BatchTest{Kind: domain.BatchTestExisting},
				Risk:    domain.RiskMedium,
				Depends: []int{1}},
		},
	}
}

func TestValidate(t *testing.T) {
	if ps := Validate(valid()); len(ps) != 0 {
		t.Fatalf("valid plan has problems: %v", ps)
	}
	q := func(opts ...domain.Option) []domain.Question {
		return []domain.Question{{Question: "which?", Header: "h", Options: opts}}
	}
	rec := domain.Option{Label: "a", Description: "x", Recommended: true}
	oth := domain.Option{Label: "b", Description: "y"}

	cases := []struct {
		name string
		mut  func(*domain.Plan)
		want string
	}{
		{"empty title", func(p *domain.Plan) { p.Title = "" }, "title: is empty"},
		{"no criteria", func(p *domain.Plan) { p.Criteria = nil; p.Batches = nil }, "criteria: need at least one criterion"},
		{"bad id", func(p *domain.Plan) { p.Criteria[0].ID = "X1" }, "X1: id must look like R1, R2, ..."},
		{"id order", func(p *domain.Plan) { p.Criteria[1].ID = "R3"; p.Batches[1].Covers = []string{"R3"} }, "R3: ids must be R1..Rn in order; expected R2"},
		{"dup id", func(p *domain.Plan) { p.Criteria[1].ID = "R1" }, "R1: duplicate criterion id"},
		{"bad test kind", func(p *domain.Plan) { p.Criteria[0].Test = "x" }, `R1: test "x" must be tdd, existing, build or manual`},
		{"manual no check", func(p *domain.Plan) { p.Criteria[1].Check = "" }, "R2: manual criterion needs check (what to look at)"},
		{"uncovered", func(p *domain.Plan) { p.Batches[0].Covers = nil }, "R1: no batch covers it"},
		{"covers unknown", func(p *domain.Plan) { p.Batches[0].Covers = []string{"R1", "R9"} }, "batch 1: covers R9, which is not a criterion"},
		{"numbering", func(p *domain.Plan) { p.Batches[1].N = 5 }, "batch 2: n is 5; batches must be numbered 1..n in order"},
		{"no change", func(p *domain.Plan) { p.Batches[0].Change = nil }, "batch 1: needs at least one change"},
		{"bad at", func(p *domain.Plan) { p.Batches[0].Change[0].At = "a/b.go" }, `batch 1 change 1: at "a/b.go" must be path:line, path:a-b or path (new)`},
		{"empty what", func(p *domain.Plan) { p.Batches[0].Change[0].What = "" }, "batch 1 change 1: what is empty"},
		{"long what", func(p *domain.Plan) { p.Batches[0].Change[0].What = strings.Repeat("x", 201) }, "batch 1 change 1: what has 201 characters; keep it to 200"},
		{"no accept", func(p *domain.Plan) { p.Batches[0].Test.Accept = "" }, "batch 1: test.accept is required unless the batch only covers manual criteria"},
		{"bad test kind batch", func(p *domain.Plan) { p.Batches[0].Test.Kind = "" }, `batch 1: test kind "" must be new or existing`},
		{"bad risk", func(p *domain.Plan) { p.Batches[0].Risk = "huge" }, `batch 1: risk "huge" must be low, medium or high`},
		{"depends forward", func(p *domain.Plan) { p.Batches[0].Depends = []int{2} }, "batch 1: depends on batch 2; only earlier batches are allowed"},
		{"bad scope", func(p *domain.Plan) { p.Decisions = []domain.Decision{{Scope: "x", Text: "t"}} }, `decision 1: scope "x" must be repo or task`},
		{"too many questions", func(p *domain.Plan) {
			p.Questions = append(q(rec, oth), append(q(rec, oth), append(q(rec, oth), append(q(rec, oth), q(rec, oth)...)...)...)...)
		}, "questions: 5 questions; ask at most 4"},
		{"one option", func(p *domain.Plan) { p.Questions = q(rec) }, "question 1: has 1 options; it needs 2 to 4"},
		{"no recommended", func(p *domain.Plan) { p.Questions = q(oth, oth) }, "question 1: needs exactly one recommended option; has 0"},
		{"two recommended", func(p *domain.Plan) { p.Questions = q(rec, rec) }, "question 1: needs exactly one recommended option; has 2"},
		{"recommended not first", func(p *domain.Plan) { p.Questions = q(oth, rec) }, "question 1: the recommended option must be the first"},
		{"backtick", func(p *domain.Plan) { p.Criteria[0].Text = "a ``` b" }, "R1 text: must not contain a triple backtick"},
		{"newline", func(p *domain.Plan) { p.Title = "a\nb" }, "title: must be one line"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := valid()
			tc.mut(&p)
			var got []string
			for _, pr := range Validate(p) {
				got = append(got, pr.String())
			}
			for _, g := range got {
				if g == tc.want {
					return
				}
			}
			t.Fatalf("want %q in %q", tc.want, got)
		})
	}
}

func TestValidateManualOnlyNeedsNoAccept(t *testing.T) {
	p := valid()
	if p.Batches[1].Test.Accept != "" {
		t.Fatal("fixture changed")
	}
	if ps := Validate(p); len(ps) != 0 {
		t.Fatalf("manual-only batch should not need accept: %v", ps)
	}
}

func TestDecode(t *testing.T) {
	if _, err := Decode(strings.NewReader(`{"title":"x","rulez":[]}`)); err == nil || !strings.Contains(err.Error(), "rulez") {
		t.Fatalf("want error naming rulez, got %v", err)
	}
	if _, err := Decode(strings.NewReader(`{"title":"x"} {"title":"y"}`)); err == nil {
		t.Fatal("want error for extra content")
	}
	p, err := Decode(strings.NewReader(`{"title":"x"}`))
	if err != nil || p.Title != "x" {
		t.Fatalf("got %v %v", p, err)
	}
}
