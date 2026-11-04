package sqlite

import (
	"testing"
	"time"

	"github.com/mananos/nxy.dev/internal/memory"
)

func TestSessionOnceOnlyFlags(t *testing.T) {
	s, _ := newMem(t)
	if err := s.StartSession(ctx, Session{ID: "s1", Folder: "/w/proj", Source: "startup", NotesPending: true}); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ConsumeNotesPending(ctx, "s1"); err != nil || !ok {
		t.Fatalf("first consume %v %v", ok, err)
	}
	if ok, _ := s.ConsumeNotesPending(ctx, "s1"); ok {
		t.Error("notes_pending consumed twice")
	}
	if ok, _ := s.ConsumeNotesPending(ctx, "missing"); ok {
		t.Error("consumed a missing session")
	}
	if ok, err := s.MarkReadHint(ctx, "s1", 5); err != nil || !ok {
		t.Fatalf("first hint %v %v", ok, err)
	}
	if ok, _ := s.MarkReadHint(ctx, "s1", 6); ok {
		t.Error("read hint given twice")
	}
	if ok, _ := s.MarkReadHint(ctx, "fresh", 7); !ok {
		t.Error("hint on a session without SessionStart")
	}
	if err := s.MarkStopAsked(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if err := s.ChainNote(ctx, "s1", "N3"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Session(ctx, "s1")
	if err != nil || !ok || !got.StopAsked || got.NoteID != "N3" || got.NotesPending || got.ReadHintAt != 5 {
		t.Fatalf("%+v %v %v", got, ok, err)
	}
}

func TestSessionMarkEditUpserts(t *testing.T) {
	s, _ := newMem(t)
	if err := s.MarkEdit(ctx, "s9", "/w/proj", 100); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkEdit(ctx, "s9", "/other", 200); err != nil {
		t.Fatal(err)
	}
	got, ok, _ := s.Session(ctx, "s9")
	if !ok || got.LastEditAt != 200 || got.Folder != "/w/proj" {
		t.Errorf("%+v", got)
	}
	if got.StartedAt != 99 || got.LastEditAt <= got.StartedAt {
		t.Errorf("first edit must count after the start: %+v", got)
	}
}

func TestSessionRecentEndWindowAndPrune(t *testing.T) {
	s, clock := newMem(t)
	base := clock.UnixMilli()
	s.StartSession(ctx, Session{ID: "a", Folder: "/w/proj", NoteID: "N1"})
	s.StartSession(ctx, Session{ID: "b", Folder: "/z", NoteID: "N2"})
	s.EndSession(ctx, "a", "clear", base)
	s.EndSession(ctx, "b", "clear", base)
	if x, ok, err := s.RecentEnd(ctx, "/w/proj", "clear", base-2000, false); err != nil || !ok || x.ID != "a" || x.NoteID != "N1" {
		t.Errorf("%+v %v %v", x, ok, err)
	}
	if _, ok, _ := s.RecentEnd(ctx, "/w/proj", "clear", base+1, false); ok {
		t.Error("ended before the window")
	}
	if _, ok, _ := s.RecentEnd(ctx, "/w/proj", "logout", base-2000, false); ok {
		t.Error("other reason matched")
	}
	if _, ok, _ := s.RecentEnd(ctx, "/W/PROJ", "clear", base-2000, true); !ok {
		t.Error("fold case did not match")
	}
	*clock = clock.Add(8 * 24 * time.Hour)
	if n, err := s.PruneSessions(ctx, clock.Add(-7*24*time.Hour)); err != nil || n != 2 {
		t.Errorf("prune %d %v", n, err)
	}
}

func TestNotesDeleteByFeatureAndFolder(t *testing.T) {
	s, _ := newMem(t)
	a, _ := s.SaveNote(ctx, memory.Note{Folder: "/w/proj", Title: "a", Body: "x", FeatureID: "F1"})
	s.SaveNote(ctx, memory.Note{Folder: "/w/proj/wt", Title: "b", Body: "x"})
	s.SaveNote(ctx, memory.Note{Folder: "/w/projx", Title: "c", Body: "x"})
	s.SaveNote(ctx, memory.Note{Folder: "/w", Title: "d", Body: "x"})
	if got, err := s.Note(ctx, "N1"); err != nil || got.ID != a.ID {
		t.Fatalf("%+v %v", got, err)
	}
	if n, err := s.DeleteNotesOfFeature(ctx, "F1"); err != nil || n != 1 {
		t.Fatalf("by feature %d %v", n, err)
	}
	if _, err := s.Note(ctx, "N1"); err == nil {
		t.Error("deleted note still readable")
	}
	if n, err := s.DeleteNotesUnder(ctx, "/w/proj", false); err != nil || n != 1 {
		t.Fatalf("under %d %v", n, err)
	}
	b, _ := s.ExportBundle(ctx, true)
	dead := 0
	for _, n := range b.Notes {
		if n.Deleted() {
			dead++
			if n.Body != "" {
				t.Error("tombstone kept the body")
			}
		}
	}
	if dead != 2 || len(b.Notes) != 4 {
		t.Errorf("tombstones %d of %d", dead, len(b.Notes))
	}
}
