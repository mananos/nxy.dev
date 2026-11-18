package plan

import (
	"sort"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

// Steps is the schedule of the plan: one step per batch. A batch starts after its Depends; without
// Depends it builds on the previous batch. It also waits for the last earlier batch of its own repo,
// so the batches of one repo run in a row.
func Steps(p domain.Plan) []domain.Step {
	out := make([]domain.Step, 0, len(p.Batches))
	lastOf := map[string]int{}
	prev := 0
	for _, b := range p.Batches {
		set := map[int]bool{}
		if len(b.Depends) > 0 {
			for _, d := range b.Depends {
				set[d] = true
			}
		} else if prev > 0 {
			set[prev] = true
		}
		if l, ok := lastOf[b.Repo]; ok {
			set[l] = true
		}
		delete(set, b.N)
		var after []int
		for d := range set {
			after = append(after, d)
		}
		sort.Ints(after)
		out = append(out, domain.Step{N: b.N, Repo: b.Repo, After: after})
		lastOf[b.Repo] = b.N
		prev = b.N
	}
	return out
}
