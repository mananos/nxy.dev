package plan

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/repos"
)

const (
	maxWhatLen   = 200
	maxQuestions = 4
	minOptions   = 2
	maxOptions   = 4
)

var (
	ruleIDRe = regexp.MustCompile(`^R[1-9][0-9]*$`)
	atRe     = regexp.MustCompile(`^\S.*(:[0-9]+(-[0-9]+)?| \(new\))$`)
)

// Problem is one thing wrong with a plan, located by Where.
type Problem = domain.Problem

type checker struct{ out []Problem }

func (c *checker) add(where, format string, a ...any) {
	c.out = append(c.out, Problem{Where: where, Msg: fmt.Sprintf(format, a...)})
}

// short flags a text field with a triple backtick or a line break.
func (c *checker) short(where, s string) {
	if strings.Contains(s, "```") {
		c.add(where, "must not contain a triple backtick")
	}
	if strings.ContainsAny(s, "\r\n") {
		c.add(where, "must be one line")
	}
}

// ValidateRepos checks the multi-repo part of a plan. names[i] is the name nxy gave p.Repos[i].
// Without repos, no decision may name a repo and nothing else is checked.
func ValidateRepos(p domain.Plan, names []string) []Problem {
	c := &checker{}
	if len(p.Repos) == 0 {
		for i, d := range p.Decisions {
			if d.Repo != "" {
				c.add(fmt.Sprintf("decision %d", i+1), "repo %q needs the plan to declare repos", d.Repo)
			}
		}
		return c.out
	}
	if len(names) != len(p.Repos) {
		c.add("repos", "%d names for %d repos", len(names), len(p.Repos))
		return c.out
	}
	declared := map[string]bool{}
	for i, r := range p.Repos {
		w := fmt.Sprintf("repo %d", i+1)
		if strings.TrimSpace(r.Path) == "" {
			c.add(w, "path is empty")
		}
		c.short(w+" path", r.Path)
		c.short(w+" role", r.Role)
		c.short(w+" suite", r.Suite)
		if declared[names[i]] {
			c.add(w, "name %q is repeated", names[i])
		}
		declared[names[i]] = true
	}
	for i, d := range p.Decisions {
		w := fmt.Sprintf("decision %d", i+1)
		switch {
		case d.Repo == "" && len(p.Repos) > 1:
			c.add(w, "needs repo (the name of the repo it belongs to)")
		case d.Repo != "" && !declared[d.Repo]:
			c.add(w, "repo %q is not one of the plan's repos", d.Repo)
		}
	}
	for i, b := range p.Batches {
		w := fmt.Sprintf("batch %d", i+1)
		seen := ""
		for j, ch := range b.Change {
			name, _, ok := repos.Split(ch.At, names)
			if !ok {
				c.add(fmt.Sprintf("%s change %d", w, j+1), "at %q must start with the repo name and a slash (%s)", ch.At, strings.Join(names, ", "))
				continue
			}
			if seen != "" && seen != name {
				c.add(w, "touches repos %s and %s; a batch touches one repo", seen, name)
			}
			seen = name
		}
	}
	return c.out
}

