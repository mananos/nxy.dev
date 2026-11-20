package sqlite

import (
	"slices"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/memory"
)

func TestCountDeliveriesKeepsUpdatedAtAndFTS(t *testing.T) {
	s, clock := newMem(t)
	r, err := s.AddRule(ctx, rule("r", "usar conexion unica", "how do we open the db?"))
	if err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(time.Hour)
	if err := s.CountDeliveries(ctx, []string{r.ID, r.ID}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Rules(ctx, "r")
	if len(got) != 1 || got[0].Deliveries != 2 || got[0].UpdatedAt != r.UpdatedAt {
		t.Fatalf("%+v", got)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM mem_fts WHERE kind='rule'`).Scan(&n)
	if n != 1 {
		t.Errorf("fts rows %d", n)
	}
	if hits, _ := s.Search(ctx, "r", false, "conexion", 0); len(hits) != 1 {
		t.Errorf("hits %+v", hits)
	}
}

func TestCiteOnlyExistingRules(t *testing.T) {
	s, clock := newMem(t)
	r, _ := s.AddRule(ctx, rule("r", "una regla", "q"))
	*clock = clock.Add(time.Hour)
	at := clock.UnixMilli()
	found, err := s.Cite(ctx, "r", []string{"k1", "K9"}, at)
	if err != nil || !slices.Equal(found, []string{"K1"}) {
		t.Fatalf("%v %v", found, err)
	}
	if found, _ := s.Cite(ctx, "other", []string{"K1"}, at); len(found) != 0 {
		t.Errorf("other repo cited %v", found)
	}
	got, _ := s.Rules(ctx, "r")
	if got[0].Citations != 1 || got[0].LastCitedAt != at || got[0].UpdatedAt != r.UpdatedAt {
		t.Errorf("%+v", got[0])
	}
}

func TestMarkObsoleteMovesUpdatedAt(t *testing.T) {
	s, clock := newMem(t)
	l, _ := s.AddLearning(ctx, memory.Learning{Repo: "r", Text: "algo cierto", Files: []string{"a.go"}})
	*clock = clock.Add(time.Hour)
	at := clock.UnixMilli()
	if err := s.MarkObsolete(ctx, "r", []string{"A1", "A9", "K1", "zzz"}, at); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Learnings(ctx, "r"); len(got) != 0 {
		t.Errorf("still listed %+v", got)
	}
	var obs, upd int64
	s.db.QueryRow(`SELECT obsolete_at, updated_at FROM mem_learnings WHERE id=?`, l.ID).Scan(&obs, &upd)
	if obs != at || upd != at {
		t.Errorf("obsolete %d updated %d want %d", obs, upd, at)
	}
}

func TestSimilarRules(t *testing.T) {
	s, _ := newMem(t)
	s.AddRule(ctx, rule("r", "use one database connection per process", "q"))
	s.AddRule(ctx, rule("r", "format dates in utc always", "q"))
	got, err := s.SimilarRules(ctx, "r", "use one database connection per request", 5)
	if err != nil || len(got) != 1 || got[0].Short != "K1" {
		t.Fatalf("%+v %v", got, err)
	}
	if got, _ := s.SimilarRules(ctx, "other", "use one database connection per request", 5); len(got) != 0 {
		t.Errorf("other repo %+v", got)
	}
	if got, _ := s.SimilarRules(ctx, "r", "completely unrelated words here", 5); len(got) != 0 {
		t.Errorf("unrelated %+v", got)
	}
}

func TestEscapesAndReviewStats(t *testing.T) {
	s, _ := newMem(t)
	if err := s.RecordPicks(ctx, "f1", "r", []domain.Pick{
		{Finding: "H1", Lens: "base", Severity: domain.Severity("high"), Chosen: true},
		{Finding: "H2", Lens: "base", Severity: domain.Severity("low")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordEscape(ctx, "", "r", "base", "se escapo X"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordEscape(ctx, "f1", "r", "sec", "se escapo Y"); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReviewStats(ctx, "r", 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.LensStat{{Lens: "base", Chosen: 1, Ignored: 1, Escapes: 1}, {Lens: "sec", Escapes: 1}}
	if !slices.Equal(got, want) {
		t.Errorf("%+v", got)
	}
	if got, _ := s.ReviewStats(ctx, "r", time.Now().Add(time.Hour).UnixMilli()); len(got) != 0 {
		t.Errorf("since %+v", got)
	}
}

func TestReviewIndexesExist(t *testing.T) {
	s, _ := newMem(t)
	for _, name := range []string{"review_picks_repo", "review_escapes_repo"} {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("index %s missing", name)
		}
	}
}

func TestMigrate5To6KeepsMemory(t *testing.T) {
	path := dbPath(t)
	migs, _ := embeddedMigrations()
	s, err := openWith(ctx, path, time.Now, migs[:5])
	if err != nil {
		t.Fatal(err)
	}
	// the schema of this version has no roles column: write the row as the old build did
	if _, err := s.db.ExecContext(ctx, `INSERT INTO mem_rules (id, short, repo, text, question, origin, created_at, updated_at)
		VALUES ('01OLD', 'K1', 'r', 'regla vieja', 'q', 'manual', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s2 := mustOpen(t, path)
	if v := version(t, s2); v != len(migs) {
		t.Errorf("version %d", v)
	}
	got, _ := s2.Rules(ctx, "r")
	if len(got) != 1 || got[0].Text != "regla vieja" {
		t.Fatalf("%+v", got)
	}
	if err := s2.CountDeliveries(ctx, []string{got[0].ID}); err != nil {
		t.Fatal(err)
	}
	if hits, _ := s2.Search(ctx, "r", false, "vieja", 0); len(hits) != 1 {
		t.Errorf("hits %+v", hits)
	}
}
