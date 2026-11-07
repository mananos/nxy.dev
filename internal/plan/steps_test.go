package plan

import (
	"reflect"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

func stepsOf(bs ...domain.Batch) []domain.Step {
	return Steps(domain.Plan{Batches: bs})
}

func afters(ss []domain.Step) map[int][]int {
	m := map[int][]int{}
	for _, s := range ss {
		m[s.N] = s.After
	}
	return m
}

func TestStepsOneRepoIsAChain(t *testing.T) {
	got := afters(stepsOf(domain.Batch{N: 1}, domain.Batch{N: 2}, domain.Batch{N: 3}))
	want := map[int][]int{1: nil, 2: {1}, 3: {2}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after = %v, want %v", got, want)
	}
}

func TestStepsParallelAcrossRepos(t *testing.T) {
	got := afters(stepsOf(
		domain.Batch{N: 1, Repo: "api"},
		domain.Batch{N: 2, Repo: "web", Depends: []int{1}},
		domain.Batch{N: 3, Repo: "api", Depends: []int{1}},
		domain.Batch{N: 4, Repo: "api"},
	))
	want := map[int][]int{1: nil, 2: {1}, 3: {1}, 4: {3}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after = %v, want %v", got, want)
	}
}

func TestStepsSameRepoWaitsAndSorts(t *testing.T) {
	ss := stepsOf(
		domain.Batch{N: 1, Repo: "api"},
		domain.Batch{N: 2, Repo: "web", Depends: []int{1}},
		domain.Batch{N: 3, Repo: "web", Depends: []int{1, 1}},
		domain.Batch{N: 4, Repo: "api", Depends: []int{2, 1}},
	)
	got := afters(ss)
	want := map[int][]int{1: nil, 2: {1}, 3: {1, 2}, 4: {1, 2}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after = %v, want %v", got, want)
	}
	if ss[1].Repo != "web" {
		t.Errorf("repo = %q", ss[1].Repo)
	}
}
