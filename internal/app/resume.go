package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/memory"
)

const (
	maxResumeDecisions = 8
	maxResumeFailure   = 400
	maxResumeLine      = 200
	maxResumeNoteBody  = 600
)

// clip cuts s to max runes, adding an ellipsis.
func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// batchProgress is "lote N de M" while a plan is being implemented, "" otherwise.
func batchProgress(f domain.Feature) string {
	total := f.State.Batches
	if total == 0 && f.Plan != nil {
		total = len(f.Plan.Batches)
	}
	if f.State.Batch == 0 || total == 0 {
		return ""
	}
	return fmt.Sprintf("lote %d de %d", f.State.Batch, total)
}

// lastFailure is what failed last in the feature, "" when nothing did.
func lastFailure(f domain.Feature) string {
	var s string
	switch f.State.Phase {
	case domain.PhaseBatchFailed:
		if f.Proof != nil {
			s = f.Proof.Failure
		}
	case domain.PhaseAffectedFailed, domain.PhaseFixesFailed:
		if f.Affected != nil {
			s = f.Affected.Failure
		}
	}
	return clip(s, maxResumeFailure)
}

// featureDecisions are the decisions of the feature's plan and design, each text once.
func featureDecisions(f domain.Feature) []string {
	var out []string
	seen := map[string]bool{}
	add := func(ds []domain.Decision) {
		for _, d := range ds {
			t := strings.TrimSpace(d.Text)
			if t == "" || seen[t] {
				continue
			}
			seen[t] = true
			out = append(out, clip(t, maxResumeLine))
		}
	}
	if f.Plan != nil {
		add(f.Plan.Decisions)
	}
	if f.Design != nil {
		add(f.Design.Decisions)
	}
	return out
}

// resumeSummary is the "para retomar" text for a feature, built from what the db knows: request, step,
// batch, last failure, decisions and the notes of that feature. otherNotes counts the other notes of the
// folder (they are not asked about while a feature is active).
func resumeSummary(f domain.Feature, notes []memory.Note, otherNotes int) string {
	var b strings.Builder
	first, _, _ := strings.Cut(f.Request, "\n")
	b.WriteString("nxy: para retomar la feature " + f.ID + " — " + clip(first, maxResumeLine) + "\n")
	step := f.State.Phase.Label()
	if p := batchProgress(f); p != "" {
		step += " (" + p + ")"
	}
	if step != "" {
		b.WriteString("Paso: " + step + "\n")
	}
	if f.Paused {
		b.WriteString("La feature está pausada: `nxy feature resume --feature " + f.ID + "` la retoma.\n")
	}
	for _, l := range RepoLines(f) {
		b.WriteString("Repo: " + l + "\n")
	}
	if fail := lastFailure(f); fail != "" {
		b.WriteString("Lo último que falló:\n" + fail + "\n")
	}
	if ds := featureDecisions(f); len(ds) > 0 {
		b.WriteString("Decisiones:\n")
		for i, d := range ds {
			if i == maxResumeDecisions {
				fmt.Fprintf(&b, "- … y %d más\n", len(ds)-i)
				break
			}
			b.WriteString("- " + d + "\n")
		}
	}
	for _, n := range notes {
		fmt.Fprintf(&b, "Nota %s «%s»", n.Short, n.Title)
		if body := clip(n.Body, maxResumeNoteBody); body != "" {
			b.WriteString(":\n" + body)
		}
		b.WriteString("\n")
	}
	if otherNotes > 0 {
		fmt.Fprintf(&b, "Hay %d notas más en esta carpeta: `nxy note list`\n", otherNotes)
	}
	return strings.TrimRight(b.String(), "\n")
}

// resumeContext builds the resume summary of f with the notes read from the db.
func (h Hooks) resumeContext(ctx context.Context, ev HookEvent, f domain.Feature) string {
	var mine []memory.Note
	other := 0
	if h.Memory != nil {
		if all, err := h.Memory.Notes(ctx, sessionFolder(ev), foldNoteFolders, h.now()); err == nil {
			for _, n := range all {
				if n.FeatureID == f.ID {
					mine = append(mine, n)
				} else {
					other++
				}
			}
		}
	}
	return resumeSummary(f, mine, other)
}
