package design

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mananos/nxy.dev/internal/domain"
)

// gapsInput is what the planner sends when the design leaves a hole.
type gapsInput struct {
	Gaps []string `json:"gaps"`
}

// DecodeGaps reads {"gaps": ["title", ...]}.
func DecodeGaps(r io.Reader) ([]string, error) {
	var g gapsInput
	if err := decodeStrict(r, "gaps", &g); err != nil {
		return nil, err
	}
	return g.Gaps, nil
}

// ValidateGaps checks the titles of the holes the planner found.
func ValidateGaps(titles []string) []domain.Problem {
	var out []domain.Problem
	if len(titles) == 0 {
		out = append(out, domain.Problem{Where: "gaps", Msg: "at least one gap is needed"})
	}
	for i, t := range titles {
		where := fmt.Sprintf("gaps[%d]", i)
		if strings.TrimSpace(t) == "" {
			out = append(out, domain.Problem{Where: where, Msg: "empty title"})
		} else if !oneLine(t) || utf8.RuneCountInString(t) > maxText {
			out = append(out, domain.Problem{Where: where, Msg: fmt.Sprintf("title must be one line of at most %d characters", maxText)})
		}
	}
	return out
}

// CheckPlan checks that the planner of an engineer feature did not change the design: same rules,
// no questions, and decisions of its own only for forks the user delegated.
func CheckPlan(d *domain.Design, p domain.Plan) []domain.Problem {
	var out []domain.Problem
	add := func(where, format string, a ...any) {
		out = append(out, domain.Problem{Where: where, Msg: fmt.Sprintf(format, a...)})
	}
	if len(p.Questions) > 0 {
		add("questions", "an engineer plan asks nothing: a design hole goes to `plan gaps`")
	}
	if len(p.Rules) != len(d.Rules) {
		add("rules", "the design approved %d rules; the plan has %d (copy them as they are)", len(d.Rules), len(p.Rules))
	}
	for i, r := range p.Rules {
		if i >= len(d.Rules) {
			break
		}
		if r.ID != d.Rules[i].ID || r.Text != d.Rules[i].Text {
			add(d.Rules[i].ID, "the rule text is the one the user approved; copy it unchanged")
		}
	}
	for i, x := range p.Decisions {
		where := fmt.Sprintf("decision %d", i+1)
		fork := findFork(d, x.Fork)
		if fork == nil || fork.Status != domain.ForkDelegated {
			add(where, "the planner decides only a delegated fork: set fork to one of them or send the hole to `plan gaps`")
		}
	}
	return out
}

// Merge sets the plan decisions to the ones of the design plus the planner's own, which carry
// the mark of who chose them.
func Merge(d *domain.Design, p *domain.Plan) {
	merged := append([]domain.Decision(nil), d.Decisions...)
	ids := make([]string, 0, len(merged))
	for _, x := range merged {
		ids = append(ids, x.ID)
	}
	n := nextNum(ids, "D")
	for _, x := range p.Decisions {
		x.ID = "D" + strconv.Itoa(n)
		x.By = domain.ByPlanner
		n++
		merged = append(merged, x)
	}
	p.Decisions = merged
}

// AddGaps raises the holes as open forks and reopens the design.
func AddGaps(d *domain.Design, titles []string) {
	AddForks(d, titles, domain.SourcePlanner)
	d.Closed = false
}
