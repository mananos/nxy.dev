package views

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mananos/nxy.dev/internal/memory"
)

func TestBuildMemoryUncited(t *testing.T) {
	for _, tc := range []struct {
		deliveries, citations int
		flag                  string
	}{{9, 0, ""}, {10, 0, FlagUncited}, {40, 1, ""}} {
		m := BuildMemory(MemoryIn{Scope: ScopeRepo, Rules: []memory.Rule{{Short: "K1", Origin: memory.OriginChat, Deliveries: tc.deliveries, Citations: tc.citations}}})
		if got := m.Rules[0].Flag; got != tc.flag {
			t.Errorf("%d/%d: flag %q, want %q", tc.deliveries, tc.citations, got, tc.flag)
		}
		if (len(m.Proposals) == 1) != (tc.flag == FlagUncited) {
			t.Errorf("%d/%d: proposals %d", tc.deliveries, tc.citations, len(m.Proposals))
		}
	}
}

func TestBuildMemoryRulesOrderOriginAndActions(t *testing.T) {
	m := BuildMemory(MemoryIn{Scope: ScopeRepo, Rules: []memory.Rule{
		{Short: "K1", Origin: memory.OriginPlan, FeatureID: "F7K2", Citations: 2, LastCitedAt: 1762603200000},
		{Short: "K2", Origin: memory.OriginManual, Deliveries: 12},
		{Short: "K3", Origin: memory.OriginBootstrap},
	}})
	if m.Rules[0].ID != "K2" {
		t.Fatalf("the odd rule goes first: %v", m.Rules[0].ID)
	}
	byID := map[string]Rule{}
	for _, r := range m.Rules {
		byID[r.ID] = r
	}
	if r := byID["K1"]; r.Origin != "closing" || r.OriginRef != "F7K2" || r.LastCited != "2025-11-08T12:00:00Z" {
		t.Errorf("K1 %+v", r)
	}
	if byID["K2"].Origin != "chat" || byID["K3"].Origin != "bootstrap" || byID["K3"].OriginRef != "" {
		t.Errorf("origins %+v %+v", byID["K2"], byID["K3"])
	}
	want := []string{"nxy", "mem", "rm", "K2"}
	if a := byID["K2"].Actions[1]; a.ID != "delete" || a.Planned || !reflect.DeepEqual(a.Argv, want) {
		t.Errorf("delete %+v", a)
	}
	if a := byID["K2"].Actions[2]; a.ID != "restore" || a.Planned || !reflect.DeepEqual(a.Argv, []string{"nxy", "mem", "restore", "K2"}) {
		t.Errorf("restore %+v", a)
	}
	if m.Create.Planned || m.Create.Argv[2] != "rule" {
		t.Errorf("create %+v", m.Create)
	}
}

func TestBuildMemoryGlobalRoles(t *testing.T) {
	m := BuildMemory(MemoryIn{Scope: ScopeGlobal, Rules: []memory.Rule{{Short: "G1", Origin: memory.OriginChat, Roles: []string{"planner"}}}})
	if m.Repo.Name != "global" {
		t.Errorf("repo %+v", m.Repo)
	}
	r := m.Rules[0]
	if !reflect.DeepEqual(r.Roles, []string{"planner"}) || r.Actions[0].Argv[len(r.Actions[0].Argv)-2] != "--roles" {
		t.Errorf("rule %+v", r)
	}
	if got := m.Create.Argv[len(m.Create.Argv)-3:]; !reflect.DeepEqual(got, []string{"--global", "--roles", "{roles}"}) {
		t.Errorf("create %v", m.Create.Argv)
	}
}

func TestBuildMemoryLearnings(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	there := filepath.Join(root, "a.go")
	exists := func(p string) bool { return p == there }
	learnings := []memory.Learning{
		{Short: "A1", Files: []string{"a.go"}, Seen: 1, FeatureID: "F1"},
		{Short: "A2", Files: []string{"a.go"}, Seen: 2},
		{Short: "A3", Files: []string{"gone.go", "gone2.go"}, Seen: 1},
	}
	for i := range learnings {
		learnings[i].Repo = "repo"
	}
	m := BuildMemory(MemoryIn{Scope: ScopeRepo, Repo: RepoRef{Name: "repo", Path: root}, Exists: exists, Learnings: learnings})
	flags := map[string]string{}
	for _, l := range m.Learnings {
		flags[l.ID] = l.Flag
	}
	want := map[string]string{"A1": "", "A2": FlagSeen, "A3": FlagStale}
	if !reflect.DeepEqual(flags, want) {
		t.Errorf("flags %v, want %v", flags, want)
	}
	if m.Learnings[2].Flag != "" && m.Learnings[0].Flag == "" {
		t.Error("odd learnings first")
	}
	for _, l := range m.Learnings {
		if l.ID == "A1" && !reflect.DeepEqual(l.Features, []string{"F1"}) {
			t.Errorf("features %v", l.Features)
		}
	}
	if len(m.Proposals) != 1 || m.Proposals[0].Kind != "promote" || m.Proposals[0].Target != "A2" {
		t.Fatalf("proposals %+v", m.Proposals)
	}
	if got := m.Proposals[0].Accept.Argv; !reflect.DeepEqual(got, []string{"nxy", "mem", "promote", "A2", "--question", "{question}"}) || m.Proposals[0].Accept.Planned {
		t.Errorf("accept %+v", m.Proposals[0].Accept)
	}
	if !m.Proposals[0].Reject.Planned {
		t.Error("reject has no command yet")
	}
}

func TestBuildMemoryNoStatWithoutRepoPath(t *testing.T) {
	exists := func(string) bool { t.Error("stat without repo path"); return false }
	m := BuildMemory(MemoryIn{Scope: ScopeRepo, Exists: exists, Learnings: []memory.Learning{{Short: "A1", Files: []string{"a.go"}}}})
	if m.Learnings[0].Stale {
		t.Error("stale without a repo path")
	}
}

func TestBuildMemoryLegacyGlobalLearningPromotes(t *testing.T) {
	m := BuildMemory(MemoryIn{Scope: ScopeGlobal, Learnings: []memory.Learning{{Short: "A1", Repo: memory.GlobalRepo, Seen: 1, Text: "x"}}})
	if len(m.Proposals) != 1 || m.Proposals[0].Kind != "promote" {
		t.Fatalf("proposals %+v", m.Proposals)
	}
}

func TestBuildMemoryClosed(t *testing.T) {
	m := BuildMemory(MemoryIn{Scope: ScopeRepo, Closed: []memory.ClosedFeature{{Meta: memory.Meta{ID: "F1", CreatedAt: 1762603200000}, Title: "T", Files: []memory.FileRef{{Repo: "r", Path: "a.go"}}}}})
	c := m.Closed[0]
	if c.ID != "F1" || c.Outcome != "success" || !reflect.DeepEqual(c.Files, []string{"a.go"}) || c.ClosedAt != "2025-11-08T12:00:00Z" {
		t.Errorf("closed %+v", c)
	}
}
