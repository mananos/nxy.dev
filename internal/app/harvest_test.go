package app

import (
	"context"
	"reflect"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/memory"
)

type harvestMemory struct {
	deliverMemory
	added []memory.Learning
}

func (m *harvestMemory) AddLearning(_ context.Context, l memory.Learning) (memory.Learning, error) {
	m.added = append(m.added, l)
	return l, nil
}

func stopEv(root, agentType, id, msg string) HookEvent {
	return HookEvent{Name: "SubagentStop", AgentType: agentType, AgentID: id, AgentResult: msg, ProjectDir: root, Cwd: root}
}

func TestHarvestImplementerLearningsStickToBatchFiles(t *testing.T) {
	mem := &harvestMemory{}
	h, root, _ := deliverSetup(t, &mem.deliverMemory)
	h.Memory = mem
	msg := "done\nlearned: calc rounds half up | files: calc.go, other.go\nfalse: a1"
	h.Handle(context.Background(), stopEv(root, roleImplementer, "ag1", msg))
	if len(mem.added) != 1 {
		t.Fatalf("added = %+v", mem.added)
	}
	l := mem.added[0]
	if l.Text != "calc rounds half up" || !reflect.DeepEqual(l.Files, []string{"calc.go"}) || l.FeatureID == "" {
		t.Errorf("learning = %+v", l)
	}
	if len(mem.obsolete) != 1 || mem.obsolete[0] != "A1" {
		t.Errorf("obsolete = %v", mem.obsolete)
	}
}

func TestHarvestOncePerAgent(t *testing.T) {
	mem := &harvestMemory{}
	h, root, _ := deliverSetup(t, &mem.deliverMemory)
	h.Memory = mem
	msg := "learned: calc rounds half up"
	h.Handle(context.Background(), stopEv(root, roleImplementer, "ag1", msg))
	h.Handle(context.Background(), stopEv(root, roleImplementer, "ag1", msg))
	if len(mem.added) != 1 {
		t.Errorf("same agent harvested %d times", len(mem.added))
	}
	h.Handle(context.Background(), stopEv(root, roleImplementer, "ag2", msg))
	if len(mem.added) != 2 {
		t.Errorf("another agent should add: %d", len(mem.added))
	}
}

func TestHarvestWithoutAgentIDSkips(t *testing.T) {
	mem := &harvestMemory{}
	h, root, _ := deliverSetup(t, &mem.deliverMemory)
	h.Memory = mem
	h.Handle(context.Background(), stopEv(root, roleImplementer, "", "learned: calc rounds half up"))
	if len(mem.added) != 0 {
		t.Errorf("harvested without agent id: %+v", mem.added)
	}
}

func TestHarvestReviewerDoesNotHarvest(t *testing.T) {
	mem := &harvestMemory{}
	h, root, _ := deliverSetup(t, &mem.deliverMemory)
	h.Memory = mem
	h.Handle(context.Background(), stopEv(root, roleReviewer, "ag1", "learned: something"))
	if len(mem.added) != 0 {
		t.Errorf("reviewer harvested: %+v", mem.added)
	}
}

func TestHarvestNilMemoryIsQuiet(t *testing.T) {
	h, root, _ := deliverSetup(t, nil)
	h.Handle(context.Background(), stopEv(root, roleImplementer, "ag1", "learned: something"))
}
