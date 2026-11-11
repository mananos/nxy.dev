package sqlite

import (
	"slices"
	"testing"

	"github.com/mananos/nxy.dev/internal/memory"
)

func globalRule(text string, roles ...string) memory.Rule {
	return memory.Rule{Text: text, Question: "when does it apply?", Origin: memory.OriginManual, Roles: roles}
}

func TestMemGlobalShortsAreIndependent(t *testing.T) {
	s, _ := newMem(t)
	var got []string
	for _, f := range []func() (memory.Rule, error){
		func() (memory.Rule, error) { return s.AddRule(ctx, rule("/srv/dev/a", "one", "q?")) },
		func() (memory.Rule, error) { return s.AddGlobalRule(ctx, globalRule("g one")) },
		func() (memory.Rule, error) { return s.AddRule(ctx, rule("/srv/dev/a", "two", "q?")) },
		func() (memory.Rule, error) { return s.AddGlobalRule(ctx, globalRule("g two")) },
	} {
		r, err := f()
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, r.Short)
	}
	if !slices.Equal(got, []string{"K1", "G1", "K2", "G2"}) {
		t.Fatalf("shorts %v", got)
	}
	if _, err := s.Resolve(ctx, KindRule, "g2"); err != nil {
		t.Errorf("resolve G2: %v", err)
	}
}

func TestMemGlobalRolesRoundTripAndSupersedes(t *testing.T) {
	s, _ := newMem(t)
	g, err := s.AddGlobalRule(ctx, globalRule("g one", "Reviewer", "planner", "planner"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(g.Roles, []string{"planner", "reviewer"}) {
		t.Fatalf("roles %v", g.Roles)
	}
	rs, err := s.GlobalRules(ctx)
	if err != nil || len(rs) != 1 || !slices.Equal(rs[0].Roles, []string{"planner", "reviewer"}) || rs[0].Repo != "" {
		t.Fatalf("global rules %+v %v", rs, err)
	}
	if rs, _ := s.Rules(ctx, "/srv/dev/a"); len(rs) != 0 {
		t.Errorf("global leaked into a repo: %+v", rs)
	}
	if _, err := s.AddRule(ctx, memory.Rule{Repo: "/srv/dev/a", Text: "k", Question: "q?", Origin: memory.OriginManual, Roles: []string{"planner"}}); err == nil {
		t.Error("roles on a repo rule must fail")
	}
	k, _ := s.AddRule(ctx, rule("/srv/dev/a", "k", "q?"))
	sup := globalRule("g new")
	sup.Supersedes = k.Short
	if _, err := s.AddGlobalRule(ctx, sup); err == nil {
		t.Error("a G must not supersede a K")
	}
	sup.Supersedes = "G1"
	if _, err := s.AddGlobalRule(ctx, sup); err != nil {
		t.Fatal(err)
	}
	if rs, _ := s.GlobalRules(ctx); len(rs) != 1 || rs[0].Short != "G2" {
		t.Errorf("after supersede: %+v", rs)
	}
}

func TestMemPromoteGlobalLearning(t *testing.T) {
	s, _ := newMem(t)
	if _, err := s.AddLearning(ctx, memory.Learning{Repo: "", Text: "prefer small commits"}); err == nil {
		t.Fatal("AddLearning still refuses an empty repo")
	}
	// a global learning arrives through import or the legacy import: build it through a bundle
	src, _ := newMem(t)
	gl := memory.Learning{Repo: "/srv/dev/a", Text: "prefer small commits"}
	added, err := src.AddLearning(ctx, gl)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO mem_learnings (id, short, repo, text, files, feature_id, hash, seen, last_seen_at, obsolete_at, promoted_to, created_at, updated_at, deleted_at, machine_id)
		VALUES (?, 'A1', '', ?, '[]', '', ?, 1, 1, 0, '', 1, 1, 0, 'm')`, added.ID, added.Text, memory.LearningHash(added.Text, nil)); err != nil {
		t.Fatal(err)
	}
	r, err := s.Promote(ctx, "A1", "when committing?", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Short != "G1" || r.Repo != "" {
		t.Fatalf("promoted %+v", r)
	}
}

func TestMemCiteGlobalFromAnyRepo(t *testing.T) {
	s, _ := newMem(t)
	if _, err := s.AddGlobalRule(ctx, globalRule("g one")); err != nil {
		t.Fatal(err)
	}
	found, err := s.Cite(ctx, "/srv/dev/a", []string{"g1", "K1"}, 5)
	if err != nil || !slices.Equal(found, []string{"G1"}) {
		t.Fatalf("found %v %v", found, err)
	}
	rs, _ := s.GlobalRules(ctx)
	if rs[0].Citations != 1 {
		t.Errorf("citations %d", rs[0].Citations)
	}
}

func TestMemSearchFindsGlobalRuleInRepo(t *testing.T) {
	s, _ := newMem(t)
	if _, err := s.AddGlobalRule(ctx, globalRule("always sign the changelog")); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Search(ctx, "/srv/dev/a", false, "changelog", 0)
	if err != nil || len(hits) != 1 || hits[0].Short != "G1" || hits[0].Repo != "" {
		t.Fatalf("hits %+v %v", hits, err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO mem_features (id, title, decisions, findings, created_at, updated_at, deleted_at, machine_id)
		VALUES ('F1', 'changelog work', '[]', '[]', 1, 1, 0, 'm')`); err != nil {
		t.Fatal(err)
	}
	hits, _ = s.Search(ctx, "/srv/dev/a", false, "changelog", 0)
	for _, h := range hits {
		if h.Kind == "feature" {
			t.Errorf("stray feature in a repo search: %+v", hits)
		}
	}
}

func TestMemGlobalRuleTravelsAndReassignsShort(t *testing.T) {
	a, _ := newMem(t)
	b, _ := newMem(t)
	if _, err := a.AddGlobalRule(ctx, globalRule("from a", "planner")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddGlobalRule(ctx, globalRule("from b")); err != nil {
		t.Fatal(err)
	}
	transfer(t, a, b, false)
	rs, err := b.GlobalRules(ctx)
	if err != nil || len(rs) != 2 {
		t.Fatalf("rules %+v %v", rs, err)
	}
	shorts := []string{rs[0].Short, rs[1].Short}
	slices.Sort(shorts)
	if !slices.Equal(shorts, []string{"G1", "G2"}) {
		t.Errorf("shorts %v", shorts)
	}
	for _, r := range rs {
		if r.Text == "from a" && !slices.Equal(r.Roles, []string{"planner"}) {
			t.Errorf("roles lost: %+v", r)
		}
	}
}
