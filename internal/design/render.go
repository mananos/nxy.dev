package design

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"text/template"
	"unicode/utf8"

	"github.com/mananos/nxy.dev/internal/domain"
)

//go:embed prompts/engineer.md
var engineerTemplate string

// Caps for the user's messages the guide carries after a /clear.
const (
	maxRefs     = 10
	maxRefRunes = 300
)

var engineerTmpl = template.Must(template.New("engineer").Parse(engineerTemplate))

type guideData struct {
	Feature domain.Feature
	Exe     string
	Agenda  string
}

// Guide is the instruction for the main thread while the feature is in Diseñando.
func Guide(f domain.Feature, exe string) string {
	d := guideData{Feature: f, Exe: strings.ReplaceAll(exe, `\`, "/")}
	if f.Design != nil {
		d.Agenda = Agenda(f.Design) + pendingState(f.Design)
	}
	var b bytes.Buffer
	if err := engineerTmpl.Execute(&b, d); err != nil {
		return "nxy: engineer guide failed: " + err.Error()
	}
	return strings.TrimRight(b.String(), "\n")
}

// pendingState lists the saved decisions and what waits for the user's answer,
// so the guide still carries them after a /clear.
func pendingState(d *domain.Design) string {
	var b strings.Builder
	if len(d.Decisions) > 0 {
		b.WriteString("\n\nDecisiones guardadas")
		for _, x := range d.Decisions {
			fmt.Fprintf(&b, "\n- %s %s — %s", x.ID, x.Text, x.Why)
			if x.Dissent != "" {
				fmt.Fprintf(&b, " (Claude mantiene su desacuerdo: %s)", x.Dissent)
			}
		}
	}
	if len(d.Refs) > 0 {
		refs := d.Refs
		if len(refs) > maxRefs {
			refs = refs[len(refs)-maxRefs:]
		}
		b.WriteString("\n\nÚltimos mensajes del usuario en este diseño (referencia, no los vuelvas a pedir)")
		for _, r := range refs {
			r = strings.Join(strings.Fields(r), " ")
			if utf8.RuneCountInString(r) > maxRefRunes {
				r = string([]rune(r)[:maxRefRunes]) + "…"
			}
			b.WriteString("\n- " + r)
		}
	}
	if d.Pending != nil {
		b.WriteString("\n\nPropuesta esperando respuesta del usuario\n" + Proposal(d))
	}
	if d.Close != nil {
		b.WriteString("\n\nCierre esperando respuesta del usuario")
		for _, r := range d.Close.Rules {
			fmt.Fprintf(&b, "\n- %s %s", r.ID, r.Text)
		}
	}
	return b.String()
}

var forkMark = map[domain.ForkStatus]string{
	domain.ForkOpen:      "abierta",
	domain.ForkDecided:   "decidida",
	domain.ForkDelegated: "delegada",
}

// Agenda lists the forks with how many are decided.
func Agenda(d *domain.Design) string {
	if d == nil || len(d.Agenda) == 0 {
		return "Agenda: todavía sin bifurcaciones."
	}
	done := 0
	for _, f := range d.Agenda {
		if f.Status != domain.ForkOpen {
			done++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Agenda: %d de %d decididas", done, len(d.Agenda))
	for _, f := range d.Agenda {
		fmt.Fprintf(&b, "\n- %s %s (%s)", f.ID, f.Title, forkMark[f.Status])
	}
	return b.String()
}

// Proposal shows the pending decision with its why and Claude's dissent.
func Proposal(d *domain.Design) string {
	if d == nil || d.Pending == nil {
		return ""
	}
	p := d.Pending
	var b strings.Builder
	fmt.Fprintf(&b, "Decisión D%d (%s): %s\nPor qué: %s", len(d.Decisions)+1, p.Fork, p.Text, p.Why)
	if p.Dissent != "" {
		fmt.Fprintf(&b, "\nClaude mantiene su desacuerdo: %s", p.Dissent)
	}
	return b.String()
}

// CloseSummary is what the user approves when closing the design.
func CloseSummary(f domain.Feature) string {
	d := f.Design
	if d == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("Decisiones")
	if len(d.Decisions) == 0 {
		b.WriteString("\n- (ninguna)")
	}
	for _, x := range d.Decisions {
		fmt.Fprintf(&b, "\n- %s %s — %s", x.ID, x.Text, x.Why)
		if x.Dissent != "" {
			fmt.Fprintf(&b, " (Claude mantiene su desacuerdo: %s)", x.Dissent)
		}
	}
	rules, d2, delegate := d.Rules, d.D2, d.Delegate
	if d.Close != nil {
		rules, d2, delegate = d.Close.Rules, d.Close.D2, d.Close.Delegate
	}
	b.WriteString("\n\nReglas")
	for _, r := range rules {
		fmt.Fprintf(&b, "\n- %s %s", r.ID, r.Text)
	}
	if strings.TrimSpace(d2) != "" {
		fmt.Fprintf(&b, "\n\nD2 (fuente, sin render)\n```d2\n%s\n```", strings.TrimSpace(d2))
	}
	var deleg []string
	for _, fk := range d.Agenda {
		if fk.Status == domain.ForkDelegated || (delegate && fk.Status == domain.ForkOpen) {
			deleg = append(deleg, fk.ID+" "+fk.Title)
		}
	}
	if len(deleg) > 0 {
		b.WriteString("\n\nLas elige el planner")
		for _, s := range deleg {
			b.WriteString("\n- " + s)
		}
	}
	return b.String()
}

// Status is the short line for Context after each step of the debate.
func Status(f domain.Feature) string {
	d := f.Design
	if d == nil {
		return ""
	}
	open := 0
	for _, fk := range d.Agenda {
		if fk.Status == domain.ForkOpen {
			open++
		}
	}
	return fmt.Sprintf("nxy feature %s: %d decisiones guardadas, %d bifurcaciones abiertas.", f.ID, len(d.Decisions), open)
}
