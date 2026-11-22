package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

type fixedClock struct{ t time.Time }

func (c *fixedClock) Now() time.Time { return c.t }

func mainStopEv(sid string) HookEvent {
	return HookEvent{Name: "Stop", SessionID: sid, ProjectDir: "/p", Cwd: "/p"}
}

func editEv(sid string) HookEvent {
	return HookEvent{Name: "PreToolUse", Tool: "Edit", SessionID: sid, ProjectDir: "/p", Cwd: "/p", FilePath: "/p/a.go"}
}

// stopHooks is notesHooks with a clock that moves, so edits come after the session start.
func stopHooks(t *testing.T) (Hooks, *noteMemory, *fakeSessions, *fixedClock) {
	t.Helper()
	h, mem, ss := notesHooks(t)
	clk := &fixedClock{t: time.UnixMilli(1_000_000)}
	h.Clock = clk
	h.Handle(context.Background(), startEv("startup", "s1"))
	clk.t = clk.t.Add(time.Minute)
	return h, mem, ss, clk
}

func TestStopAsksForANoteOnceAfterAnEdit(t *testing.T) {
	h, _, ss, _ := stopHooks(t)
	ctx := context.Background()
	if r := h.Handle(ctx, mainStopEv("s1")); r.Block != "" {
		t.Fatalf("no edits yet, yet blocked: %+v", r)
	}
	h.Handle(ctx, editEv("s1"))
	if ss.rows["s1"].LastEditAt == 0 {
		t.Fatal("the edit was not marked")
	}
	r := h.Handle(ctx, mainStopEv("s1"))
	if !strings.Contains(r.Block, `"/bin/nxy" note save`) || !strings.Contains(r.Block, "--session s1") || !strings.Contains(r.Block, "<<'NOTE'") {
		t.Fatalf("block = %q", r.Block)
	}
	if r := h.Handle(ctx, mainStopEv("s1")); r.Block != "" {
		t.Errorf("asked twice: %+v", r)
	}
}

func TestStopDoesNotAskWithAnActiveFeature(t *testing.T) {
	h, _, _, _ := stopHooks(t)
	ctx := context.Background()
	h.Handle(ctx, prompt("/nxy:feature algo"))
	h.Handle(ctx, editEv("s1"))
	if r := h.Handle(ctx, mainStopEv("s1")); r.Block != "" {
		t.Errorf("blocked with an active feature: %+v", r)
	}
}

func TestStopDoesNotAskWhenTheNoteIsNewerThanTheLastEdit(t *testing.T) {
	h, mem, ss, clk := stopHooks(t)
	ctx := context.Background()
	h.Handle(ctx, editEv("s1"))
	clk.t = clk.t.Add(time.Minute)
	// the session chained a note written after that edit
	mem.notes[0].UpdatedAt = clk.t.UnixMilli()
	_ = ss.ChainNote(ctx, "s1", "N1")
	if r := h.Handle(ctx, mainStopEv("s1")); r.Block != "" {
		t.Errorf("asked although the note is current: %+v", r)
	}
}

func TestStopAsksAgainForAChainedNoteOlderThanTheEdit(t *testing.T) {
	h, mem, ss, clk := stopHooks(t)
	ctx := context.Background()
	mem.notes[0].UpdatedAt = clk.t.UnixMilli()
	_ = ss.ChainNote(ctx, "s1", "N1")
	clk.t = clk.t.Add(time.Minute)
	h.Handle(ctx, editEv("s1"))
	r := h.Handle(ctx, mainStopEv("s1"))
	if !strings.Contains(r.Block, "--id N1") {
		t.Errorf("block = %q", r.Block)
	}
}

func TestStopNoteDeadChainedNote(t *testing.T) {
	for name, deleted := range map[string]bool{"deleted": true, "missing": false} {
		t.Run(name, func(t *testing.T) {
			h, mem, ss, clk := stopHooks(t)
			ctx := context.Background()
			if deleted {
				mem.notes[0].DeletedAt = clk.t.UnixMilli()
			} else {
				mem.notes = nil
			}
			_ = ss.ChainNote(ctx, "s1", "N1")
			clk.t = clk.t.Add(time.Minute)
			h.Handle(ctx, editEv("s1"))
			r := h.Handle(ctx, mainStopEv("s1"))
			if !strings.Contains(r.Block, "note save") {
				t.Fatalf("no note asked: %q", r.Block)
			}
			if strings.Contains(r.Block, "--id") || strings.Contains(r.Block, "continues note") {
				t.Errorf("block chains a dead note: %q", r.Block)
			}
		})
	}
}

