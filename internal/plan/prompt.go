package plan

import (
	_ "embed"
	"encoding/json"
	"strings"
	"text/template"

	"github.com/mananos/nxy.dev/internal/domain"
)

//go:embed prompts/planner.md
var plannerTemplate string

var plannerTmpl = template.Must(template.New("planner").Parse(plannerTemplate))

//go:embed prompts/implementer.md
var implementerTemplate string

var implementerTmpl = template.Must(template.New("implementer").Parse(implementerTemplate))

//go:embed prompts/affectedfix.md
var affectedFixTemplate string

var affectedFixTmpl = template.Must(template.New("affectedfix").Parse(affectedFixTemplate))

type affectedFixData struct {
	Feature   domain.Feature
	Exe       string
	Decisions []domain.Decision
	Failure   string
}

// AffectedFixPrompt builds the prompt of the implementer that fixes the
// failing affected tests of the feature.
func AffectedFixPrompt(f domain.Feature, exe string) string {
	d := affectedFixData{Feature: f, Exe: strings.ReplaceAll(exe, `\`, "/")}
	if f.Plan != nil {
		d.Decisions = f.Plan.Decisions
	}
	if f.Affected != nil {
		d.Failure = f.Affected.Failure
	}
	var b strings.Builder
	if err := affectedFixTmpl.Execute(&b, d); err != nil {
		return "nxy: could not build the affected fix prompt: " + err.Error()
	}
	return b.String()
}

// plannerExample is the example plan of the prompt; the tests check it is valid.
const plannerExample = `{
  "title": "Greet by name",
  "decisions": [{"scope": "task", "text": "Greeting stays English", "question": "Which language?"}],
  "rules": [
    {"id": "R1", "text": "greet(name) returns \"Hello, <name>\"", "test": "tdd"},
    {"id": "R2", "text": "The README shows the new greeting", "test": "manual", "check": "README usage section"}
  ],
  "batches": [
    {
      "n": 1,
      "title": "Greeting function",
      "covers": ["R1", "R2"],
      "read": ["internal/greet/greet.go:1-40"],
      "change": [
        {"at": "internal/greet/greet.go:12", "what": "add greet(name) returning Hello, name"},
        {"at": "README.md:30", "what": "document the greeting"}
      ],
      "test": {"kind": "new", "name": "TestGreet", "accept": "go test ./internal/greet/"},
      "risk": "low"
    }
  ],
  "suite": "go test ./...",
  "risks": ["Callers of the old signature"],
  "questions": [
    {
      "question": "Where should the greeting live?",
      "header": "Location",
      "options": [
        {"label": "internal/greet", "description": "New package", "recommended": true},
        {"label": "cmd/nxy", "description": "Next to main"}
      ]
    }
  ]
}`

type plannerData struct {
	Feature     domain.Feature
	Exe         string
	Example     string
	PrevJSON    string
	NotesJoined []string
}

// PlannerPrompt builds the full prompt of the planner: the literal request,
// what the user answered, the previous plan if any, and how to save.
func PlannerPrompt(f domain.Feature, exe string) string {
	d := plannerData{
		Feature:     f,
		Exe:         strings.ReplaceAll(exe, `\`, "/"),
		Example:     plannerExample,
		NotesJoined: f.ChangeNotes,
	}
	if f.Plan != nil {
		if b, err := json.MarshalIndent(f.Plan, "", "  "); err == nil {
			d.PrevJSON = string(b)
		}
	}
	var b strings.Builder
	if err := plannerTmpl.Execute(&b, d); err != nil {
		return "nxy: could not build the planner prompt: " + err.Error()
	}
	return b.String()
}

type implementerData struct {
	Feature   domain.Feature
	Exe       string
	Batch     domain.Batch
	Total     int
	Decisions []domain.Decision
	Rules     []domain.Rule
	Accept    string
	Need      Need
	Failure   string
	RedDone   bool
}

// ImplementerPrompt builds the full prompt of the implementer for the batch
// the feature is on (f.Next.Batch).
func ImplementerPrompt(f domain.Feature, exe string) string {
	if f.Plan == nil || f.Next.Batch < 1 || f.Next.Batch > len(f.Plan.Batches) {
		return "nxy: could not build the implementer prompt: no batch to implement"
	}
	n := f.Next.Batch
	p := *f.Plan
	d := implementerData{
		Feature:   f,
		Exe:       strings.ReplaceAll(exe, `\`, "/"),
		Batch:     p.Batches[n-1],
		Total:     len(p.Batches),
		Decisions: p.Decisions,
		Accept:    p.Batches[n-1].Test.Accept,
		Need:      NeedOf(p, n),
	}
	for _, id := range d.Batch.Covers {
		for _, r := range p.Rules {
			if r.ID == id {
				d.Rules = append(d.Rules, r)
			}
		}
	}
	if f.Proof != nil && f.Proof.Batch == n {
		d.Failure = f.Proof.Failure
		d.RedDone = f.Proof.Red != nil && f.Proof.Red.Exit != 0
	}
	var b strings.Builder
	if err := implementerTmpl.Execute(&b, d); err != nil {
		return "nxy: could not build the implementer prompt: " + err.Error()
	}
	return b.String()
}
