package review

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

const (
	// MaxGroups and MaxOptions are the limits of one AskUserQuestion call.
	MaxGroups  = 4
	MaxOptions = 4
	// NoneLabel is the extra option of a group with a single finding.
	NoneLabel = "Ninguno"
)

// CriterionLine shows the criterion checks of the review: "R1 ✔ · R2 ✘". A criterion the base
// pass did not answer shows "?".
func CriterionLine(rv domain.Review, criteria []domain.Criterion) string {
	answered := map[string]domain.CriterionCheck{}
	for _, rc := range rv.Criteria {
		answered[rc.ID] = rc
	}
	ids := make([]string, 0, len(criteria))
	for _, r := range criteria {
		ids = append(ids, r.ID)
	}
	if len(criteria) == 0 {
		for _, rc := range rv.Criteria {
			ids = append(ids, rc.ID)
		}
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		mark := "?"
		if rc, ok := answered[id]; ok {
			mark = "✘"
			if rc.OK {
				mark = "✔"
			}
		}
		parts = append(parts, id+" "+mark)
	}
	return strings.Join(parts, " · ")
}

func criteriaOf(f domain.Feature) []domain.Criterion {
	if f.Plan == nil {
		return nil
	}
	return f.Plan.Criteria
}

func where(f domain.Finding) string {
	if f.File == "" {
		return ""
	}
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d", f.File, f.Line)
	}
	return f.File
}

func sevEs(s domain.Severity) string {
	switch s {
	case domain.SeverityHigh:
		return "alta"
	case domain.SeverityMedium:
		return "media"
	}
	return "baja"
}

func writeFinding(b *strings.Builder, f domain.Finding) {
	fmt.Fprintf(b, "- %s [%s · %s] %s", f.ID, f.Lens, sevEs(f.Severity), f.Title)
	if w := where(f); w != "" {
		fmt.Fprintf(b, " (%s)", w)
	}
	b.WriteString("\n")
	if f.Scenario != "" {
		fmt.Fprintf(b, "  - Escenario: %s\n", f.Scenario)
	}
	if f.Why != "" {
		fmt.Fprintf(b, "  - Por qué: %s\n", f.Why)
	}
	if f.Lesson != "" {
		fmt.Fprintf(b, "  - Regla general: %s\n", f.Lesson)
	}
	if f.Fix != "" {
		fmt.Fprintf(b, "  - Arreglo: %s\n", f.Fix)
	}
}

