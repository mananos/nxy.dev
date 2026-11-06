package app

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

// pathRepos is fakeRepos where some folders are no git repo.
type pathRepos struct {
	fakeRepos
	notGit map[string]bool
}

func (r pathRepos) Root(dir string) (string, bool) { return dir, !r.notGit[dir] }

// racyStore makes the next Transition fail as if another feature took a checkout meanwhile.
type racyStore struct {
	*memStore
	armed  bool
	holder []string
}

func (r *racyStore) Transition(ctx context.Context, id string, fn func(domain.Feature) (domain.Feature, error)) error {
	if r.armed {
		r.armed = false
		_, _ = r.memStore.CreateFeature(ctx, domain.Feature{Home: "x", Holds: r.holder})
		return ErrActiveFeature
	}
	return r.memStore.Transition(ctx, id, fn)
}

func repoCo(rel string) string { return "repo:" + filepath.Join("/p", rel) }

func multiPlan(paths ...string) domain.Plan {
	p := validPlan()
	p.Batches = nil
	for i, path := range paths {
		p.Repos = append(p.Repos, domain.PlanRepo{Path: path, Role: "r" + path})
		p.Batches = append(p.Batches, domain.Batch{
			N: i + 1, Title: "b", Covers: []string{"R1"},
			Change: []domain.Change{{At: path + "/x.go:1", What: "x"}},
			Test:   domain.BatchTest{Kind: domain.BatchTestNew, Name: "TestX", Accept: "go test ./..."},
			Risk:   domain.RiskLow,
		})
	}
	return p
}

