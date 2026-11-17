package plan

import (
	"fmt"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
)

// Labels of the repo-busy question. The pause and drop labels carry names, so they are told apart
// by their start.
const (
	ClashPauseWord  = "Pausar la"
	ClashDropWord   = "Sacar "
	ClashWorktree   = "Abrir en un worktree"
	ClashStopLabel  = "Parar esta feature"
	clashDropSuffix = " del plan"
)

// ClashQuestion is the exact text asked when a repo the plan needs is held by another feature.
func ClashQuestion(featureID string) string {
	return fmt.Sprintf("nxy feature %s: repo ocupado — ¿qué hacemos?", featureID)
}

// ClashFeatures is the ids of the features that hold the busy checkouts, once each, in order.
func ClashFeatures(cs []domain.RepoClash) []string {
	var out []string
	for _, c := range cs {
		dup := false
		for _, id := range out {
			dup = dup || id == c.FeatureID
		}
		if !dup {
			out = append(out, c.FeatureID)
		}
	}
	return out
}

// ClashRepos is the names of the busy repos, once each.
func ClashRepos(cs []domain.RepoClash) []string {
	var out []string
	for _, c := range cs {
		dup := false
		for _, n := range out {
			dup = dup || n == c.Repo
		}
		if !dup {
			out = append(out, c.Repo)
		}
	}
	return out
}

// ClashOptions are the options of the repo-busy question of f: pause the other feature (recommended),
// open in a worktree (one repo) or take the repo out of the plan (2+ repos, unless every repo is
// busy), and stop this feature.
func ClashOptions(f domain.Feature) []domain.Option {
	ids := ClashFeatures(f.Clash)
	pause := ClashPauseWord + " feature " + strings.Join(ids, ", ")
	why := "Libera su repo y sigue con el plan; la otra queda pausada y la retomás después"
	if len(ids) > 1 {
		pause = ClashPauseWord + "s features " + strings.Join(ids, ", ")
		why = "Libera sus repos y sigue con el plan; las otras quedan pausadas y las retomás después"
	}
	opts := []domain.Option{{Label: pause, Description: why, Recommended: true}}
	if len(f.Repos) <= 1 {
		opts = append(opts, domain.Option{Label: ClashWorktree, Description: "Trabaja en una carpeta aparte del repo (<repo>.worktrees/<rama>) y deja la ocupada como está"})
	} else if busy := ClashRepos(f.Clash); len(busy) < len(f.Repos) {
		opts = append(opts, domain.Option{
			Label:       ClashDropWord + strings.Join(busy, " y ") + clashDropSuffix,
			Description: "nxy vuelve a planificar sin ese repo",
		})
	}
	return append(opts, domain.Option{Label: ClashStopLabel, Description: "Cierra esta feature sin tocar nada"})
}

// ClashChoice is what an answer to the repo-busy question stands for.
type ClashChoice int

const (
	ClashNone ClashChoice = iota
	ClashPause
	ClashOpenWorktree
	ClashDrop
	ClashStopFeature
)

// ParseClash reads the answer to the repo-busy question.
func ParseClash(answer string) ClashChoice {
	a := strings.TrimSpace(answer)
	switch {
	case strings.HasPrefix(a, ClashPauseWord):
		return ClashPause
	case a == ClashWorktree:
		return ClashOpenWorktree
	case strings.HasPrefix(a, ClashDropWord) && strings.HasSuffix(a, clashDropSuffix):
		return ClashDrop
	case a == ClashStopLabel:
		return ClashStopFeature
	}
	return ClashNone
}

// Labels of the decision questions' options.
const (
	Retry    = "Reintentar"
	Continue = "Seguir"
	Stop     = "Parar"
)

// DecisionQuestion is the exact text asked when a batch failed twice.
func DecisionQuestion(featureID string, batch int) string {
	return fmt.Sprintf("nxy feature %s: el lote %d falló dos veces — ¿qué hacemos?", featureID, batch)
}

// AffectedFailedQuestion is the exact text asked when the affected tests failed twice.
func AffectedFailedQuestion(featureID string) string {
	return fmt.Sprintf("nxy feature %s: los tests afectados fallaron dos veces — ¿qué hacemos?", featureID)
}

// UnverifiedQuestion is the exact text asked when the affected tests could not be verified.
func UnverifiedQuestion(featureID string) string {
	return fmt.Sprintf("nxy feature %s: no se pudieron verificar los tests afectados — ¿qué hacemos?", featureID)
}

// FixesFailedQuestion is the exact text asked when the review fixes could not be verified.
func FixesFailedQuestion(featureID string) string {
	return fmt.Sprintf("nxy feature %s: fallaron los arreglos de la review — ¿qué hacemos?", featureID)
}

// PlannerFailedQuestion is the exact text asked when the planner ended without saving a plan.
func PlannerFailedQuestion(featureID string) string {
	return fmt.Sprintf("nxy feature %s: el planner no guardó un plan — ¿qué hacemos?", featureID)
}