func TestOwnNoteCommandsArePreApproved(t *testing.T) {
	h, _, _, _ := stopHooks(t)
	h.Config = fakeConfig{}
	ctx := context.Background()
	bash := func(cmd string) HookResponse {
		return h.Handle(ctx, HookEvent{Name: "PreToolUse", Tool: "Bash", SessionID: "s1", ProjectDir: "/p", Cwd: "/p", Command: cmd, HasCommand: true})
	}
	save := "\"/bin/nxy\" note save \"migrar login\" --session s1 <<'NOTE'\nfalta el test\nNOTE\n"
	if r := bash(save); r.AllowReason == "" {
		t.Errorf("own session note not approved: %+v", r)
	}
	withID := "\"/bin/nxy\" note save \"migrar login\" --session s1 --id N3 <<'NOTE'\nx\nNOTE\n"
	if r := bash(withID); r.AllowReason == "" {
		t.Errorf("update of a note not approved: %+v", r)
	}
	for name, cmd := range map[string]string{
		"other session":   "\"/bin/nxy\" note save \"x\" --session other <<'NOTE'\nx\nNOTE\n",
		"no session":      "\"/bin/nxy\" note save \"x\" <<'NOTE'\nx\nNOTE\n",
		"JSON delimiter":  "\"/bin/nxy\" note save \"x\" --session s1 <<'JSON'\nx\nJSON\n",
		"trailing text":   "\"/bin/nxy\" note save \"x\" --session s1 <<'NOTE'\nx\nNOTE\nrm -rf /\n",
		"extra flag":      "\"/bin/nxy\" note save \"x\" --session s1 --cwd /etc <<'NOTE'\nx\nNOTE\n",
		"other binary":    "\"/tmp/evil\" note save \"x\" --session s1 <<'NOTE'\nx\nNOTE\n",
		"bad id":          "\"/bin/nxy\" note save \"x\" --session s1 --id ../x <<'NOTE'\nx\nNOTE\n",
		"done with flags": "\"/bin/nxy\" note done N1 --all",
	} {
		if r := bash(cmd); r.AllowReason != "" {
			t.Errorf("%s: approved: %+v", name, r)
		}
	}
	if r := bash("\"/bin/nxy\" note done N2"); r.AllowReason == "" {
		t.Errorf("note done not approved: %+v", r)
	}
	if r := bash("\"/bin/nxy\" note done ../x"); r.AllowReason != "" {
		t.Errorf("note done with a bad ref approved: %+v", r)
	}
}

func TestShellWriteMarksTheEdit(t *testing.T) {
	h, _, ss, _ := stopHooks(t)
	h.Config = fakeConfig{}
	h.Handle(context.Background(), HookEvent{Name: "PreToolUse", Tool: "Bash", SessionID: "s1", ProjectDir: "/p", Cwd: "/p",
		Command: "echo hi > /p/out.txt", HasCommand: true})
	if ss.rows["s1"].LastEditAt == 0 {
		t.Errorf("a shell write did not mark the edit")
	}
}

func TestEndingAFeatureDeletesItsNotes(t *testing.T) {
	h, mem, _ := notesHooks(t)
	ctx := context.Background()
	r := h.Handle(ctx, prompt("/nxy:feature algo"))
	id := r.Next.Feature.ID
	if _, _, err := h.StopFeature(ctx, "/p"); err != nil {
		t.Fatal(err)
	}
	if len(mem.deletedFor) != 1 || mem.deletedFor[0] != id {
		t.Errorf("stop: deletedFor = %v", mem.deletedFor)
	}
	// a feature that closed well
	mem.deletedFor = nil
	f := domain.Feature{ID: "F9", State: domain.State{Phase: domain.PhaseDone, Outcome: domain.OutcomeSuccess}}
	h.afterAdvance(ctx, f, []domain.Action{{Kind: domain.Close}})
	if len(mem.deletedFor) != 1 || mem.deletedFor[0] != "F9" {
		t.Errorf("close: deletedFor = %v", mem.deletedFor)
	}
}
