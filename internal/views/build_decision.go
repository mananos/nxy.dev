package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/ask"
	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/plan"
)

// BuildDecision is the question f.Next asks the user; false when the step is not a question.
// askedAt is epoch ms (0 when unknown). The first question fills the top-level fields, the rest go in Rest.
// Every option answers with `nxy action decide`, which is planned until F5.4. Fork stays nil here.
func BuildDecision(f domain.Feature, askedAt int64) (Decision, bool) {
	a, ok := ask.For(f)
	if !ok {
		return Decision{}, false
	}
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
	}
	if askedAt > 0 {
		d.AskedAt = time.UnixMilli(askedAt).UTC().Format(time.RFC3339)
	}
	for i, q := range a.Questions {
		opts := optionsOf(f.ID, i+1, q.Options)
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

func optionsOf(feature string, q int, in []ask.Option) []Option {
	out := make([]Option, 0, len(in))
	for i, o := range in {
		act := Action{ID: slug(o.Label), Label: o.Label, Argv: o.Argv, Planned: o.Argv == nil}
		if act.Argv == nil {
			act.Argv = []string{"nxy", "action", "decide", "--feature", feature, "--question", fmt.Sprint(q), "--option", fmt.Sprint(i + 1)}
		}
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
