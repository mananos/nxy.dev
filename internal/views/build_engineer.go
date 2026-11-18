package views

import (
	"github.com/mananos/nxy.dev/internal/design"
	"github.com/mananos/nxy.dev/internal/domain"
)

// EngineerIn is what the engineer screen needs. Draw turns a d2 source into a drawn diagram; the
// provider backs it with d2 and a cache, so the builder stays pure.
type EngineerIn struct {
	FeatureIn
	Draw func(src string) Diagram
	// Waiting are the questions the hook holds in the TUI; the one of the current fork gives AskID.
	Waiting []domain.PendingAsk
}

// BuildEngineer builds the engineer mode screen; false when the feature has no design.
func BuildEngineer(in EngineerIn) (Engineer, bool) {
	d := in.Feature.Design
	if d == nil {
		return Engineer{}, false
	}
	draw := func(src string) Diagram {
		if src == "" || in.Draw == nil {
			return Diagram{Source: src, Lines: []string{}}
		}
		g := in.Draw(src)
		if g.Lines == nil {
			g.Lines = []string{}
		}
		return g
	}
	out := Engineer{
		Feature: Header(in.FeatureIn), Agenda: []EngFork{}, Options: []EngOption{},
		Today: draw(d.Today), Other: "Otra: escribila en el chat",
	}
	choice := map[string]string{}
	for _, dec := range d.Decisions {
		if dec.Fork != "" {
			choice[dec.Fork] = dec.Text
		}
	}
	var cur *domain.Fork
	for i, fk := range d.Agenda {
		e := EngFork{ID: fk.ID, Title: fk.Title}
		switch fk.Status {
		case domain.ForkDecided:
			e.Status, e.Choice = ForkDecided, choice[fk.ID]
		case domain.ForkDelegated:
			e.Status, e.Choice = ForkDecided, "la decide el planner"
		default:
			e.Status = ForkPending
			if fk.ID == d.Current {
				e.Status = ForkCurrent
				cur = &d.Agenda[i]
			}
		}
		out.Agenda = append(out.Agenda, e)
	}
	if cur == nil {
		return out, true
	}
	out.Current, out.Question, out.Opinion = cur.ID, cur.Question, cur.Opinion
	if out.Question == "" {
		out.Question = cur.Title
	}
	row := waitingRow(in.Feature, in.Waiting, []string{design.ForkQuestion(in.Feature.ID, cur.ID)})
	if row != nil {
		out.AskID = row.ID
	}
	for i, o := range cur.Options {
		key := o.Key
		if key == "" {
			key = string(rune('A' + i))
		}
		out.Options = append(out.Options, EngOption{
			Key: key, Label: o.Label, Summary: o.Summary, Pros: nonNilStrings(o.Pros), Cons: nonNilStrings(o.Cons),
			Touches: o.Touches, Diagram: draw(o.D2),
			Action: Action{ID: "decide-" + key, Label: o.Label, Argv: decideArgv(row, o.Label)},
		})
	}
	return out, true
}
