package app

import (
	"context"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/memory"
)

func memRules() *deliverMemory {
	return &deliverMemory{rules: []memory.Rule{
		deliverRule("U1", "K1", "always wrap errors"),
		deliverRule("U2", "K2", "docs only", "docs/*.md"),
	}}
}

func TestEngineerMemoryNoteHasRulesAndCountsDelivery(t *testing.T) {
	h, _ := flowHooks(false)
	mem := memRules()
	h.Memory = mem
	r := h.Handle(context.Background(), prompt("/nxy:engineer add a cache"))
	if r.Next == nil {
		t.Fatalf("no next: %+v", r)
	}
	for _, want := range []string{"K1: always wrap errors", "K2: docs only", "mem related"} {
		if !strings.Contains(r.Next.Note, want) {
			t.Errorf("note lacks %q: %q", want, r.Next.Note)
		}
	}
	if len(mem.delivered) != 2 {
		t.Errorf("delivered = %v, want 2 rules", mem.delivered)
	}
}

func TestEngineerMemoryNoteHasMainGlobalNotPlanner(t *testing.T) {
	h, _ := flowHooks(false)
	mem := memRules()
	mem.globals = []memory.Rule{globalRule("G3", "G3", "main only", []string{memory.RoleMain})}
	h.Memory = mem
	r := h.Handle(context.Background(), prompt("/nxy:engineer add a cache"))
	if r.Next == nil || !strings.Contains(r.Next.Note, "G3: main only") {
		t.Fatalf("note = %+v", r.Next)
	}
	pl := h.memoryFor(context.Background(), domain.Feature{ProjectDir: t.TempDir()}, rolePlanner, nil).Text
	if strings.Contains(pl, "G3") {
		t.Errorf("planner prompt has a main-only rule: %q", pl)
	}
}

func TestEngineerMemoryDelegateHasNoNote(t *testing.T) {
	h, _ := flowHooks(false)
	mem := memRules()
	h.Memory = mem
	r := h.Handle(context.Background(), prompt("/nxy:feature add a cache"))
	if r.Next == nil || r.Next.Note != "" {
		t.Errorf("delegate note = %+v", r.Next)
	}
	if len(mem.delivered) != 0 {
		t.Errorf("delivered = %v", mem.delivered)
	}
}

func TestEngineerMemoryWithoutRulesOrMemoryHasNoNote(t *testing.T) {
	h, _ := flowHooks(false)
	h.Memory = &deliverMemory{}
	if r := h.Handle(context.Background(), prompt("/nxy:engineer a")); r.Next == nil || r.Next.Note != "" {
		t.Errorf("no rules: %+v", r.Next)
	}
	h2, _ := flowHooks(false)
	if r := h2.Handle(context.Background(), prompt("/nxy:engineer a")); r.Next == nil || r.Next.Note != "" {
		t.Errorf("no memory: %+v", r.Next)
	}
}

func TestRuleHintOnlyWithActiveFeatureAndWord(t *testing.T) {
	h, _ := flowHooks(false)
	mem := memRules()
	h.Memory = mem
	ctx := context.Background()

	if r := h.Handle(ctx, prompt("siempre usá tabs")); r.Context != "" {
		t.Errorf("hint without feature: %q", r.Context)
	}
	h.Handle(ctx, prompt("/nxy:feature add a cache"))
	if r := h.Handle(ctx, prompt("hacelo simple")); r.Context != "" {
		t.Errorf("hint without word: %q", r.Context)
	}
	r := h.Handle(ctx, prompt("de ahora en adelante usá tabs"))
	if !strings.Contains(r.Context, "repo rule") || !strings.Contains(r.Context, "nxy feature rule") {
		t.Errorf("hint = %q", r.Context)
	}
}

func TestRuleHintNeverWritesMemory(t *testing.T) {
	h, _ := flowHooks(false)
	mem := &writeSpyMemory{deliverMemory: memRules()}
	h.Memory = mem
	ctx := context.Background()
	h.Handle(ctx, prompt("/nxy:feature add a cache"))
	h.Handle(ctx, prompt("always use tabs"))
	if mem.writes != 0 {
		t.Errorf("memory written %d times", mem.writes)
	}
}

type writeSpyMemory struct {
	*deliverMemory
	writes int
}

func (m *writeSpyMemory) AddRule(ctx context.Context, r memory.Rule) (memory.Rule, error) {
	m.writes++
	return m.deliverMemory.AddRule(ctx, r)
}
func (m *writeSpyMemory) AddLearning(ctx context.Context, l memory.Learning) (memory.Learning, error) {
	m.writes++
	return m.deliverMemory.AddLearning(ctx, l)
}
