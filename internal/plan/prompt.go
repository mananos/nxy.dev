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
	Mem       string
}

// AffectedFixPrompt builds the prompt of the implementer that fixes the
// failing affected tests of the feature. mem is the memory packet (empty prints no section).
func AffectedFixPrompt(f domain.Feature, exe, mem string) string {
	d := affectedFixData{Feature: f, Exe: strings.ReplaceAll(exe, `\`, "/"), Mem: mem}
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
  "criteria": [
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

//go:embed prompts/planner-engineer.md
var plannerEngineerTemplate string

var plannerEngineerTmpl = template.Must(template.New("planner-engineer").Parse(plannerEngineerTemplate))

// engineerExample is the example plan of the engineer prompt: no questions, no decisions of its own.
const engineerExample = `{
  "title": "Greet by name",
  "criteria": [
    {"id": "R1", "text": "greet(name) returns \"Hello, <name>\"", "test": "tdd"}
  ],
  "batches": [
    {
      "n": 1,
      "title": "Greeting function",
      "covers": ["R1"],
      "read": ["internal/greet/greet.go:1-40"],
      "change": [{"at": "internal/greet/greet.go:12", "what": "add greet(name) returning Hello, name"}],
      "test": {"kind": "new", "name": "TestGreet", "accept": "go test ./internal/greet/"},
      "risk": "low"
    }
  ],
  "suite": "go test ./...",
  "risks": ["Callers of the old signature"]
}`

type plannerData struct {
	Feature     domain.Feature
	Exe         string
	Example     string
	PrevJSON    string
	NotesJoined []string
	Design      *domain.Design
	Delegated   []domain.Fork
	Mem         string
}

// PlannerPrompt builds the full prompt of the planner: the literal request,
// what the user answered, the previous plan if any, and how to save. An engineer feature gets
// the design instead of the conversation. mem is the memory packet (empty prints no section).
func PlannerPrompt(f domain.Feature, exe, mem string) string {
	engineer := f.Mode == domain.ModeEngineer
	d := plannerData{
		Feature:     f,
		Mem:         mem,
		Exe:         strings.ReplaceAll(exe, `\`, "/"),
		Example:     plannerExample,
		NotesJoined: f.ChangeNotes,
	}
	tmpl := plannerTmpl
	if engineer {
		tmpl = plannerEngineerTmpl
		d.Example = engineerExample
		d.Design = f.Design
		if f.Design != nil {
			for _, fk := range f.Design.Agenda {
				if fk.Status == domain.ForkDelegated {
					d.Delegated = append(d.Delegated, fk)
				}
			}
		}
	}
	if f.Plan != nil {
		prev := *f.Plan
		if engineer {
			// nxy owns the design's decisions in the plan; the planner only sees (and revises) its own.
			prev.Decisions = nil
			for _, x := range f.Plan.Decisions {
				if x.By == domain.ByPlanner {
					x.ID, x.By, x.Dissent = "", "", ""
					prev.Decisions = append(prev.Decisions, x)
				}
				// criteria are the approved ones and there are no questions in engineer.
				prev.Questions = nil
				prev.Criteria = nil
				if f.Design != nil {
					prev.Criteria = f.Design.Criteria
				}
			}
		}
		if b, err := json.MarshalIndent(prev, "", "  "); err == nil {
			d.PrevJSON = string(b)
		}
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, d); err != nil {
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
	Criteria  []domain.Criterion
	Accept    string
	Need      Need
	Failure   string
	RedDone   bool
	Mem       string
}

// ImplementerPrompt builds the full prompt of the implementer for the batch
// the feature is on (f.Next.Batch). mem is the memory packet (empty prints no section).
func ImplementerPrompt(f domain.Feature, exe, mem string) string {
	if f.Plan == nil || f.Next.Batch < 1 || f.Next.Batch > len(f.Plan.Batches) {
		return "nxy: could not build the implementer prompt: no batch to implement"
	}
	n := f.Next.Batch
	p := *f.Plan
	d := implementerData{
		Feature:   f,
		Mem:       mem,
		Exe:       strings.ReplaceAll(exe, `\`, "/"),
		Batch:     p.Batches[n-1],
		Total:     len(p.Batches),
		Decisions: p.Decisions,
		Accept:    p.Batches[n-1].Test.Accept,
		Need:      NeedOf(p, n),
	}
	for _, id := range d.Batch.Covers {
		for _, r := range p.Criteria {
			if r.ID == id {
				d.Criteria = append(d.Criteria, r)
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