// Checkpoint is what the user sees at the second checkpoint: the rule line, the
// offered findings with scenario and fix, the findings that were already there
// (informational) and how many were dropped or refuted. The main thread copies it
// verbatim into the chat.
func Checkpoint(f domain.Feature) string {
	rv := domain.Review{}
	if f.Review != nil {
		rv = *f.Review
	}
	var b strings.Builder
	b.WriteString("## Review\n\n")
	if rv.Warning != "" {
		fmt.Fprintf(&b, "Aviso: %s\n\n", rv.Warning)
	}
	if line := CriterionLine(rv, criteriaOf(f)); line != "" {
		fmt.Fprintf(&b, "Criterios: %s\n\n", line)
	}
	if f.Mode == domain.ModeEngineer && len(rv.Decisions) > 0 {
		b.WriteString("### Diff por decisión\n\n")
		text := map[string]string{}
		for _, d := range DesignDecisions(f) {
			text[d.ID] = d.Text
		}
		for _, dc := range rv.Decisions {
			fmt.Fprintf(&b, "- %s", dc.ID)
			if t := text[dc.ID]; t != "" {
				fmt.Fprintf(&b, " %s", t)
			}
			switch {
			case dc.File != "" && dc.Line > 0:
				fmt.Fprintf(&b, " → %s:%d", dc.File, dc.Line)
			case dc.File != "":
				fmt.Fprintf(&b, " → %s", dc.File)
			default:
				b.WriteString(" → fuera del diff")
			}
			if dc.How != "" {
				fmt.Fprintf(&b, " — %s", dc.How)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	offered := Offered(rv)
	if len(offered) == 0 {
		b.WriteString("Sin hallazgos para arreglar.\n")
	} else {
		b.WriteString("### Hallazgos de este cambio\n\n")
		for _, fd := range offered {
			writeFinding(&b, fd)
		}
	}
	var pre []domain.Finding
	dropped, refuted := 0, 0
	for _, fd := range rv.Findings {
		switch {
		case fd.Dropped != "":
			dropped++
		case fd.Verdict == domain.VerdictRefuted:
			refuted++
		case fd.Cause != domain.CauseChange:
			pre = append(pre, fd)
		}
	}
	if len(pre) > 0 {
		b.WriteString("\n### Ya estaban (sólo informativo)\n\n")
		for _, fd := range pre {
			writeFinding(&b, fd)
		}
	}
	if dropped > 0 || refuted > 0 {
		var bits []string
		if dropped > 0 {
			bits = append(bits, fmt.Sprintf("%d descartados por falta de precisión", dropped))
		}
		if refuted > 0 {
			bits = append(bits, fmt.Sprintf("%d refutados", refuted))
		}
		fmt.Fprintf(&b, "\n%s.", strings.Join(bits, ", "))
		if rv.FullPath != "" {
			fmt.Fprintf(&b, " Ver completo: %s", rv.FullPath)
		}
		b.WriteString("\n")
	} else if rv.FullPath != "" {
		fmt.Fprintf(&b, "\nVer completo: %s\n", rv.FullPath)
	}
	return b.String()
}

// Option is one choice of a question.
type Option struct {
	Label       string
	Description string
}

// Group is one multiSelect question of the findings checkpoint.
type Group struct {
	Header  string
	Options []Option
}

// Shown cuts the offered findings to the ones that fit in one AskUserQuestion call
// (MaxGroups x MaxOptions). left is how many did not fit.
func Shown(offered []domain.Finding) (shown []domain.Finding, left int) {
	max := MaxGroups * MaxOptions
	if len(offered) > max {
		return offered[:max], len(offered) - max
	}
	return offered, 0
}

// Groups splits the offered findings in at most MaxGroups questions of at most
// MaxOptions options. A group with one finding gets a "Ninguno" option so it can
// be answered with nothing. left is how many findings did not fit.
func Groups(offered []domain.Finding) (groups []Group, left int) {
	shown, left := Shown(offered)
	n := (len(shown) + MaxOptions - 1) / MaxOptions
	for i := 0; i < n; i++ {
		end := (i + 1) * MaxOptions
		if end > len(shown) {
			end = len(shown)
		}
		g := Group{Header: fmt.Sprintf("Arreglos %d/%d", i+1, n)}
		for _, fd := range shown[i*MaxOptions : end] {
			desc := sevEs(fd.Severity)
			if w := where(fd); w != "" {
				desc += " · " + w
			}
			g.Options = append(g.Options, Option{Label: fd.ID + " " + fd.Title, Description: desc})
		}
		if len(g.Options) == 1 {
			g.Options = append(g.Options, Option{Label: NoneLabel, Description: "No arreglar este"})
		}
		groups = append(groups, g)
	}
	return groups, left
}

// FixQuestionPrefix starts the text of every findings question of a feature.
func FixQuestionPrefix(id string) string {
	return fmt.Sprintf("nxy feature %s: ¿qué hallazgos arreglamos?", id)
}

// FixQuestion is the exact text of one findings question (part of parts).
func FixQuestion(id string, part, parts int) string {
	return fmt.Sprintf("%s (%d/%d)", FixQuestionPrefix(id), part, parts)
}

var findingIDRE = regexp.MustCompile(`(?i)\bH(\d+)\b`)

// ChosenIDs reads the finding ids (H1, H3) out of an answer, in order, once each.
func ChosenIDs(answer string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range findingIDRE.FindAllStringSubmatch(answer, -1) {
		id := "H" + m[1]
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
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

// Markdown is the whole review: every finding with why it was dropped or what
// the refuter said.
func Markdown(f domain.Feature) string {
	rv := domain.Review{}
	if f.Review != nil {
		rv = *f.Review
	}
	var b strings.Builder
	b.WriteString("## Review completa\n\n")
	fmt.Fprintf(&b, "Riesgo: %s", riskEs(rv.Risk))
	if rv.RiskWhy != "" {
		fmt.Fprintf(&b, " (%s)", rv.RiskWhy)
	}
	b.WriteString("\n")
	if len(rv.Lenses) > 0 {
		names := make([]string, len(rv.Lenses))
		for i, l := range rv.Lenses {
			names[i] = l.Name
		}
		fmt.Fprintf(&b, "Lentes: %s\n", strings.Join(names, ", "))
	}
	b.WriteString("\n### Criterios\n\n")
	answered := map[string]domain.CriterionCheck{}
	for _, rc := range rv.Criteria {
		answered[rc.ID] = rc
	}
	for _, r := range criteriaOf(f) {
		mark := "?"
		why := ""
		if rc, ok := answered[r.ID]; ok {
			mark = "✘"
			if rc.OK {
				mark = "✔"
			}
			why = rc.Why
		}
		fmt.Fprintf(&b, "- %s %s: %s", r.ID, mark, r.Text)
		if why != "" {
			fmt.Fprintf(&b, " — %s", why)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n### Hallazgos\n\n")
	if len(rv.Findings) == 0 {
		b.WriteString("Ninguno.\n")
	}
	for _, fd := range rv.Findings {
		writeFinding(&b, fd)
		if fd.Cause == domain.CausePreexisting {
			b.WriteString("  - Ya estaba antes del cambio\n")
		}
		if fd.Evidence != "" {
			fmt.Fprintf(&b, "  - Evidencia: %s\n", fd.Evidence)
		}
		if fd.Dropped != "" {
			fmt.Fprintf(&b, "  - Descartado: %s\n", fd.Dropped)
		}
		if fd.Verdict != "" {
			fmt.Fprintf(&b, "  - Refutador: %s", fd.Verdict)
			if fd.VerdictWhy != "" {
				fmt.Fprintf(&b, " — %s", fd.VerdictWhy)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// criterionProof is how a criterion was proved: the name of the test of the batch that
// covers it, or its kind (manual, build).
func criterionProof(p *domain.Plan, r domain.Criterion) string {
	if p != nil && r.Test != domain.TestManual {
		for _, bt := range p.Batches {
			for _, c := range bt.Covers {
				if c == r.ID && bt.Test.Name != "" {
					return bt.Test.Name
				}
			}
		}
	}
	return string(r.Test)
}

// Summary is the "Listo" line, which starts with the word "Listo": the criteria that held with how each was proved,
// the criteria that did not, and the fixes applied from the review.
func Summary(f domain.Feature) string {
	rv := domain.Review{}
	if f.Review != nil {
		rv = *f.Review
	}
	answered := map[string]domain.CriterionCheck{}
	for _, rc := range rv.Criteria {
		answered[rc.ID] = rc
	}
	var parts []string
	for _, r := range criteriaOf(f) {
		rc, ok := answered[r.ID]
		switch {
		case !ok:
			parts = append(parts, r.ID+" ?")
		case rc.OK:
			parts = append(parts, r.ID+" ✔ "+criterionProof(f.Plan, r))
		default:
			parts = append(parts, r.ID+" ✘")
		}
	}
	var b strings.Builder
	line := func(s string) {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(s)
	}
	b.WriteString(strings.Join(append([]string{"Listo"}, parts...), " · "))
	if f.Review == nil || len(rv.Criteria) == 0 {
		line("La review no corrió: los criterios quedan sin chequear (?).")
	}
	if rv.Warning != "" {
		line("Aviso: " + rv.Warning)
	}
	unverifiedFixes := false
	if a := f.Affected; a != nil {
		switch {
		case a.Failure != "":
			line("Afectados en rojo (aceptados con Seguir).")
			unverifiedFixes = true
		case a.Reason != "":
			line("Afectados sin verificar: " + a.Reason)
			unverifiedFixes = true
		case len(a.Runs) == 0:
			unverifiedFixes = true
		}
	} else {
		unverifiedFixes = true
	}
	if len(rv.Chosen) > 0 {
		chosen := map[string]bool{}
		for _, id := range rv.Chosen {
			chosen[id] = true
		}
		var fixed []string
		for _, fd := range rv.Findings {
			if chosen[fd.ID] {
				fixed = append(fixed, fd.ID+" "+fd.Title)
			}
		}
		if len(fixed) > 0 {
			label := "Arreglos aplicados"
			if unverifiedFixes {
				label = "Arreglos pedidos (sin verificar)"
			}
			line(fmt.Sprintf("%s: %s", label, strings.Join(fixed, "; ")))
		}
	}
	return b.String()
}
