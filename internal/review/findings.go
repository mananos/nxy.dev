package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/plan"
)

// NearLines is how far from a hunk a finding still counts as caused by the change.
const NearLines = 3

// BaseLens is the lens that answers the rules of the plan.
const BaseLens = "base"

// Submission is what one reviewer saves.
type Submission struct {
	Lens      string                 `json:"lens"`
	Rules     []domain.RuleCheck     `json:"rules,omitempty"`
	Decisions []domain.DecisionCheck `json:"decisions,omitempty"`
	Findings  []domain.Finding       `json:"findings"`
}

// DesignDecisions are the decisions of the design a feature carries (the ones
// with an id), in order; none for a delegate feature.
func DesignDecisions(f domain.Feature) []domain.Decision {
	if f.Mode != domain.ModeEngineer {
		return nil
	}
	var src []domain.Decision
	switch {
	case f.Plan != nil && len(f.Plan.Decisions) > 0:
		src = f.Plan.Decisions
	case f.Design != nil:
		src = f.Design.Decisions
	}
	var out []domain.Decision
	for _, d := range src {
		if d.ID != "" {
			out = append(out, d)
		}
	}
	return out
}

// Decode reads one submission; an unknown field or trailing content is an error.
func Decode(r io.Reader) (Submission, error) {
	var s Submission
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Submission{}, fmt.Errorf("review JSON: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Submission{}, errors.New("review JSON: extra content after the object")
	}
	return s, nil
}

func validSeverity(s domain.Severity) bool {
	return s == domain.SeverityHigh || s == domain.SeverityMedium || s == domain.SeverityLow
}

// Validate lists what is wrong with a submission for lens. Rules must be
// answered, once each and only those of the plan, in the base pass; no other
// pass may carry rules. In an engineer feature the base pass also answers each
// decision once and every finding carries a why and a lesson; decisions are
// refused outside the base pass of an engineer feature.
func Validate(sub Submission, lens string, rules []domain.Rule, base bool, decisions []domain.Decision, engineer bool) []plan.Problem {
	var out []plan.Problem
	add := func(where, format string, a ...any) {
		out = append(out, plan.Problem{Where: where, Msg: fmt.Sprintf(format, a...)})
	}
	if sub.Lens != lens {
		add("lens", "must be %q, got %q", lens, sub.Lens)
	}
	if !base {
		if len(sub.Rules) > 0 {
			add("rules", "only the base pass answers the rules")
		}
	} else {
		want := map[string]bool{}
		for _, r := range rules {
			want[r.ID] = true
		}
		seen := map[string]bool{}
		for i, rc := range sub.Rules {
			where := fmt.Sprintf("rules[%d]", i)
			switch {
			case !want[rc.ID]:
				add(where, "%q is not a rule of the plan", rc.ID)
			case seen[rc.ID]:
				add(where, "%s answered more than once", rc.ID)
			}
			seen[rc.ID] = true
			if !rc.OK && strings.TrimSpace(rc.Why) == "" {
				add(where, "a failed rule needs a why")
			}
		}
		for _, r := range rules {
			if !seen[r.ID] {
				add("rules", "%s is missing", r.ID)
			}
		}
	}
	if !base || !engineer {
		if len(sub.Decisions) > 0 {
			add("decisions", "only the base pass of an engineer feature answers the decisions")
		}
	} else {
		want := map[string]bool{}
		for _, d := range decisions {
			want[d.ID] = true
		}
		seen := map[string]bool{}
		for i, dc := range sub.Decisions {
			where := fmt.Sprintf("decisions[%d]", i)
			switch {
			case !want[dc.ID]:
				add(where, "%q is not a decision of the design", dc.ID)
			case seen[dc.ID]:
				add(where, "%s answered more than once", dc.ID)
			}
			seen[dc.ID] = true
			if strings.TrimSpace(dc.File) == "" && strings.TrimSpace(dc.How) == "" {
				add(where, "needs a file and line, or a how saying why it is not in the diff")
			}
		}
		for _, d := range decisions {
			if !seen[d.ID] {
				add("decisions", "%s is missing", d.ID)
			}
		}
	}
	for i, f := range sub.Findings {
		where := fmt.Sprintf("findings[%d]", i)
		if engineer {
			if strings.TrimSpace(f.Why) == "" {
				add(where+".why", "is empty: say why it matters")
			}
			if strings.TrimSpace(f.Lesson) == "" {
				add(where+".lesson", "is empty: give the general rule behind it")
			}
		}
		if !validSeverity(f.Severity) {
			add(where+".severity", "must be high, medium or low")
		}
		if strings.TrimSpace(f.Title) == "" {
			add(where+".title", "is empty")
		}
		for _, x := range []struct{ name, val string }{
			{"id", f.ID}, {"rule", f.Rule}, {"dropped", f.Dropped},
			{"verdict", string(f.Verdict)}, {"verdict_why", f.VerdictWhy},
		} {
			if x.val != "" {
				add(where+"."+x.name, "is set by nxy, do not send it")
			}
		}
	}
	return out
}

