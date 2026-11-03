package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/memory"
)

// deliverMemory is a FlowMemory that serves fixed rules and learnings and records what the flow does.
type deliverMemory struct {
	rules      []memory.Rule
	learnings  []memory.Learning
	delivered  []string
	obsolete   []string
	relatedFor []string
}

func (m *deliverMemory) Rules(context.Context, string) ([]memory.Rule, error) { return m.rules, nil }
func (m *deliverMemory) Related(_ context.Context, _ string, paths []string) ([]memory.ClosedFeature, []memory.Learning, error) {
	m.relatedFor = paths
	return nil, m.learnings, nil
}
func (m *deliverMemory) AddLearning(context.Context, memory.Learning) (memory.Learning, error) {
	return memory.Learning{}, nil
}
func (m *deliverMemory) AddRule(context.Context, memory.Rule) (memory.Rule, error) {
	return memory.Rule{}, nil
}
func (m *deliverMemory) SaveClosedFeature(context.Context, memory.ClosedFeature) (memory.ClosedFeature, error) {
	return memory.ClosedFeature{}, nil
}
func (m *deliverMemory) CountDeliveries(_ context.Context, ids []string) error {
	m.delivered = append(m.delivered, ids...)
	return nil
}
func (m *deliverMemory) Cite(context.Context, string, []string, int64) ([]string, error) {
	return nil, nil
}
func (m *deliverMemory) MarkObsolete(_ context.Context, _ string, refs []string, _ int64) error {
	m.obsolete = append(m.obsolete, refs...)
	return nil
}
func (m *deliverMemory) SimilarRules(context.Context, string, string, int) ([]memory.Rule, error) {
	return nil, nil
}
func (m *deliverMemory) RecordEscape(context.Context, string, string, string, string) error {
	return nil
}
func (m *deliverMemory) ReviewStats(context.Context, string, int64) ([]domain.LensStat, error) {
	return nil, nil
}

type osFiles struct{}

func (osFiles) NativePath(p string) string { return p }
func (osFiles) IsDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
func (osFiles) Exists(p string) bool { _, err := os.Stat(p); return err == nil }

func deliverRule(id, short, text string, paths ...string) memory.Rule {
	return memory.Rule{Meta: memory.Meta{ID: id}, Short: short, Text: text, Paths: paths}
}

func deliverSetup(t *testing.T, mem *deliverMemory) (Hooks, string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "calc.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, st := flowHooks(false)
	h.Exe = "/bin/nxy"
	h.Files = osFiles{}
	if mem != nil {
		h.Memory = mem
	}
	p := validPlan()
	p.Batches[0].Change = []domain.Change{{At: "calc.go:3", What: "x"}}
	id, err := st.CreateFeature(context.Background(), domain.Feature{
		Repo: h.Repos.Key(root), ProjectDir: root, Request: "r", State: runningState, Next: runningNext, PlanRev: 1, Plan: &p,
	})
	if err != nil {
		t.Fatal(err)
	}
	return h, root, id
}

func implementerEv(root string) HookEvent {
	return HookEvent{Name: "PreToolUse", Tool: "Agent", Subagent: roleImplementer, ProjectDir: root, Cwd: root}
}

func TestDeliverImplementerGetsRulesAndCountsThem(t *testing.T) {
	mem := &deliverMemory{
		rules: []memory.Rule{
			deliverRule("U1", "K1", "always wrap errors"),
			deliverRule("U2", "K2", "docs only", "docs/*.md"),
			deliverRule("U3", "K3", "root files", "*.go"),
		},
		learnings: []memory.Learning{{Meta: memory.Meta{ID: "L1"}, Short: "A1", Text: "calc is slow", Files: []string{"calc.go"}}},
	}
	h, root, _ := deliverSetup(t, mem)
	r := h.Handle(context.Background(), implementerEv(root))
	for _, want := range []string{"K1: always wrap errors", "K3: root files", "A1: calc is slow", "learned:"} {
		if !strings.Contains(r.Prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, r.Prompt)
		}
	}
	if strings.Contains(r.Prompt, "K2") {
		t.Error("the rule of another area was delivered")
	}
	if strings.Join(mem.delivered, ",") != "U3,U1" {
		t.Errorf("delivered = %v", mem.delivered)
	}
	if len(mem.relatedFor) != 1 || mem.relatedFor[0] != "calc.go" {
		t.Errorf("related for %v", mem.relatedFor)
	}
}