func TestSavePlanReposResolvedAndHeld(t *testing.T) {
	h, st := flowHooks(false)
	f := startedFeature(t, h, "x")
	res, err := h.SavePlan(context.Background(), f.ID, planReader(t, multiPlan("api", "web")))
	if err != nil || len(res.Problems) > 0 || res.Blocked != "" {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	g := st.feats[f.ID]
	if len(g.Repos) != 2 || g.Repos[0].Name != "api" || g.Repos[1].Name != "web" || g.Repos[0].Role != "rapi" {
		t.Errorf("repos = %+v", g.Repos)
	}
	if g.Plan.Batches[0].Repo != "api" || g.Plan.Batches[1].Repo != "web" || g.Plan.Repos[1].Name != "web" {
		t.Errorf("plan = %+v", g.Plan)
	}
	for _, co := range []string{g.Home, repoCo("api"), repoCo("web")} {
		if !slices.Contains(g.Holds, co) {
			t.Errorf("holds = %v, missing %s", g.Holds, co)
		}
	}
}

func TestSavePlanRepoWithoutGitIsRefused(t *testing.T) {
	h, st := flowHooks(false)
	h.Repos = pathRepos{notGit: map[string]bool{filepath.Join("/p", "ghost"): true}}
	f := startedFeature(t, h, "x")
	res, err := h.SavePlan(context.Background(), f.ID, planReader(t, multiPlan("api", "ghost")))
	if err != nil || len(res.Problems) != 1 || !strings.Contains(res.Problems[0].Msg, "repo ghost: no es un repo git") {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	if st.feats[f.ID].Plan != nil {
		t.Error("the plan was stored")
	}
}

func TestSavePlanRepeatedFolderNamesTakeParents(t *testing.T) {
	h, st := flowHooks(false)
	f := startedFeature(t, h, "x")
	res, err := h.SavePlan(context.Background(), f.ID, planReader(t, multiPlan("a/api", "b/api")))
	if err != nil || len(res.Problems) > 0 {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	g := st.feats[f.ID]
	if g.Repos[0].Name != "a/api" || g.Repos[1].Name != "b/api" || g.Plan.Batches[1].Repo != "b/api" {
		t.Errorf("repos = %+v", g.Repos)
	}
}

func TestSavePlanBatchWithTwoReposIsRefused(t *testing.T) {
	h, _ := flowHooks(false)
	f := startedFeature(t, h, "x")
	p := multiPlan("api", "web")
	p.Batches[0].Change = append(p.Batches[0].Change, domain.Change{At: "web/y.go:1", What: "y"})
	res, err := h.SavePlan(context.Background(), f.ID, planReader(t, p))
	if err != nil || len(res.Problems) == 0 || !strings.Contains(res.Problems[0].Msg, "a batch touches one repo") {
		t.Fatalf("res = %+v err = %v", res, err)
	}
}

func TestSavePlanClashOneRepoWaitsForTheUser(t *testing.T) {
	h, st := flowHooks(false)
	f := startedFeature(t, h, "x")
	other, _ := st.CreateFeature(context.Background(), domain.Feature{Home: "o", Holds: []string{repoCo("api")}})
	res, err := h.SavePlan(context.Background(), f.ID, planReader(t, multiPlan("api", "web")))
	if err != nil || res.Rev != 0 || !strings.Contains(res.Blocked, "repo api is held by feature "+other) || !strings.Contains(res.Blocked, "plan blocked") {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	g := st.feats[f.ID]
	if g.State.Phase != domain.PhasePlanning || g.Plan != nil || g.PendingPlan == nil || g.PlanRev != 0 {
		t.Errorf("feature = %+v", g)
	}
	if len(g.Clash) != 1 || g.Clash[0].Repo != "api" || g.Clash[0].FeatureID != other || g.Clash[0].Checkout != repoCo("api") {
		t.Errorf("clash = %+v", g.Clash)
	}
	if slices.Contains(g.Holds, repoCo("api")) {
		t.Errorf("holds = %v", g.Holds)
	}
}

func TestSavePlanClashTwoReposAndSessionFolder(t *testing.T) {
	h, st := flowHooks(false)
	f := startedFeature(t, h, "x")
	ctx := context.Background()
	a, _ := st.CreateFeature(ctx, domain.Feature{Home: "o", Holds: []string{repoCo("api")}})
	b, _ := st.CreateFeature(ctx, domain.Feature{Home: "o2", Holds: []string{repoCo("web")}})
	res, err := h.SavePlan(ctx, f.ID, planReader(t, multiPlan("api", "web")))
	if err != nil || !strings.Contains(res.Blocked, "repo api is held by feature "+a) || !strings.Contains(res.Blocked, "repo web is held by feature "+b) {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	if len(st.feats[f.ID].Clash) != 2 {
		t.Errorf("clash = %+v", st.feats[f.ID].Clash)
	}
}

func TestSavePlanClashOnTheSessionFolderWithoutRepos(t *testing.T) {
	h, st := flowHooks(false)
	ctx := context.Background()
	g := startedFeature(t, h, "x")
	x := st.feats[g.ID]
	x.Holds = nil // created without the folder: another session's feature has it
	st.feats[g.ID] = x
	other, _ := st.CreateFeature(ctx, domain.Feature{Home: "o", Holds: []string{"repo:/p"}})
	res, err := h.SavePlan(ctx, g.ID, planReader(t, validPlan()))
	if err != nil || !strings.Contains(res.Blocked, "held by feature "+other) {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	if st.feats[g.ID].Plan != nil || st.feats[g.ID].PendingPlan == nil {
		t.Errorf("feature = %+v", st.feats[g.ID])
	}
}

func TestSavePlanRaceBecomesAClash(t *testing.T) {
	h, st := flowHooks(false)
	f := startedFeature(t, h, "x")
	h.Store = &racyStore{memStore: st, armed: true, holder: []string{repoCo("api")}}
	res, err := h.SavePlan(context.Background(), f.ID, planReader(t, multiPlan("api")))
	if err != nil || !strings.Contains(res.Blocked, "repo api is held by feature") {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	if g := st.feats[f.ID]; g.PendingPlan == nil || g.Plan != nil || len(g.Clash) != 1 {
		t.Errorf("feature = %+v", g)
	}
}
