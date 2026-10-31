package plan

import (
	"fmt"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
)

// Checkpoint is what the user needs to decide: rules and batches with what
// changes, the test, the command nxy runs for it (Accept) and the risk. It leaves
// out the request and Read. The main thread copies it verbatim into the chat.
func Checkpoint(p domain.Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n", p.Title)
	writeRules(&b, p.Rules)
	for _, bt := range p.Batches {
		fmt.Fprintf(&b, "\n### Lote %d — %s\n", bt.N, bt.Title)
		fmt.Fprintf(&b, "- Cubre: %s\n", strings.Join(bt.Covers, ", "))
		b.WriteString("- Cambiar:\n")
		for _, c := range bt.Change {
			fmt.Fprintf(&b, "  - %s — %s\n", c.At, c.What)
		}
		fmt.Fprintf(&b, "- Test: %s\n", testLine(bt.Test))
		if bt.Test.Accept != "" {
			fmt.Fprintf(&b, "- Accept: `%s`\n", bt.Test.Accept)
		}
		fmt.Fprintf(&b, "- Riesgo: %s\n", bt.Risk)
	}
	return b.String()
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
	if len(p.Decisions) > 0 {
		b.WriteString("\n### Decisiones\n\n")
		for _, d := range p.Decisions {
			fmt.Fprintf(&b, "- [%s] %s\n", d.Scope, d.Text)
		}
	}
	b.WriteString("\n")
	writeRules(&b, p.Rules)
	for _, bt := range p.Batches {
		fmt.Fprintf(&b, "\n### Lote %d — %s\n", bt.N, bt.Title)
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
		fmt.Fprintf(&b, "- Riesgo: %s\n", bt.Risk)
	}
	if len(p.Risks) > 0 {
		b.WriteString("\n### Riesgos\n\n")
		for _, r := range p.Risks {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}
	return b.String()
}

func writeRules(b *strings.Builder, rules []domain.Rule) {
	b.WriteString("### Reglas\n\n")
	for _, r := range rules {
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
