package sqlite

import (
	"testing"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

func TestPendingAsks(t *testing.T) {
	s := mustOpen(t, dbPath(t))
	qs := []domain.AskedQuestion{{Question: "¿sí?", Header: "h", Options: []string{"a", "b"}}}
	a, err := s.CreatePendingAsk(ctx, domain.PendingAsk{SessionID: "s", FeatureID: "F1", ToolUseID: "t", Questions: qs})
	if err != nil || a.ID == "" || a.CreatedAt == 0 {
		t.Fatalf("create = %+v %v", a, err)
	}
	got, err := s.PendingAsk(ctx, a.ID)
	if err != nil || got.FeatureID != "F1" || len(got.Questions) != 1 || got.Questions[0].Options[1] != "b" || got.Answers != nil {
		t.Fatalf("read = %+v %v", got, err)
	}
	if open, _ := s.OpenPendingAsks(ctx); len(open) != 1 {
		t.Fatalf("open = %+v", open)
	}
	ok, err := s.AnswerPendingAsk(ctx, a.ID, []string{"a"}, "tui")
	if err != nil || !ok {
		t.Fatalf("answer = %v %v", ok, err)
	}
	if ok, _ := s.AnswerPendingAsk(ctx, a.ID, []string{"b"}, "terminal"); ok {
		t.Fatal("el segundo Answer debió dar false")
	}
	got, _ = s.PendingAsk(ctx, a.ID)
	if len(got.Answers) != 1 || got.Answers[0] != "a" || got.AnsweredBy != "tui" {
		t.Fatalf("after answer = %+v", got)
	}
	if open, _ := s.OpenPendingAsks(ctx); len(open) != 0 {
		t.Fatalf("open after answer = %+v", open)
	}
	if err := s.DeletePendingAsk(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PendingAsk(ctx, a.ID); err == nil {
		t.Fatal("la fila borrada se leyó")
	}
}

func TestReleaseFeatureAsks(t *testing.T) {
	s := mustOpen(t, dbPath(t))
	var ids []string
	for _, f := range []string{"F1", "F1", "F2"} {
		a, err := s.CreatePendingAsk(ctx, domain.PendingAsk{FeatureID: f})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, a.ID)
	}
	if err := s.ReleaseFeatureAsks(ctx, "F1", "stopped"); err != nil {
		t.Fatal(err)
	}
	open, _ := s.OpenPendingAsks(ctx)
	if len(open) != 1 || open[0].FeatureID != "F2" {
		t.Fatalf("open = %+v", open)
	}
	got, _ := s.PendingAsk(ctx, ids[0])
	if got.AnsweredBy != "stopped" || got.Answers == nil || len(got.Answers) != 0 {
		t.Fatalf("released = %+v", got)
	}
}

func TestSweepPendingAsks(t *testing.T) {
	clock := time.Now()
	s, err := Open(ctx, dbPath(t), func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	old, _ := s.CreatePendingAsk(ctx, domain.PendingAsk{FeatureID: "F1"})
	clock = clock.Add(pendingAskTTL / 2)
	fresh, _ := s.CreatePendingAsk(ctx, domain.PendingAsk{FeatureID: "F1"})
	clock = clock.Add(pendingAskTTL/2 + time.Hour)
	if _, err := s.SweepNotes(ctx, clock); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PendingAsk(ctx, old.ID); err == nil {
		t.Fatal("la fila de más de 24 h sigue")
	}
	_ = s.TouchPendingAsk(ctx, fresh.ID) // el barrido no mira touched_at; sólo así se puede leer una fila vieja
	if _, err := s.PendingAsk(ctx, fresh.ID); err != nil {
		t.Fatalf("la fila nueva se barrió: %v", err)
	}
}

func TestPendingAskStaleWhenNotRenewed(t *testing.T) {
	clock := time.Now()
	s, err := Open(ctx, dbPath(t), func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a, err := s.CreatePendingAsk(ctx, domain.PendingAsk{FeatureID: "F1", Questions: []domain.AskedQuestion{{Question: "¿?", Options: []string{"a"}}}})
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(pendingAskStale + time.Second)
	if open, _ := s.OpenPendingAsks(ctx); len(open) != 0 {
		t.Fatalf("una fila sin renovar apareció: %+v", open)
	}
	if _, err := s.PendingAsk(ctx, a.ID); err == nil {
		t.Fatal("una fila sin renovar se leyó")
	}
	if ok, _ := s.AnswerPendingAsk(ctx, a.ID, []string{"a"}, "tui"); ok {
		t.Fatal("una fila sin renovar se contestó")
	}
	if err := s.TouchPendingAsk(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if open, _ := s.OpenPendingAsks(ctx); len(open) != 1 {
		t.Fatalf("la fila renovada no apareció: %+v", open)
	}
}
