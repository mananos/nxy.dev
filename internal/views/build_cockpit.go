package views

import (
	"sort"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

// CockpitItem is one feature of Inicio with what the caller read for it: the batches running and
// failed, the tokens of its agents and the last tool of the only agent running.
type CockpitItem struct {
	FeatureIn
	Running   []int
	Failed    []int
	TokensIn  int64
	TokensOut int64
	Tool      *ToolUse
	Waiting   []domain.PendingAsk // the questions of the feature the hook holds in the TUI
}

// CockpitIn is the data of Inicio.
type CockpitIn struct {
	Now          time.Time
	Features     []CockpitItem
	CostTodayUSD float64
	Worktrees    []Worktree
}

// BuildCockpit lists the features that need the user first, then the ones working or verifying,
// then the done ones; each group newest first.
func BuildCockpit(in CockpitIn) Cockpit {
	items := append([]CockpitItem(nil), in.Features...)
	rank := func(it CockpitItem) int {
		switch StatusOf(it.Feature.State.Phase) {
		case StatusNeedsYou:
			return 0
		case StatusDone:
			return 2
		}
		return 1
	}
	sort.SliceStable(items, func(i, j int) bool {
		ri, rj := rank(items[i]), rank(items[j])
		if ri != rj {
			return ri < rj
		}
		if items[i].UpdatedAt != items[j].UpdatedAt {
			return items[i].UpdatedAt > items[j].UpdatedAt
		}
		return items[i].Feature.ID < items[j].Feature.ID
	})
	c := Cockpit{
		GeneratedAt: in.Now.UTC().Format(time.RFC3339),
		Totals:      CockpitTotals{CostTodayUSD: in.CostTodayUSD},
		Features:    make([]CockpitFeature, 0, len(items)),
		Worktrees:   in.Worktrees,
	}
	if c.Worktrees == nil {
		c.Worktrees = []Worktree{}
	}
	for _, it := range items {
		fi := it.FeatureIn
		fi.Now = in.Now
		cf := CockpitFeature{
			FeatureHeader: Header(fi),
			Running:       nonNilInts(it.Running),
			Failed:        nonNilInts(it.Failed),
			LastEvent:     Doing(it.Feature, it.Tool),
			TokensIn:      it.TokensIn,
			TokensOut:     it.TokensOut,
			Actions:       []Action{},
		}
		if cf.Status != StatusDone {
			c.Totals.Active++
		}
		if cf.Status == StatusNeedsYou {
			c.Totals.NeedsYou++
		}
		if d, ok := BuildDecision(it.Feature, 0, it.Waiting); ok {
			cf.Decision = &DecisionRef{FeatureID: d.FeatureID, Header: d.Header, Question: d.Question}
			for _, o := range d.Options {
				if d.AskID != "" {
					cf.Actions = append(cf.Actions, o.Action)
				}
			}
		}
		if cf.Status != StatusDone {
			cf.Actions = append(cf.Actions, lifeAction(it.Feature))
		}
		c.Features = append(c.Features, cf)
	}
	return c
}

func lifeAction(f domain.Feature) Action {
	if f.Paused {
		return Action{ID: "resume", Label: "Reanudar", Argv: []string{"nxy", "feature", "resume", "--feature", f.ID}}
	}
	return Action{ID: "pause", Label: "Pausar", Argv: []string{"nxy", "feature", "pause", "--feature", f.ID}}
}

func nonNilInts(n []int) []int {
	if n == nil {
		return []int{}
	}
	return n
}