// Precision returns why a finding is dropped before the user sees it, or "".
func Precision(f domain.Finding) string {
	if strings.TrimSpace(f.File) == "" || f.Line <= 0 {
		return "sin path:línea"
	}
	if !hasArrow(f.Scenario) {
		return "sin escenario entrada → resultado"
	}
	return ""
}

func hasArrow(s string) bool {
	for _, arrow := range []string{"→", "->"} {
		i := strings.Index(s, arrow)
		if i < 0 {
			continue
		}
		if strings.TrimSpace(s[:i]) != "" && strings.TrimSpace(s[i+len(arrow):]) != "" {
			return true
		}
	}
	return false
}

// Classify sets the cause of each finding: change when it falls within NearLines
// of a hunk of its file (a file with no ranges was changed whole), else preexisting.
// hunks is keyed by path; a finding may name a path suffix.
func Classify(fs []domain.Finding, hunks map[string][][2]int) []domain.Finding {
	lower := map[string][][2]int{}
	for k, v := range hunks {
		lower[strings.ToLower(k)] = v
	}
	out := make([]domain.Finding, len(fs))
	for i, f := range fs {
		f.Cause = domain.CausePreexisting
		name := strings.ToLower(f.File)
		ranges, ok := lower[name]
		if !ok && name != "" {
			for k, v := range lower {
				if strings.HasSuffix(k, "/"+name) {
					ranges, ok = v, true
					break
				}
			}
		}
		if ok {
			if len(ranges) == 0 {
				f.Cause = domain.CauseChange
			}
			for _, r := range ranges {
				if f.Line >= r[0]-NearLines && f.Line <= r[1]+NearLines {
					f.Cause = domain.CauseChange
					break
				}
			}
		}
		out[i] = f
	}
	return out
}

func sevRank(s domain.Severity) int {
	switch s {
	case domain.SeverityHigh:
		return 0
	case domain.SeverityMedium:
		return 1
	}
	return 2
}

// Assemble puts the findings of all lenses together: every failed rule becomes
// a high finding of the base lens (exempt from the precision filter); the rest
// is filtered and numbered H1..Hn, change first and then by severity. The
// findings of rv.Findings are expected already classified.
func Assemble(rv domain.Review, rules []domain.Rule) domain.Review {
	text := map[string]string{}
	for _, r := range rules {
		text[r.ID] = r.Text
	}
	var all []domain.Finding
	for _, rc := range rv.Rules {
		if rc.OK {
			continue
		}
		title := "Regla " + rc.ID + " no se cumple"
		if t := text[rc.ID]; t != "" {
			title += ": " + t
		}
		cause := domain.CauseChange
		all = append(all, domain.Finding{
			Lens: BaseLens, Severity: domain.SeverityHigh, File: rc.File, Line: rc.Line,
			Title: title, Scenario: rc.Why, Cause: cause, Rule: rc.ID,
		})
	}
	for _, f := range rv.Findings {
		if f.Rule == "" {
			f.Dropped = Precision(f)
		}
		all = append(all, f)
	}
	rank := func(f domain.Finding) int {
		r := sevRank(f.Severity)
		if f.Cause != domain.CauseChange {
			r += 10
		}
		return r
	}
	sort.SliceStable(all, func(i, j int) bool { return rank(all[i]) < rank(all[j]) })
	for i := range all {
		all[i].ID = fmt.Sprintf("H%d", i+1)
	}
	rv.Findings = all
	return rv
}

// Offered are the findings the user is asked about: caused by the change, not
// dropped and not refuted.
func Offered(rv domain.Review) []domain.Finding {
	var out []domain.Finding
	for _, f := range rv.Findings {
		if f.Cause == domain.CauseChange && f.Dropped == "" && f.Verdict != domain.VerdictRefuted {
			out = append(out, f)
		}
	}
	return out
}

// HighUnproven says the refuter is needed: high risk and some offered high
// finding without a verdict yet.
func HighUnproven(rv domain.Review) bool {
	if rv.Risk != domain.RiskHigh {
		return false
	}
	for _, f := range Offered(rv) {
		if f.Severity == domain.SeverityHigh && f.Verdict == "" {
			return true
		}
	}
	return false
}
