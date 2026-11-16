package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/ask"
	"github.com/mananos/nxy.dev/internal/design"
	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/plan"
)

// BuildDecision is the question f.Next asks the user; false when the step is not a question.
// askedAt is epoch ms (0 when unknown). The first question fills the top-level fields, the rest go in Rest.
// waiting are the questions the hook holds in the TUI. When one is the question of f, every option
// answers with `nxy action decide --ask <id>`; without it options carry no argv and the question is
// answered in the Claude Code terminal. A waiting fork question is a Decision of Kind "fork".
func BuildDecision(f domain.Feature, askedAt int64, waiting []domain.PendingAsk) (Decision, bool) {
	a, ok := ask.For(f)
	if !ok {
		return forkDecision(f, askedAt, waiting)
	}
	row := waitingRow(f, waiting, asked(a.Questions))
	d := Decision{
		ID:        fmt.Sprintf("%s:%s:%d", f.ID, a.Kind, f.PlanRev),
		FeatureID: f.ID,
		Repo:      f.Repo,
		Kind:      string(a.Kind),
		Title:     a.Title,
		Body:      nonNilStrings(a.Lines()),
		Context:   nonNilStrings(a.Context),
		Options:   []Option{},
		Rest:      []DecisionQuestion{},
		Release:   Action{Argv: []string{}},
	}
	if row != nil {
		d.AskID = row.ID
		d.Release = releaseAction(row.ID)
	}
	if askedAt > 0 {
		d.AskedAt = time.UnixMilli(askedAt).UTC().Format(time.RFC3339)
	}
	for i, q := range a.Questions {
		opts := optionsOf(row, q.Options)
		if i == 0 {
			d.Header, d.Question, d.MultiSelect, d.Options = q.Header, q.Question, q.MultiSelect, opts
			continue
		}
		d.Rest = append(d.Rest, DecisionQuestion{Header: q.Header, Question: q.Question, MultiSelect: q.MultiSelect, Options: opts})
	}
	if len(a.Questions) == 0 {
		d.Header = a.Title
		if d.Header == "" {
			d.Header = f.Next.Label()
		}
	}
	return d, true
}

// asked is the texts of the questions of an ask, in order.
func asked(qs []ask.Question) []string {
	out := make([]string, 0, len(qs))
	for _, q := range qs {
		out = append(out, q.Question)
	}
	return out
}

// waitingRow is the pending ask of feature f whose questions have exactly these texts; nil when none.
func waitingRow(f domain.Feature, waiting []domain.PendingAsk, texts []string) *domain.PendingAsk {
	if len(texts) == 0 {
		return nil
	}
	for i := range waiting {
		w := &waiting[i]
		if w.FeatureID != f.ID || len(w.Questions) != len(texts) {
			continue
		}
		same := true
		for j, q := range w.Questions {
			if q.Question != texts[j] {
				same = false
				break
			}
		}
		if same {
			return w
		}
	}
	return nil
}

// decideArgv is the command that answers a pending ask with one label (nil without a pending ask).
func decideArgv(row *domain.PendingAsk, label string) []string {
	if row == nil {
		return []string{}
	}
	return []string{"nxy", "action", "decide", "--ask", row.ID, "--answer", label}
}

func releaseAction(askID string) Action {
	return Action{ID: "release", Label: "Contestar en la terminal", Argv: []string{"nxy", "action", "decide", "--ask", askID, "--release"}}
}

// forkDecision is the Decision of a fork question the hook is holding; false when none waits.
func forkDecision(f domain.Feature, askedAt int64, waiting []domain.PendingAsk) (Decision, bool) {
	if f.Design == nil || f.Design.Current == "" {
		return Decision{}, false
	}
	row := waitingRow(f, waiting, []string{design.ForkQuestion(f.ID, f.Design.Current)})
	if row == nil {
		return Decision{}, false
	}
	var fk domain.Fork
	n := 0
	for i, a := range f.Design.Agenda {
		if a.ID == f.Design.Current {
			fk, n = a, i+1
		}
	}
	q := row.Questions[0]
	desc := map[string]string{}
	for _, o := range fk.Options {
		desc[o.Label] = o.Summary
	}
	d := Decision{
		ID:        fmt.Sprintf("%s:fork:%s", f.ID, f.Design.Current),
		FeatureID: f.ID,
		Repo:      f.Repo,
		Kind:      "fork",
		Title:     fk.Title,
		Body:      []string{},
		Header:    q.Header,
		Question:  q.Question,
		Context:   []string{},
		AskID:     row.ID,
		Release:   releaseAction(row.ID),
		Fork:      &ForkRef{N: n, Of: len(f.Design.Agenda), Title: fk.Title},
		Options:   []Option{},
		Rest:      []DecisionQuestion{},
	}
	d.MultiSelect = q.MultiSelect
	if askedAt > 0 {
		d.AskedAt = time.UnixMilli(askedAt).UTC().Format(time.RFC3339)
	}
	for _, l := range q.Options {
		d.Options = append(d.Options, Option{Label: l, Description: desc[l], Action: Action{ID: slug(l), Label: l, Argv: decideArgv(row, l)}})
	}
	return d, true
}

func optionsOf(row *domain.PendingAsk, in []ask.Option) []Option {
	out := make([]Option, 0, len(in))
	for _, o := range in {
		act := Action{ID: slug(o.Label), Label: o.Label, Argv: decideArgv(row, o.Label)}
		if o.Label == plan.Stop {
			act.Confirm = "Termina la feature"
		}
		out = append(out, Option{Label: o.Label, Description: o.Description, Recommended: o.Recommended, Action: act})
	}
	return out
}

// slug is a label as an id: lowercase letters and digits joined by "_".
func slug(s string) string {
	var b strings.Builder
	under := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if under && b.Len() > 0 {
				b.WriteByte('_')
			}
			under = false
			b.WriteRune(r)
		} else {
			under = true
		}
	}
	return b.String()
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