// A learning whose files are missing in this checkout (another branch may have them) is not
// delivered, but it is not marked obsolete either: no git port can say the base branch lost them.
func TestDeliverSkipsMissingFilesWithoutObsolete(t *testing.T) {
	mem := &deliverMemory{learnings: []memory.Learning{
		{Meta: memory.Meta{ID: "L1"}, Short: "A1", Text: "old trick", Files: []string{"gone.go"}},
		{Meta: memory.Meta{ID: "L2"}, Short: "A2", Text: "live trick", Files: []string{"gone.go", "calc.go"}},
	}}
	h, root, _ := deliverSetup(t, mem)
	r := h.Handle(context.Background(), implementerEv(root))
	if strings.Contains(r.Prompt, "old trick") || !strings.Contains(r.Prompt, "A2: live trick") {
		t.Errorf("prompt = %s", r.Prompt)
	}
	if len(mem.obsolete) != 0 {
		t.Errorf("obsolete = %v", mem.obsolete)
	}
}

func TestDeliverBudgetNamesWhatDidNotFit(t *testing.T) {
	long := strings.Repeat("x", 1500)
	mem := &deliverMemory{rules: []memory.Rule{
		deliverRule("U1", "K1", long), deliverRule("U2", "K2", long), deliverRule("U3", "K3", long),
	}}
	h, root, _ := deliverSetup(t, mem)
	pkt := h.memoryFor(context.Background(), domain.Feature{ProjectDir: root}, roleImplementer, []string{"calc.go"})
	if len(pkt.Text) > memory.DispatchBudget || !strings.Contains(pkt.Text, "not shown (budget)") {
		t.Errorf("len = %d, text tail = %q", len(pkt.Text), pkt.Text[len(pkt.Text)-60:])
	}
	if len(mem.delivered) != 1 {
		t.Errorf("delivered = %v", mem.delivered)
	}
}

func TestDeliverPlannerGetsEveryRule(t *testing.T) {
	mem := &deliverMemory{rules: []memory.Rule{
		deliverRule("U1", "K1", "general"), deliverRule("U2", "K2", "area", "docs/*.md"),
	}}
	h, root, _ := deliverSetup(t, mem)
	pkt := h.memoryFor(context.Background(), domain.Feature{ProjectDir: root}, rolePlanner, nil)
	if !strings.Contains(pkt.Text, "K1: general") || !strings.Contains(pkt.Text, "K2: area") || len(mem.delivered) != 2 {
		t.Errorf("pkt = %+v delivered = %v", pkt, mem.delivered)
	}
	empty := &deliverMemory{}
	h.Memory = empty
	if got := h.memoryFor(context.Background(), domain.Feature{ProjectDir: root}, rolePlanner, nil).Text; got != noRulesYet {
		t.Errorf("empty memory text = %q", got)
	}
}

func TestDeliverNilMemoryChangesNothing(t *testing.T) {
	h, root, _ := deliverSetup(t, nil)
	r := h.Handle(context.Background(), implementerEv(root))
	if r.Prompt == "" || strings.Contains(r.Prompt, "nxy memory") || strings.Contains(r.Prompt, "learned:") {
		t.Errorf("prompt = %s", r.Prompt)
	}
	if got := h.memoryFor(context.Background(), domain.Feature{ProjectDir: root}, rolePlanner, nil); got.Text != "" {
		t.Errorf("packet = %+v", got)
	}
}
