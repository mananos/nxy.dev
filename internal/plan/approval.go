package plan

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

// Labels of the approval question's options.
const (
	Approve  = "Aprobar"
	Change   = "Cambiar"
	ViewFull = "Ver completo"
)

// ApprovalQuestion is the exact text of the approval question. It carries the
// feature id and plan revision so an old answer cannot approve a newer plan.
func ApprovalQuestion(featureID string, rev int) string {
	return fmt.Sprintf("¿Aprobás el plan %s r%d de nxy?", featureID, rev)
}

// ApprovalRev is the plan revision of q when q is an approval question of the feature (any revision).
func ApprovalRev(featureID, q string) (int, bool) {
	rest, ok := strings.CutPrefix(q, "¿Aprobás el plan "+featureID+" r")
	if !ok {
		return 0, false
	}
	rest, ok = strings.CutSuffix(rest, " de nxy?")
	if !ok {
		return 0, false
	}
	rev, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return rev, true
}

// BranchQuestion is the exact text of the question for the branch of the feature.
func BranchQuestion(featureID string) string {
	return fmt.Sprintf("nxy feature %s: ¿en qué rama trabajamos?", featureID)
}

// BasePrefix starts every base question of the feature.
func BasePrefix(featureID string) string {
	return fmt.Sprintf("nxy feature %s: ¿desde qué base se crea la rama", featureID)
}

// BaseQuestion is the exact text of the question for the base of the branch in a repo.
func BaseQuestion(featureID, repo string) string {
	return BasePrefix(featureID) + " en " + repo + "?"
}

// maxAsk is how many questions fit in one AskUserQuestion call.
const maxAsk = 4

// BranchAsk is one question of the branch step: its text, the header and the options.
type BranchAsk struct {
	Question string
	Header   string
	Options  []domain.Option
}

// BranchQuestions are the questions still open for the offer of f, at most four: the branch first,
// then the base of each repo that has none yet.
func BranchQuestions(f domain.Feature) []BranchAsk {
	o := f.BranchOffer
	if o == nil {
		return nil
	}
	var out []BranchAsk
	if o.Branch == "" {
		var opts []domain.Option
		for i, b := range o.Suggest {
			d := "Rama nueva con este nombre. Otra: escribila en Other"
			if len(o.Repos) > 0 && b == o.Repos[0].Current {
				d = "La rama en la que ya estás. Otra: escribila en Other"
			}
			if i == 0 {
				d += " (recomendada)"
			}
			opts = append(opts, domain.Option{Label: b, Description: d, Recommended: i == 0})
		}
		out = append(out, BranchAsk{Question: BranchQuestion(f.ID), Header: "Rama", Options: opts})
	}
	for _, r := range o.Repos {
		if r.Base != "" || len(out) >= maxAsk {
			continue
		}
		var opts []domain.Option
		for i, b := range r.Bases {
			d := "Rama local desde la que sale la rama nueva"
			if i == 0 {
				d += " (recomendada)"
			}
			opts = append(opts, domain.Option{Label: b, Description: d, Recommended: i == 0})
		}
		out = append(out, BranchAsk{Question: BaseQuestion(f.ID, r.Name), Header: "Base", Options: opts})
	}
	return out
}
