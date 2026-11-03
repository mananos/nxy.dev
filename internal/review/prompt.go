package review

import (
	_ "embed"
	"strings"
	"text/template"

	"github.com/mananos/nxy.dev/internal/domain"
)

//go:embed prompts/reviewer.md
var reviewerTemplate string

var reviewerTmpl = template.Must(template.New("reviewer").Parse(reviewerTemplate))

//go:embed prompts/refuter.md
var refuterTemplate string

var refuterTmpl = template.Must(template.New("refuter").Parse(refuterTemplate))

//go:embed prompts/fix.md
var fixTemplate string

var fixTmpl = template.Must(template.New("fix").Parse(fixTemplate))

const reviewerExample = `{
  "lens": "api",
  "findings": [
    {
      "severity": "high",
      "file": "internal/greet/greet.go",
      "line": 14,
      "title": "Empty name panics",
      "scenario": "greet(\"\") -> index out of range at line 14",
      "evidence": "name[0] is read before the length check",
      "fix": "check len(name) before reading name[0]"
    }
  ]
}`

const reviewerBaseExample = `{
  "lens": "base",
  "criteria": [
    {"id": "R1", "ok": true},
    {"id": "R2", "ok": false, "why": "the README does not mention the greeting", "file": "README.md", "line": 30}
  ],
  "findings": []
}`

const reviewerEngineerExample = `{
  "lens": "api",
  "findings": [
    {
      "severity": "high",
      "file": "internal/greet/greet.go",
      "line": 14,
      "title": "Empty name panics",
      "scenario": "greet(\"\") -> index out of range at line 14",
      "evidence": "name[0] is read before the length check",
      "fix": "check len(name) before reading name[0]",
      "why": "a caller that passes user input crashes the whole request",
      "lesson": "validate the length of a value before indexing it"
    }
  ]
}`

const reviewerEngineerBaseExample = `{
  "lens": "base",
  "criteria": [
    {"id": "R1", "ok": true}
  ],
  "decisions": [
    {"id": "D1", "file": "internal/greet/greet.go", "line": 12, "how": "the greeting is built here"},
    {"id": "D2", "how": "nothing in the diff: it only constrains the next step"}
  ],
  "findings": []
}`

const refuterExample = `{
  "verdicts": [
    {"id": "H1", "verdict": "confirmed", "why": "greet(\"\") reaches line 14 with no check before it"},
    {"id": "H2", "verdict": "refuted", "why": "the caller rejects empty input at handler.go:40"}
  ]
}`

type reviewerData struct {
	Feature     domain.Feature
	Exe         string
	Lens        Lens
	Criteria    []domain.Criterion
	Conventions []string
	Mem         string
	Files       []string
	Diffs       []FileDiff
	Cut         []string
	Example     string
	Engineer    bool
	Decisions   []domain.Decision
}

// ReviewerPrompt builds the prompt of the reviewer of one lens. Only the base
// lens gets the criteria of the plan. The diff is cut to MaxDiffLines; what does
// not fit is listed for the reviewer to read on its own. mem is the memory packet of the diff
// (repo rules and learnings; empty prints no section).
func ReviewerPrompt(f domain.Feature, lens Lens, diffs []FileDiff, exe, mem string) string {
	d := reviewerData{
		Mem:     mem,
		Feature: f,
		Exe:     strings.ReplaceAll(exe, `\`, "/"),
		Lens:    lens,
		Example: reviewerExample,
	}
	if f.Plan != nil {
		for _, dc := range f.Plan.Decisions {
			if dc.Scope == domain.ScopeRepo {
				d.Conventions = append(d.Conventions, dc.Text)
			}
		}
		if lens.Name == BaseLens {
			d.Criteria = f.Plan.Criteria
			d.Example = reviewerBaseExample
		}
	}
	if f.Mode == domain.ModeEngineer {
		d.Engineer = true
		d.Example = reviewerEngineerExample
		if lens.Name == BaseLens {
			d.Decisions = DesignDecisions(f)
			if len(d.Decisions) > 0 {
				d.Example = reviewerEngineerBaseExample
			} else {
				d.Example = reviewerBaseExample
			}
		}
	}
	d.Diffs, d.Cut = Cut(diffs, MaxDiffLines)
	for _, fd := range diffs {
		d.Files = append(d.Files, fd.Path)
	}
	var b strings.Builder
	if err := reviewerTmpl.Execute(&b, d); err != nil {
		return "nxy: could not build the reviewer prompt: " + err.Error()
	}
	return b.String()
}

type refuterData struct {
	Feature  domain.Feature
	Exe      string
	Findings []domain.Finding
	Example  string
}

// RefuterPrompt builds the prompt of the refuter: the offered high findings
// that still have no verdict.
func RefuterPrompt(f domain.Feature, exe string) string {
	d := refuterData{Feature: f, Exe: strings.ReplaceAll(exe, `\`, "/"), Example: refuterExample}
	if f.Review != nil {
		for _, fd := range f.Review.Findings {
			if needsVerdict(fd) {
				d.Findings = append(d.Findings, fd)
			}
		}
	}
	var b strings.Builder
	if err := refuterTmpl.Execute(&b, d); err != nil {
		return "nxy: could not build the refuter prompt: " + err.Error()
	}
	return b.String()
}

type fixData struct {
	Feature   domain.Feature
	Exe       string
	Decisions []domain.Decision
	Findings  []domain.Finding
	Failure   string
	Mem       string
}

// FixPrompt builds the prompt of the single implementer that fixes every chosen
// finding. On a retry (an attempt already used) it carries what failed. mem is the memory
// packet (empty prints no section).
func FixPrompt(f domain.Feature, exe, mem string) string {
	d := fixData{Feature: f, Exe: strings.ReplaceAll(exe, `\`, "/"), Mem: mem}
	if f.Plan != nil {
		d.Decisions = f.Plan.Decisions
	}
	if f.Review != nil {
		chosen := map[string]bool{}
		for _, id := range f.Review.Chosen {
			chosen[id] = true
		}
		for _, fd := range f.Review.Findings {
			if chosen[fd.ID] {
				d.Findings = append(d.Findings, fd)
			}
		}
	}
	if f.State.Attempt > 0 && f.Affected != nil {
		d.Failure = f.Affected.Failure
	}
	var b strings.Builder
	if err := fixTmpl.Execute(&b, d); err != nil {
		return "nxy: could not build the fix prompt: " + err.Error()
	}
	return b.String()
}
