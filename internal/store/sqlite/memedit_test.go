package sqlite

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/memory"
)

func strp(s string) *string       { return &s }
func listp(s ...string) *[]string { return &s }

func TestMemEditRuleKeepsIdentityAndReindexes(t *testing.T) {
	s, clock := newMem(t)
	r, err := s.AddRule(ctx, rule("/srv/dev/a", "use the old pool", "when?"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cite(ctx, "/srv/dev/a", []string{"K1"}, memT0.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(time.Minute)
	e, err := s.EditRule(ctx, "k1", memory.RuleEdit{Text: strp("use the shared pool"), Paths: listp("db/", "db/")})
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != r.ID || e.Short != "K1" || e.Citations != 1 || e.UpdatedAt <= r.UpdatedAt || e.CreatedAt != r.CreatedAt {
		t.Fatalf("edited %+v from %+v", e, r)
	}
	if hits, _ := s.Search(ctx, "/srv/dev/a", false, "shared", 0); len(hits) != 1 {
		t.Errorf("new text not found: %+v", hits)
	}
	if hits, _ := s.Search(ctx, "/srv/dev/a", false, "old", 0); len(hits) != 0 {
		t.Errorf("old text still found: %+v", hits)
	}
	if _, err := s.EditRule(ctx, "K1", memory.RuleEdit{}); err == nil {
		t.Error("an empty edit must fail")
	}
	if _, err := s.EditRule(ctx, "K1", memory.RuleEdit{Roles: listp("planner")}); err == nil {
		t.Error("roles on a K must fail")
	}
	g, _ := s.AddGlobalRule(ctx, globalRule("g one"))
	g2, err := s.EditRule(ctx, g.Short, memory.RuleEdit{Roles: listp("Reviewer")})
	if err != nil || !slices.Equal(g2.Roles, []string{"reviewer"}) {
		t.Fatalf("global roles %+v %v", g2, err)
	}
}

func TestMemEditLearning(t *testing.T) {
	s, _ := newMem(t)
	a, _ := s.AddLearning(ctx, memory.Learning{Repo: "/srv/dev/a", Text: "one"})
	s.AddLearning(ctx, memory.Learning{Repo: "/srv/dev/a", Text: "two"})
	if _, err := s.EditLearning(ctx, a.Short, memory.LearningEdit{Text: strp("two")}); err == nil || !strings.Contains(err.Error(), "mismo texto y archivos que A2") {
		t.Fatalf("collision not refused: %v", err)
	}
	e, err := s.EditLearning(ctx, a.Short, memory.LearningEdit{Text: strp("three"), Files: listp("x.go")})
	if err != nil || e.Short != "A1" || e.Hash == a.Hash || !slices.Equal(e.Files, []string{"x.go"}) {
		t.Fatalf("edit %+v %v", e, err)
	}
	if _, err := s.EditLearning(ctx, "A1", memory.LearningEdit{}); err == nil {
		t.Error("an empty edit must fail")
	}
	if _, err := s.Promote(ctx, "A2", "when?", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EditLearning(ctx, "A2", memory.LearningEdit{Text: strp("x")}); err == nil || !strings.Contains(err.Error(), "ya es la regla K1") {
		t.Errorf("promoted edit: %v", err)
	}
}

func TestMemDeleteRestore(t *testing.T) {
	s, clock := newMem(t)
	s.AddRule(ctx, rule("/srv/dev/a", "searchable thing", "q?"))
	*clock = clock.Add(time.Minute)
	short, err := s.DeleteMemory(ctx, "K1")
	if err != nil || short != "K1" {
		t.Fatalf("rm %q %v", short, err)
	}
	if rs, _ := s.Rules(ctx, "/srv/dev/a"); len(rs) != 0 {
		t.Errorf("deleted rule listed: %+v", rs)
	}
	if _, err := s.Resolve(ctx, KindRule, "K1"); !errors.Is(err, memory.ErrNotFound) {
		t.Errorf("resolve deleted: %v", err)
	}
	if hits, _ := s.Search(ctx, "/srv/dev/a", false, "searchable", 0); len(hits) != 0 {
		t.Errorf("deleted rule found: %+v", hits)
	}
	if _, err := s.DeleteMemory(ctx, "K1"); err == nil {
		t.Error("deleting twice must fail")
	}
	n, _ := s.AddRule(ctx, rule("/srv/dev/a", "second", "q?"))
	if n.Short != "K2" {
		t.Errorf("short reused: %s", n.Short)
	}
	if short, err := s.RestoreMemory(ctx, "K1"); err != nil || short != "K1" {
		t.Fatalf("restore %q %v", short, err)
	}
	if hits, _ := s.Search(ctx, "/srv/dev/a", false, "searchable", 0); len(hits) != 1 || hits[0].Short != "K1" {
		t.Errorf("restored rule not found: %+v", hits)
	}
	if _, err := s.RestoreMemory(ctx, "K1"); err == nil {
		t.Error("restoring a live rule must fail")
	}
	if _, err := s.DeleteMemory(ctx, "N1"); err == nil {
		t.Error("notes must be refused")
	}

	// learnings: a restore that clashes with a live twin is refused
	l, _ := s.AddLearning(ctx, memory.Learning{Repo: "/srv/dev/a", Text: "same"})
	s.DeleteMemory(ctx, l.Short)
	s.AddLearning(ctx, memory.Learning{Repo: "/srv/dev/a", Text: "same"})
	if _, err := s.RestoreMemory(ctx, "A1"); err == nil || !strings.Contains(err.Error(), "A2 tiene el mismo aprendizaje") {
		t.Errorf("restore collision: %v", err)
	}
}

func TestMemEditDeleteRestoreTravel(t *testing.T) {
	a, ac := newMem(t)
	b, _ := newMem(t)
	a.AddRule(ctx, rule("/srv/dev/a", "k text", "q?"))
	a.AddGlobalRule(ctx, globalRule("g text"))
	a.AddLearning(ctx, memory.Learning{Repo: "/srv/dev/a", Text: "a text"})
	transfer(t, a, b, false)

	*ac = ac.Add(time.Minute)
	if _, err := a.EditRule(ctx, "K1", memory.RuleEdit{Text: strp("k edited")}); err != nil {
		t.Fatal(err)
	}
	a.EditRule(ctx, "G1", memory.RuleEdit{Roles: listp("planner")})
	a.EditLearning(ctx, "A1", memory.LearningEdit{Text: strp("a edited")})
	transfer(t, a, b, false)
	if rs, _ := b.Rules(ctx, "/srv/dev/a"); len(rs) != 1 || rs[0].Text != "k edited" {
		t.Fatalf("edit did not travel: %+v", rs)
	}
	if gs, _ := b.GlobalRules(ctx); len(gs) != 1 || !slices.Equal(gs[0].Roles, []string{"planner"}) {
		t.Fatalf("roles did not travel: %+v", gs)
	}
	if ls, _ := b.Learnings(ctx, "/srv/dev/a"); len(ls) != 1 || ls[0].Text != "a edited" {
		t.Fatalf("learning edit did not travel: %+v", ls)
	}

	*ac = ac.Add(time.Minute)
	for _, ref := range []string{"K1", "G1", "A1"} {
		if _, err := a.DeleteMemory(ctx, ref); err != nil {
			t.Fatal(err)
		}
	}
	transfer(t, a, b, false)
	gs, _ := b.GlobalRules(ctx)
	rs, _ := b.Rules(ctx, "/srv/dev/a")
	ls, _ := b.Learnings(ctx, "/srv/dev/a")
	if len(gs)+len(rs)+len(ls) != 0 {
		t.Fatalf("deletions did not travel: %v %v %v", gs, rs, ls)
	}

	*ac = ac.Add(time.Minute)
	for _, ref := range []string{"K1", "G1", "A1"} {
		if _, err := a.RestoreMemory(ctx, ref); err != nil {
			t.Fatal(err)
		}
	}
	transfer(t, a, b, false)
	gs, _ = b.GlobalRules(ctx)
	rs, _ = b.Rules(ctx, "/srv/dev/a")
	ls, _ = b.Learnings(ctx, "/srv/dev/a")
	if len(gs) != 1 || len(rs) != 1 || len(ls) != 1 || rs[0].Short != "K1" || gs[0].Short != "G1" || ls[0].Short != "A1" {
		t.Fatalf("restores did not travel: %v %v %v", gs, rs, ls)
	}
	if d := transfer(t, a, b, false); d.Writes() != 0 {
		t.Errorf("second import writes %d", d.Writes())
	}
}

func TestMemEditedLearningCollidingOnImportFolds(t *testing.T) {
	a, ac := newMem(t)
	b, _ := newMem(t)
	a.AddLearning(ctx, memory.Learning{Repo: "/srv/dev/a", Text: "alpha"})
	b.AddLearning(ctx, memory.Learning{Repo: "/srv/dev/a", Text: "beta"})
	transfer(t, a, b, false)

	*ac = ac.Add(time.Minute)
	if _, err := a.EditLearning(ctx, "A1", memory.LearningEdit{Text: strp("beta")}); err != nil {
		t.Fatal(err)
	}
	transfer(t, a, b, false) // must not abort on the unique index
	ls, err := b.Learnings(ctx, "/srv/dev/a")
	if err != nil || len(ls) != 1 || ls[0].Text != "beta" || ls[0].Seen != 2 {
		t.Fatalf("learnings %+v %v", ls, err)
	}
	if d := transfer(t, a, b, false); d.Writes() != 0 {
		t.Errorf("second import writes %d", d.Writes())
	}
}

func TestMemKeep(t *testing.T) {
	s, _ := newMem(t)
	s.AddRule(ctx, rule("/srv/dev/a", "keep me", "q?"))
	s.AddLearning(ctx, memory.Learning{Repo: "/srv/dev/a", Text: "learnt"})
	for _, ref := range []string{"K1", "A1"} {
		if short, err := s.KeepMemory(ctx, ref); err != nil || short != ref {
			t.Fatalf("keep %s: %q %v", ref, short, err)
		}
	}
	if rs, _ := s.Rules(ctx, "/srv/dev/a"); len(rs) != 1 || rs[0].KeptAt == 0 {
		t.Errorf("rule not kept: %+v", rs)
	}
	if ls, _ := s.Learnings(ctx, "/srv/dev/a"); len(ls) != 1 || ls[0].KeptAt == 0 {
		t.Errorf("learning not kept: %+v", ls)
	}
	if _, err := s.KeepMemory(ctx, "K9"); err == nil {
		t.Error("keeping an unknown ref must fail")
	}
}
