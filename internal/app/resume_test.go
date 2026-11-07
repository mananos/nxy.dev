package app

import (
	"context"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/memory"
)

func resumeFeature() domain.Feature {
	return domain.Feature{
		ID:      "f1",
		Request: "migrar el login\nsegunda línea",
		State:   domain.State{Phase: domain.PhaseBatchFailed, Batch: 2, Batches: 4},
		Proof:   &domain.Proof{Batch: 2, Failure: "FAIL TestLogin"},
		Plan: &domain.Plan{
			Batches:   make([]domain.Batch, 4),
			Decisions: []domain.Decision{{Text: "usar JWT"}, {Text: "usar JWT"}},
		},
		Design: &domain.Design{Decisions: []domain.Decision{{Text: "sin cookies"}}},
	}
}

func TestResumeSummaryContent(t *testing.T) {
	notes := []memory.Note{{Short: "N4", Title: "falta el test", Body: "revisar el mock"}}
	got := resumeSummary(resumeFeature(), notes, 2)
	for _, want := range []string{
		"feature f1", "migrar el login", "lote 2 de 4", "FAIL TestLogin",
		"- usar JWT", "- sin cookies", "Nota N4 «falta el test»", "revisar el mock", "Hay 2 notas más",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Count(got, "usar JWT") != 1 {
		t.Errorf("decision repeated:\n%s", got)
	}
	if strings.Contains(got, "segunda línea") || strings.Contains(got, "¿Con cuál seguimos?") {
		t.Errorf("unexpected text:\n%s", got)
	}
}

func TestResumeSummaryMinimal(t *testing.T) {
	f := domain.Feature{ID: "f2", Request: "algo", State: domain.State{Phase: domain.PhasePlanning}}
	got := resumeSummary(f, nil, 0)
	if strings.Contains(got, "lote") || strings.Contains(got, "falló") || strings.Contains(got, "Decisiones") || strings.Contains(got, "Hay ") {
		t.Errorf("empty parts shown:\n%s", got)
	}
}

func TestSessionStartActiveFeatureAddsResume(t *testing.T) {
	h, mem, ss := notesHooks(t)
	started := h.Handle(context.Background(), prompt("/nxy:feature algo"))
	id := started.Next.Feature.ID
	mem.notes = append(mem.notes, memory.Note{Short: "N9", Title: "de la feature", FeatureID: id})
	r := h.Handle(context.Background(), startEv("resume", "s1"))
	if r.Next == nil {
		t.Fatalf("step instruction missing: %+v", r)
	}
	for _, want := range []string{"para retomar la feature " + id, "algo", "N9", "Hay 2 notas más"} {
		if !strings.Contains(r.Context, want) {
			t.Errorf("context missing %q:\n%s", want, r.Context)
		}
	}
	if ss.rows["s1"].NotesPending {
		t.Errorf("asked about notes with an active feature")
	}
}

func TestStatusShowsProgressAndFailure(t *testing.T) {
	h, st := flowHooks(false)
	h.Handle(context.Background(), prompt("/nxy:feature algo"))
	for id, f := range st.feats {
		f.State = domain.State{Phase: domain.PhaseBatchFailed, Batch: 1, Batches: 3}
		f.Proof = &domain.Proof{Batch: 1, Failure: "boom"}
		f.Plan = &domain.Plan{Decisions: []domain.Decision{{Text: "D uno"}}}
		st.feats[id] = f
	}
	v, err := h.Status(context.Background(), "/p")
	if err != nil {
		t.Fatal(err)
	}
	if v.Progress != "lote 1 de 3" || v.Failure != "boom" || len(v.Decisions) != 1 || v.Decisions[0] != "D uno" {
		t.Errorf("view = %+v", v)
	}
}