// Validate returns every problem in p, in a fixed order. No problems means
// the plan is good to save.
func Validate(p domain.Plan) []Problem {
	c := &checker{}

	if strings.TrimSpace(p.Title) == "" {
		c.add("title", "is empty")
	}
	c.short("title", p.Title)

	for i, d := range p.Decisions {
		w := fmt.Sprintf("decision %d", i+1)
		if d.Scope != domain.ScopeRepo && d.Scope != domain.ScopeTask {
			c.add(w, "scope %q must be repo or task", d.Scope)
		}
		c.short(w+" text", d.Text)
		c.short(w+" question", d.Question)
		if d.Scope == domain.ScopeRepo && strings.TrimSpace(d.Question) == "" {
			c.add(w+" question", "a repo decision needs the question it settles (it may become a rule)")
		}
		if d.ID != "" || d.By != "" || d.Dissent != "" {
			c.add(w, "id, by and dissent are not yours to write: nxy sets them")
		}
	}

	criteria := map[string]domain.TestKind{}
	if len(p.Criteria) == 0 {
		c.add("criteria", "need at least one criterion")
	}
	for i, r := range p.Criteria {
		w := r.ID
		if w == "" {
			w = fmt.Sprintf("criterion %d", i+1)
		}
		switch {
		case !ruleIDRe.MatchString(r.ID):
			c.add(w, "id must look like R1, R2, ...")
		case r.ID != fmt.Sprintf("R%d", i+1):
			c.add(w, "ids must be R1..Rn in order; expected R%d", i+1)
		}
		if _, dup := criteria[r.ID]; dup {
			c.add(w, "duplicate criterion id")
		}
		criteria[r.ID] = r.Test
		if strings.TrimSpace(r.Text) == "" {
			c.add(w, "text is empty")
		}
		c.short(w+" text", r.Text)
		switch r.Test {
		case domain.TestTDD, domain.TestExisting, domain.TestBuild:
		case domain.TestManual:
			if strings.TrimSpace(r.Check) == "" {
				c.add(w, "manual criterion needs check (what to look at)")
			}
		default:
			c.add(w, "test %q must be tdd, existing, build or manual", r.Test)
		}
		c.short(w+" check", r.Check)
	}

	covered := map[string]bool{}
	if len(p.Batches) == 0 {
		c.add("batches", "need at least one batch")
	}
	for i, b := range p.Batches {
		w := fmt.Sprintf("batch %d", i+1)
		if b.N != i+1 {
			c.add(w, "n is %d; batches must be numbered 1..n in order", b.N)
		}
		if strings.TrimSpace(b.Title) == "" {
			c.add(w, "title is empty")
		}
		c.short(w+" title", b.Title)

		onlyManual := len(b.Covers) > 0
		for _, id := range b.Covers {
			kind, ok := criteria[id]
			if !ok {
				c.add(w, "covers %s, which is not a criterion", id)
				onlyManual = false
				continue
			}
			covered[id] = true
			if kind != domain.TestManual {
				onlyManual = false
			}
		}
		if len(b.Covers) == 0 {
			c.add(w, "covers no criterion")
		}

		for _, rd := range b.Read {
			c.short(w+" read", rd)
		}
		if len(b.Change) == 0 {
			c.add(w, "needs at least one change")
		}
		for j, ch := range b.Change {
			cw := fmt.Sprintf("%s change %d", w, j+1)
			if !atRe.MatchString(ch.At) {
				c.add(cw, "at %q must be path:line, path:a-b or path (new)", ch.At)
			}
			c.short(cw+" at", ch.At)
			if strings.TrimSpace(ch.What) == "" {
				c.add(cw, "what is empty")
			}
			if n := len([]rune(ch.What)); n > maxWhatLen {
				c.add(cw, "what has %d characters; keep it to %d", n, maxWhatLen)
			}
			c.short(cw+" what", ch.What)
		}

		switch b.Test.Kind {
		case domain.BatchTestNew, domain.BatchTestExisting:
		default:
			c.add(w, "test kind %q must be new or existing", b.Test.Kind)
		}
		if strings.TrimSpace(b.Test.Accept) == "" && !onlyManual {
			c.add(w, "test.accept is required unless the batch only covers manual criteria")
		}
		c.short(w+" test name", b.Test.Name)
		c.short(w+" test accept", b.Test.Accept)

		switch b.Risk {
		case domain.RiskLow, domain.RiskMedium, domain.RiskHigh:
		default:
			c.add(w, "risk %q must be low, medium or high", b.Risk)
		}
		for _, d := range b.Depends {
			if d < 1 || d >= i+1 {
				c.add(w, "depends on batch %d; only earlier batches are allowed", d)
			}
		}
	}

	for _, r := range p.Criteria {
		if r.ID != "" && !covered[r.ID] {
			c.add(r.ID, "no batch covers it")
		}
	}

	c.short("suite", p.Suite)

	for i, r := range p.Risks {
		c.short(fmt.Sprintf("risk %d", i+1), r)
	}

	if len(p.Questions) > maxQuestions {
		c.add("questions", "%d questions; ask at most %d", len(p.Questions), maxQuestions)
	}
	for i, q := range p.Questions {
		w := fmt.Sprintf("question %d", i+1)
		if strings.TrimSpace(q.Question) == "" {
			c.add(w, "question is empty")
		}
		c.short(w+" question", q.Question)
		c.short(w+" header", q.Header)
		if n := len(q.Options); n < minOptions || n > maxOptions {
			c.add(w, "has %d options; it needs %d to %d", n, minOptions, maxOptions)
		}
		rec := 0
		for _, o := range q.Options {
			if o.Recommended {
				rec++
			}
			c.short(w+" option label", o.Label)
			c.short(w+" option description", o.Description)
		}
		if rec != 1 {
			c.add(w, "needs exactly one recommended option; has %d", rec)
		} else if !q.Options[0].Recommended {
			c.add(w, "the recommended option must be the first")
		}
	}

	return c.out
}
