package claudecode

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/docs"
	"github.com/mananos/nxy.dev/internal/domain"
)

func TestInstructionDocsSteps(t *testing.T) {
	f := domain.Feature{
		ID:    "f1",
		State: domain.State{Phase: domain.PhaseDocs},
		Next:  domain.Action{Kind: domain.AskDocs},
		Docs:  &domain.Docs{Docs: []domain.DocFile{{Path: "README.md"}, {Path: "docs/a.md"}, {Path: "docs/b.md"}, {Path: "docs/c.md"}}},
	}
	s := Instruction(f, "")
	for _, want := range []string{"AskUserQuestion", docs.QuestionText("f1"), docs.LabelUpdate, docs.LabelLeave, "README.md, docs/a.md, docs/b.md, +1 more", `"multiSelect":false`} {
		if !strings.Contains(s, want) {
			t.Errorf("ask docs misses %q: %s", want, s)
		}
	}
	if st := StopInstruction(f, "", ""); !strings.Contains(st, "Input:") || strings.Contains(st, "Then nothing else") {
		t.Errorf("stop ask docs: %s", st)
	}
	f.Next = domain.Action{Kind: domain.DispatchDocumenter}
	f.State = domain.State{Phase: domain.PhaseDocumenting}
	if s := Instruction(f, ""); !strings.Contains(s, "`nxy:documenter`") || !strings.Contains(s, "feature f1 docs") || !strings.Contains(s, "nxy writes its prompt") {
		t.Errorf("documenter: %s", s)
	}
}
