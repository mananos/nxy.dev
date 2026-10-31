package plan

import (
	"fmt"

	"github.com/mananos/nxy.dev/internal/domain"
)

// Need is what a batch must prove before it counts as done.
type Need struct {
	Red   bool // a failing run before the code (TDD)
	Green bool // a passing run after the code
}

// NeedOf says what batch n (1-based) has to prove. A batch without Accept
// (manual only) needs nothing; a new test covering a tdd rule needs a red first.
func NeedOf(p domain.Plan, n int) Need {
	if n < 1 || n > len(p.Batches) {
		return Need{}
	}
	b := p.Batches[n-1]
	need := Need{Green: b.Test.Accept != ""}
	if need.Green && b.Test.Kind == domain.BatchTestNew {
		kinds := map[string]domain.TestKind{}
		for _, r := range p.Rules {
			kinds[r.ID] = r.Test
		}
		for _, id := range b.Covers {
			if kinds[id] == domain.TestTDD {
				need.Red = true
			}
		}
	}
	return need
}

// Verdict says whether the proof of batch satisfies need. A proof of another
// batch counts as empty. A green only counts if nothing was written after it.
func Verdict(need Need, pr *domain.Proof, batch int) (ok bool, why string) {
	if pr == nil || pr.Batch != batch {
		pr = &domain.Proof{Batch: batch}
	}
	if need.Red {
		switch {
		case pr.Red == nil:
			return false, "no red recorded: write the test and run `batch red` before the code"
		case pr.Red.Exit == 0:
			return false, "the recorded red passed: the test must fail before the code"
		}
	}
	if need.Green {
		switch {
		case pr.Green == nil:
			return false, "no green recorded: run `batch green` after the code"
		case pr.Green.Exit != 0:
			return false, fmt.Sprintf("green failed (exit %d):\n%s", pr.Green.Exit, pr.Green.Tail)
		case pr.Green.Edits != pr.Edits:
			return false, "files were written after the last green: run `batch green` again"
		}
	}
	return true, ""
}
