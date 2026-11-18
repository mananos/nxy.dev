package plan

import (
	"fmt"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

// Checkpoint is what the user needs to decide: criteria and batches with what
// changes, the test, the command nxy runs for it (Accept) and the risk. It leaves
// out the request and Read. The main thread copies it verbatim into the chat.
func Checkpoint(p domain.Plan) string {
	return checkpoint(p, true)
}

// CheckpointOf is the checkpoint of a feature: in engineer mode the criteria were
// already approved when the design closed, so it shows only the batches (and what
// the planner chose on its own); in delegate mode it is Checkpoint.
func CheckpointOf(f domain.Feature) string {
	p := domain.Plan{}
	if f.Plan != nil {
		p = *f.Plan
	}
	if f.Mode != domain.ModeEngineer {
		return checkpoint(p, true)
	}
	return checkpoint(p, false)
}

func checkpoint(p domain.Plan, withCriteria bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n", p.Title)
	writeRepos(&b, p)
	if withCriteria {
		writeCriteria(&b, p.Criteria)
	} else {
		var chosen []domain.Decision
		for _, d := range p.Decisions {
			if d.By == domain.ByPlanner {
				chosen = append(chosen, d)
			}
		}
		if len(chosen) > 0 {
			b.WriteString("### Decisiones que eligió el planner\n\n")
			for _, d := range chosen {
				fmt.Fprintf(&b, "- %s%s %s\n", repoTag(d.Repo), d.ID, d.Text)
			}
		}
	}
	for _, bt := range p.Batches {
		fmt.Fprintf(&b, "\n### Lote %d — %s%s\n", bt.N, repoTag(bt.Repo), bt.Title)
		fmt.Fprintf(&b, "- Cubre: %s\n", strings.Join(bt.Covers, ", "))
		b.WriteString("- Cambiar:\n")
		for _, c := range bt.Change {
			fmt.Fprintf(&b, "  - %s — %s\n", c.At, c.What)
		}
		fmt.Fprintf(&b, "- Test: %s\n", testLine(bt.Test))
		if bt.Test.Accept != "" {
			fmt.Fprintf(&b, "- Accept: `%s`\n", bt.Test.Accept)
		}
		b.WriteString(riskLine(bt))
	}
	writeSuite(&b, p)
	return b.String()
}

// repoTag is "[repo] " for a multi-repo item, empty otherwise.
func repoTag(name string) string {
	if name == "" {
		return ""
	}
	return "[" + name + "] "
}

// writeRepos is the repos line of a multi-repo plan: name — role — path.
func writeRepos(b *strings.Builder, p domain.Plan) {
	if len(p.Repos) == 0 {
		return
	}
	b.WriteString("### Repos\n\n")
	for _, r := range p.Repos {
		name := r.Name
		if name == "" {
			name = r.Path
		}
		role := r.Role
		if role == "" {
			role = "sin rol"
		}
		fmt.Fprintf(b, "- %s — %s — %s\n", name, role, r.Path)
	}
	b.WriteString("\n")
}

// riskLine is the risk of a batch, with why nxy raised it when it did.
func riskLine(bt domain.Batch) string {
	r := riskEs(bt.Risk)
	if bt.RiskWhy != "" {
		return fmt.Sprintf("- Riesgo: %s (subido por nxy: %s)\n", r, bt.RiskWhy)
	}
	return fmt.Sprintf("- Riesgo: %s\n", r)
}

func riskEs(r domain.Risk) string {
	switch r {
	case domain.RiskLow:
		return "bajo"
	case domain.RiskMedium:
		return "medio"
	case domain.RiskHigh:
		return "alto"
	}
	return string(r)
}

func writeSuite(b *strings.Builder, p domain.Plan) {
	if p.Suite != "" {
		fmt.Fprintf(b, "\n- Suite (sólo si no se pueden calcular los afectados): `%s`\n", p.Suite)
	}
	if p.RepoRunnersWarn != "" {
		fmt.Fprintf(b, "\n- Runners del repo: %s\n", p.RepoRunnersWarn)
	}
	for i, r := range p.RepoRunners {
		if i == 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(b, "- Runners del repo (.nxy/runners.json): `%s` → `%s`\n", r.Name, r.Command)
	}
}

// Markdown is the whole plan document of the feature (14.13).
func Markdown(f domain.Feature) string {
	var b strings.Builder
	p := domain.Plan{}
	if f.Plan != nil {
		p = *f.Plan
	}
	fmt.Fprintf(&b, "## %s\n\n", p.Title)
	b.WriteString("### Pedido (literal)\n\n")
	for _, ln := range strings.Split(strings.ReplaceAll(f.Request, "\r\n", "\n"), "\n") {
		b.WriteString("> " + ln + "\n")
	}
	if len(p.Repos) > 0 {
		b.WriteString("\n")
		writeRepos(&b, p)
	}
	if f.Mode == domain.ModeEngineer {
		writeDesign(&b, f, p)
	} else if len(p.Decisions) > 0 {
		b.WriteString("\n### Decisiones\n\n")
		for _, d := range p.Decisions {
			fmt.Fprintf(&b, "- [%s] %s%s\n", d.Scope, repoTag(d.Repo), d.Text)
		}
	}
	b.WriteString("\n")
	writeCriteria(&b, p.Criteria)
	for _, bt := range p.Batches {
		fmt.Fprintf(&b, "\n### Lote %d — %s%s\n", bt.N, repoTag(bt.Repo), bt.Title)
		fmt.Fprintf(&b, "- Cubre: %s\n", strings.Join(bt.Covers, ", "))
		if len(bt.Depends) > 0 {
			ds := make([]string, len(bt.Depends))
			for i, d := range bt.Depends {
				ds[i] = fmt.Sprint(d)
			}
			fmt.Fprintf(&b, "- Depende de: %s\n", strings.Join(ds, ", "))
		}
		if len(bt.Read) > 0 {
			fmt.Fprintf(&b, "- Leer: %s\n", strings.Join(bt.Read, ", "))
		}
		b.WriteString("- Cambiar:\n")
		for _, c := range bt.Change {
			fmt.Fprintf(&b, "  - %s — %s\n", c.At, c.What)
		}
		fmt.Fprintf(&b, "- Test: %s\n", testLine(bt.Test))
		if bt.Test.Accept != "" {
			fmt.Fprintf(&b, "- Accept: `%s`\n", bt.Test.Accept)
		}
		b.WriteString(riskLine(bt))
	}
	writeSuite(&b, p)
	if len(p.Risks) > 0 {
		b.WriteString("\n### Riesgos\n\n")
		for _, r := range p.Risks {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}
	return b.String()
}

// writeDesign is the "Diseño" section of an engineer plan document. It uses only
// domain types (design imports plan, not the other way around). D2 is source only.
func writeDesign(b *strings.Builder, f domain.Feature, p domain.Plan) {
	if len(p.Decisions) == 0 && (f.Design == nil || (f.Design.D2 == "" && len(f.Design.Agenda) == 0)) {
		return
	}
	b.WriteString("\n### Diseño\n\n")
	for _, d := range p.Decisions {
		fmt.Fprintf(b, "- %s%s %s", repoTag(d.Repo), d.ID, d.Text)
		if d.Why != "" {
			fmt.Fprintf(b, " — %s", d.Why)
		}
		if d.By == domain.ByPlanner {
			b.WriteString(" (la eligió el planner)")
		}
		b.WriteString("\n")
		if d.Dissent != "" {
			fmt.Fprintf(b, "  - Claude mantiene su desacuerdo: %s\n", d.Dissent)
		}
	}
	if f.Design == nil {
		return
	}
	for _, fk := range f.Design.Agenda {
		if fk.Status == domain.ForkDelegated && !plannerChose(p, fk.ID) {
			fmt.Fprintf(b, "- %s %s — la eligió el planner\n", fk.ID, fk.Title)
		}
	}
	if f.Design.D2 != "" {
		b.WriteString("\n```d2\n" + strings.TrimRight(f.Design.D2, "\r\n") + "\n```\n")
	}
}

func plannerChose(p domain.Plan, fork string) bool {
	for _, d := range p.Decisions {
		if d.Fork == fork && d.By == domain.ByPlanner {
			return true
		}
	}
	return false
}

func writeCriteria(b *strings.Builder, criteria []domain.Criterion) {
	b.WriteString("### Criterios\n\n")
	for _, r := range criteria {
		fmt.Fprintf(b, "- %s: %s (%s)", r.ID, r.Text, r.Test)
		if r.Test == domain.TestManual && r.Check != "" {
			fmt.Fprintf(b, " — mirar: %s", r.Check)
		}
		b.WriteString("\n")
	}
}

func testLine(t domain.BatchTest) string {
	kind := "existente"
	if t.Kind == domain.BatchTestNew {
		kind = "nuevo"
	}
	if t.Name == "" {
		return kind
	}
	return fmt.Sprintf("%s (%s)", t.Name, kind)
}
