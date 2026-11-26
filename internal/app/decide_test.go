package app

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

func decideSetup(t *testing.T, multi bool) (Hooks, *fakeAsks, string) {
	t.Helper()
	asks := newFakeAsks()
	row, err := asks.CreatePendingAsk(context.Background(), domain.PendingAsk{FeatureID: "F1", Questions: []domain.AskedQuestion{
		{Question: "Uno?", Options: []string{"A", "B"}, MultiSelect: multi},
		{Question: "Dos?", Options: []string{"X", "Y"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return Hooks{Asks: asks}, asks, row.ID
}

func TestDecideOneAnswerPerQuestion(t *testing.T) {
	h, asks, id := decideSetup(t, false)
	ctx := context.Background()
	if err := h.Decide(ctx, id, []string{"A"}, false); err == nil {
		t.Error("a missing answer was accepted")
	}
	if err := h.Decide(ctx, id, []string{"A", "Y"}, false); err != nil {
		t.Fatal(err)
	}
	row, _ := asks.PendingAsk(ctx, id)
	if len(row.Answers) != 2 || row.Answers[0] != "A" || row.Answers[1] != "Y" || row.AnsweredBy != "tui" {
		t.Errorf("row = %+v", row)
	}
	if err := h.Decide(ctx, id, []string{"B", "X"}, false); !errors.Is(err, ErrAskAnswered) {
		t.Errorf("second answer: %v", err)
	}
}

func TestDecideMultiSelectJoinsWithComma(t *testing.T) {
	h, asks, id := decideSetup(t, true)
	if err := h.Decide(context.Background(), id, []string{"A,B", "X"}, false); err != nil {
		t.Fatal(err)
	}
	row, _ := asks.PendingAsk(context.Background(), id)
	if row.Answers[0] != "A, B" {
		t.Errorf("answers = %q", row.Answers)
	}
}

func TestDecideMultiSelectLabelsWithCommas(t *testing.T) {
	asks := newFakeAsks()
	opts := []string{"H1: uno, dos", "H2: tres, cuatro, cinco", "H3: seis"}
	row, err := asks.CreatePendingAsk(context.Background(), domain.PendingAsk{FeatureID: "F1", Questions: []domain.AskedQuestion{
		{Question: "Hallazgos?", Options: opts, MultiSelect: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := Hooks{Asks: asks}
	ctx := context.Background()
	if err := h.Decide(ctx, row.ID, []string{"H1: uno, dos,H2: tres, cuatro, cinco, H3: seis"}, false); err != nil {
		t.Fatal(err)
	}
	got, _ := asks.PendingAsk(ctx, row.ID)
	if want := "H1: uno, dos, H2: tres, cuatro, cinco, H3: seis"; got.Answers[0] != want {
		t.Errorf("answers = %q, want %q", got.Answers, want)
	}
}

func TestDecideFreeText(t *testing.T) {
	ctx := context.Background()
	h, asks, id := decideSetup(t, false)
	if err := h.Decide(ctx, id, []string{"  mi propia idea ", "Y"}, false); err != nil {
		t.Fatal(err)
	}
	row, _ := asks.PendingAsk(ctx, id)
	if row.Answers[0] != "mi propia idea" || row.Answers[1] != "Y" {
		t.Errorf("answers = %q", row.Answers)
	}
	h, asks, id = decideSetup(t, true)
	if err := h.Decide(ctx, id, []string{"A, mío", "X"}, false); err != nil {
		t.Fatal(err)
	}
	row, _ = asks.PendingAsk(ctx, id)
	if row.Answers[0] != "A, mío" {
		t.Errorf("mixed answers = %q", row.Answers)
	}
	for _, bad := range []string{"", " ", ",", "A,"} {
		h, _, id := decideSetup(t, true)
		if err := h.Decide(ctx, id, []string{bad, "X"}, false); err == nil {
			t.Errorf("answer %q was accepted", bad)
		}
	}
	for _, bad := range []string{"", " ", ","} {
		h, _, id := decideSetup(t, false)
		if err := h.Decide(ctx, id, []string{"A", bad}, false); err == nil {
			t.Errorf("single answer %q was accepted", bad)
		}
	}
}

func TestDecideMultiSelectEmptyAnswer(t *testing.T) {
	for _, bad := range []string{"", " ", ",", "A,"} {
		h, asks, id := decideSetup(t, true)
		ctx := context.Background()
		if err := h.Decide(ctx, id, []string{bad, "X"}, false); err == nil {
			t.Errorf("answer %q was accepted", bad)
		}
		row, _ := asks.PendingAsk(ctx, id)
		if len(row.Answers) != 0 {
			t.Errorf("answer %q left answers = %q", bad, row.Answers)
		}
	}
}

// staleAsks reads every unanswered row as the store does once its hook stopped renewing it.
type staleAsks struct{ *fakeAsks }

func (staleAsks) PendingAsk(context.Context, string) (domain.PendingAsk, error) {
	return domain.PendingAsk{}, sql.ErrNoRows
}

func TestDecideStaleRowIsGone(t *testing.T) {
	h, asks, id := decideSetup(t, false)
	h.Asks = staleAsks{asks}
	if err := h.Decide(context.Background(), id, []string{"A", "X"}, false); !errors.Is(err, ErrAskGone) {
		t.Errorf("stale: %v", err)
	}
}

func TestDecideReleaseAndGone(t *testing.T) {
	h, asks, id := decideSetup(t, false)
	ctx := context.Background()
	if err := h.Decide(ctx, id, nil, true); err != nil {
		t.Fatal(err)
	}
	row, _ := asks.PendingAsk(ctx, id)
	if row.Answers == nil || len(row.Answers) != 0 || row.AnsweredBy != "terminal" {
		t.Errorf("row = %+v", row)
	}
	_ = asks.DeletePendingAsk(ctx, id)
	if err := h.Decide(ctx, id, []string{"A", "X"}, false); !errors.Is(err, ErrAskGone) {
		t.Errorf("gone: %v", err)
	}
}
