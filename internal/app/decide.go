package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

var (
	// ErrAskAnswered means the held question already has an answer.
	ErrAskAnswered = errors.New("ya contestada")
	// ErrAskGone means nothing is waiting for the question anymore.
	ErrAskGone = errors.New("ya no está esperando: contestá en la terminal de Claude Code")
)

// Decide answers a question held by the hook: one answer per question, in order, each a label of its
// options (a multiSelect one may carry several, separated by commas). With release the question goes
// back to the terminal dialog and answers is ignored.
func (h Hooks) Decide(ctx context.Context, askID string, answers []string, release bool) error {
	if h.Asks == nil {
		return ErrAskGone
	}
	row, err := h.Asks.PendingAsk(ctx, askID)
	if err != nil {
		return ErrAskGone
	}
	if row.Answers != nil {
		return ErrAskAnswered
	}
	var stored []string
	by := answeredByTerminal
	if !release {
		by = answeredByTUI
		if len(answers) != len(row.Questions) {
			return fmt.Errorf("la pregunta tiene %d respuestas y llegaron %d", len(row.Questions), len(answers))
		}
		for i, q := range row.Questions {
			a, err := checkAnswer(q.Options, q.MultiSelect, answers[i])
			if err != nil {
				return err
			}
			stored = append(stored, a)
		}
	}
	ok, err := h.Asks.AnswerPendingAsk(ctx, askID, stored, by)
	if err != nil {
		return err
	}
	if !ok {
		return ErrAskAnswered
	}
	return nil
}

// checkAnswer validates one answer against the labels and returns it as AskUserQuestion stores it.
func checkAnswer(options []string, multi bool, answer string) (string, error) {
	answer = strings.TrimSpace(answer)
	if slices.Contains(options, answer) {
		return answer, nil
	}
	if multi {
		// Labels may hold commas themselves, so recognise them (longest first) instead of splitting.
		sorted := slices.Clone(options)
		slices.SortStableFunc(sorted, func(a, b string) int { return len(b) - len(a) })
		var parts []string
		rest := answer
		trailing := false
		for rest != "" {
			found := false
			for _, o := range sorted {
				if o == "" {
					continue
				}
				after, ok := strings.CutPrefix(rest, o)
				if !ok {
					continue
				}
				after = strings.TrimLeft(after, " ")
				if after != "" && !strings.HasPrefix(after, ",") {
					continue
				}
				parts = append(parts, o)
				trailing = strings.HasPrefix(after, ",")
				rest = strings.TrimLeft(strings.TrimPrefix(after, ","), " ")
				found = true
				break
			}
			if !found {
				bad, _, _ := strings.Cut(rest, ",")
				return "", fmt.Errorf("%q no es una opción de la pregunta", strings.TrimSpace(bad))
			}
		}
		if len(parts) == 0 || trailing {
			return "", fmt.Errorf("%q no es una opción de la pregunta", answer)
		}
		return strings.Join(parts, ", "), nil
	}
	return "", fmt.Errorf("%q no es una opción de la pregunta", answer)
}
