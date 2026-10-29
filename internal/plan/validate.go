package plan

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
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
type Problem struct {
	Where string
	Msg   string
}

func (p Problem) String() string { return p.Where + ": " + p.Msg }

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
	}

	rules := map[string]domain.TestKind{}
	if len(p.Rules) == 0 {
		c.add("rules", "need at least one rule")
	}
	for i, r := range p.Rules {
		w := r.ID
		if w == "" {
			w = fmt.Sprintf("rule %d", i+1)
		}
		switch {
		case !ruleIDRe.MatchString(r.ID):
			c.add(w, "id must look like R1, R2, ...")
		case r.ID != fmt.Sprintf("R%d", i+1):
			c.add(w, "ids must be R1..Rn in order; expected R%d", i+1)
		}
		if _, dup := rules[r.ID]; dup {
			c.add(w, "duplicate rule id")
		}
		rules[r.ID] = r.Test
		if strings.TrimSpace(r.Text) == "" {
			c.add(w, "text is empty")
		}
		c.short(w+" text", r.Text)
		switch r.Test {
		case domain.TestTDD, domain.TestExisting, domain.TestBuild:
		case domain.TestManual:
			if strings.TrimSpace(r.Check) == "" {
				c.add(w, "manual rule needs check (what to look at)")
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
			kind, ok := rules[id]
			if !ok {
				c.add(w, "covers %s, which is not a rule", id)
				onlyManual = false
				continue
			}
			covered[id] = true
			if kind != domain.TestManual {
				onlyManual = false
			}
		}
		if len(b.Covers) == 0 {
			c.add(w, "covers no rule")
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
			c.add(w, "test.accept is required unless the batch only covers manual rules")
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

	for _, r := range p.Rules {
		if r.ID != "" && !covered[r.ID] {
			c.add(r.ID, "no batch covers it")
		}
	}

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
