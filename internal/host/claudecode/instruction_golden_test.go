package claudecode

// TestInstructionGolden freezes the text of every question the main thread gets, so a refactor of
// how the asks are built cannot change a byte of what Claude receives. The expected texts live in
// testdata/golden/instructions/asks.json at the repo root ({case/variant: text}).
//
// To regenerate the file from the current code, run the test with NXY_WRITE_GOLDEN=1; without that
// variable the test only compares, and it fails when the file is missing.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

const goldenExe = "/opt/nxy/bin/nxy"

func goldenPlan(questions int, noTests bool) *domain.Plan {
	p := &domain.Plan{
		Title:    "Add the export command",
		Criteria: []domain.Criterion{{ID: "R1", Text: "The export writes a file", Test: domain.TestTDD}},
		Batches: []domain.Batch{{
			N: 1, Title: "Write the exporter", Covers: []string{"R1"},
			Change: []domain.Change{{At: "internal/export/export.go:10", What: "write the rows"}},
			Test:   domain.BatchTest{Kind: domain.BatchTestNew, Name: "TestExport", Accept: "go test ./internal/export/"},
			Risk:   domain.RiskLow,
		}},
		NoTests: noTests,
	}
	for i := 1; i <= questions; i++ {
		n := strconv.Itoa(i)
		p.Questions = append(p.Questions, domain.Question{
			Question: "Which format " + n + "?",
			Header:   "Format " + n,
			Options: []domain.Option{
				{Label: "CSV " + n, Description: "Plain text", Recommended: i == 1},
				{Label: "JSON " + n, Description: "Structured"},
			},
		})
	}
	return p
}

func goldenFeature(kind domain.ActionKind, phase domain.Phase) domain.Feature {
	return domain.Feature{
		ID:      "F1",
		Request: "export the data",
		State:   domain.State{Phase: phase},
		Next:    domain.Action{Kind: kind},
		Plan:    goldenPlan(0, false),
		PlanRev: 1,
	}
}

func goldenFindings(n int) []domain.Finding {
	var out []domain.Finding
	for i := 1; i <= n; i++ {
		sev := domain.SeverityMedium
		if i%3 == 0 {
			sev = domain.SeverityHigh
		}
		out = append(out, domain.Finding{
			ID: "H" + strconv.Itoa(i), Lens: "base", Severity: sev, Cause: domain.CauseChange,
			File: "internal/export/export.go", Line: 10 + i, Title: "finding " + strconv.Itoa(i),
			Scenario: "when it runs", Fix: "change it",
		})
	}
	return out
}

func goldenRules(n int) []domain.RuleCandidate {
	var out []domain.RuleCandidate
	for i := 1; i <= n; i++ {
		c := domain.RuleCandidate{
			ID: "S" + strconv.Itoa(i), Text: "rule " + strconv.Itoa(i), Question: "why " + strconv.Itoa(i), Origin: "design",
		}
		if i == 2 {
			c.Supersedes, c.SupersedesText = "K3", "old rule"
		}
		out = append(out, c)
	}
	return out
}

type goldenCase struct {
	name string
	f    domain.Feature
	// only renders the text of one function instead of the three variants of Instruction.
	only func(domain.Feature) string
}

