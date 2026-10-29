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