func goldenCases() []goldenCase {
	var cs []goldenCase
	add := func(name string, f domain.Feature) { cs = append(cs, goldenCase{name: name, f: f}) }

	// AskQuestions: with the Engineer offer, without it, answered ones skipped, all answered.
	f := goldenFeature(domain.AskQuestions, domain.PhaseQuestions)
	f.Plan = goldenPlan(2, false)
	add("questions/offer", f)
	f.Plan = goldenPlan(6, false)
	add("questions/offer-more-than-fit", f)
	f.OfferAnswered = true
	f.Answers = []domain.Answer{{Question: "Which format 1?", Answer: "CSV 1"}}
	add("questions/skip-answered", f)
	f.Mode = domain.ModeEngineer
	f.Plan = goldenPlan(2, false)
	f.Answers = []domain.Answer{{Question: "Which format 1?", Answer: "CSV 1"}, {Question: "Which format 2?", Answer: "CSV 2"}}
	add("questions/all-answered", f)
	f.Answers = nil
	add("questions/engineer-mode", f)
	f.Plan = nil
	add("questions/no-plan", f)

	// AskApproval: with and without the plan.
	add("approval/plan", goldenFeature(domain.AskApproval, domain.PhaseApproval))
	f = goldenFeature(domain.AskApproval, domain.PhaseApproval)
	f.Plan = nil
	add("approval/no-plan", f)
	f = goldenFeature(domain.AskApproval, domain.PhaseApproval)
	f.PlanRev = 3
	f.Mode = domain.ModeEngineer
	add("approval/engineer-rev3", f)

	add("change-note", goldenFeature(domain.AskChangeNote, domain.PhaseApproval))

	// AskClash: one repo; two repos with one busy; two holders.
	clash := func(repos int, holders ...domain.RepoClash) domain.Feature {
		f := goldenFeature(domain.AskClash, domain.PhaseRepoClash)
		f.Clash = holders
		for _, n := range []string{"api", "web"}[:repos] {
			f.Repos = append(f.Repos, domain.FeatureRepo{Name: n})
		}
		return f
	}
	add("clash/one-repo", clash(1, domain.RepoClash{Repo: "api", Checkout: "/w/api", FeatureID: "F0", Phase: "running_batch"}))
	add("clash/two-repos-one-busy", clash(2, domain.RepoClash{Repo: "api", Checkout: "/w/api", FeatureID: "F0", Phase: "running_batch"}))
	add("clash/two-holders", clash(2,
		domain.RepoClash{Repo: "api", Checkout: "/w/api", FeatureID: "F0", Phase: "running_batch"},
		domain.RepoClash{Repo: "web", Checkout: "/w/web", FeatureID: "F2"}))

	// AskBranch: with and without the last error; branch asked, bases per repo.
	branch := func(o *domain.BranchOffer) domain.Feature {
		f := goldenFeature(domain.AskBranch, domain.PhaseBranch)
		f.BranchOffer = o
		return f
	}
	add("branch/suggest", branch(&domain.BranchOffer{
		Suggest: []string{"feature/export", "feature/export-2"},
		Repos:   []domain.BranchRepoOffer{{Name: "api", Current: "main", Bases: []string{"main", "develop"}}},
	}))
	add("branch/error", branch(&domain.BranchOffer{
		Suggest: []string{"feature/export"},
		Repos:   []domain.BranchRepoOffer{{Name: "api", Current: "main", Bases: []string{"main", "develop"}}},
		Error:   "branch feature/export already exists",
	}))
	add("branch/bases-two-repos", branch(&domain.BranchOffer{
		Branch: "feature/export",
		Repos: []domain.BranchRepoOffer{
			{Name: "api", Current: "main", Bases: []string{"main", "develop"}},
			{Name: "web", Current: "develop", Bases: []string{"develop", "main"}},
		},
	}))
	add("branch/no-offer", branch(nil))

	// AskDecision per phase, and an unknown phase.
	decision := func(phase domain.Phase) domain.Feature {
		f := goldenFeature(domain.AskDecision, phase)
		f.State.Batch = 2
		f.State.Batches = 3
		return f
	}
	f = decision(domain.PhaseBatchFailed)
	f.Proofs = []domain.Proof{{Batch: 2, Failure: "FAIL: TestExport\nexpected 1 got 2"}}
	add("decision/batch-failed", f)
	f = decision(domain.PhaseAffectedFailed)
	f.Affected = &domain.AffectedRun{Failure: "FAIL: TestOther", Reason: "a reason", Note: "a note"}
	add("decision/affected-failed", f)
	add("decision/affected-failed-no-run", decision(domain.PhaseAffectedFailed))
	f = decision(domain.PhaseAffectedUnverified)
	f.Affected = &domain.AffectedRun{Reason: "no runner found", Note: "add .nxy/runners.json", Failure: "ignored here"}
	add("decision/affected-unverified", f)
	f = decision(domain.PhaseFixesFailed)
	f.Affected = &domain.AffectedRun{Failure: "FAIL: TestFix", Reason: "still red", Note: "second try"}
	add("decision/fixes-failed", f)
	add("decision/planner-failed", decision(domain.PhasePlannerFailed))
	add("decision/unknown-phase", decision(domain.PhaseDocs))

	// AskFindings: more findings than fit, and few.
	findings := func(n int) domain.Feature {
		f := goldenFeature(domain.AskFindings, domain.PhaseChoosingFindings)
		f.Review = &domain.Review{Risk: domain.RiskMedium, Findings: goldenFindings(n)}
		return f
	}
	add("findings/few", findings(3))
	add("findings/one", findings(1))
	add("findings/left", findings(20))

	// AskDocs.
	f = goldenFeature(domain.AskDocs, domain.PhaseDocs)
	f.Docs = &domain.Docs{Docs: []domain.DocFile{{Path: "README.md"}, {Path: "docs/a.md"}, {Path: "docs/b.md"}, {Path: "docs/c.md"}}}
	add("docs/four", f)
	f.Docs = nil
	add("docs/none", f)

	// AskRules: few and more candidates than fit.
	f = goldenFeature(domain.AskRules, domain.PhaseRules)
	f.RuleOffer = goldenRules(5)
	add("rules/five", f)
	f.RuleOffer = goldenRules(1)
	add("rules/one", f)
	f.RuleOffer = goldenRules(20)
	add("rules/left", f)

	// AskNoTests keeps its current default line.
	add("no-tests", goldenFeature(domain.AskNoTests, domain.PhaseApproval))

	// Design proposal and close.
	f = goldenFeature(domain.DesignDebate, domain.PhaseDesigning)
	f.Mode = domain.ModeEngineer
	f.Design = &domain.Design{
		Decisions: []domain.Decision{{ID: "D1", Scope: domain.ScopeTask, Text: "use CSV"}},
		Pending:   &domain.Decision{Scope: domain.ScopeTask, Text: "write one file per table", Why: "simpler to diff"},
	}
	cs = append(cs, goldenCase{name: "design/proposal", f: f, only: ProposalInstruction})
	f.Design = &domain.Design{
		Decisions: []domain.Decision{{ID: "D1", Scope: domain.ScopeTask, Text: "use CSV"}},
		Close:     &domain.DesignClose{Criteria: []domain.Criterion{{ID: "R1", Text: "The export writes a file", Test: domain.TestTDD}}},
	}
	cs = append(cs, goldenCase{name: "design/close", f: f, only: CloseInstruction})
	return cs
}

func renderGolden() map[string]string {
	out := map[string]string{}
	for _, c := range goldenCases() {
		if c.only != nil {
			out[c.name] = c.only(c.f)
			continue
		}
		out[c.name+"/instruction"] = Instruction(c.f, "")
		out[c.name+"/instruction-note"] = Instruction(c.f, "a note")
		out[c.name+"/stop"] = StopInstruction(c.f, "", goldenExe)
		out[c.name+"/for"] = InstructionFor(c.f, "", goldenExe)
	}
	return out
}

func TestInstructionGolden(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "golden", "instructions", "asks.json")
	got := renderGolden()

	if os.Getenv("NXY_WRITE_GOLDEN") == "1" {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(got); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden file missing (%v): run with NXY_WRITE_GOLDEN=1 to create it", err)
	}
	var want map[string]string
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("case %s is in the golden but no longer rendered", name)
			continue
		}
		if g != w {
			t.Errorf("case %s changed:\n--- want ---\n%s\n--- got ---\n%s", name, w, g)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("case %s has no golden", name)
		}
	}
	if len(want) == 0 {
		t.Error("golden has no cases")
	}
}
