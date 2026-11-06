package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/memory"
	"github.com/mananos/nxy.dev/internal/review"
)

// ReviewStatsDays is the period `nxy review status` looks at.
const ReviewStatsDays = 30

// ReviewShow is the full review of the active feature of dir, or of the latest one; ok is false
// when there is no feature or it has no review yet.
func (h Hooks) ReviewShow(ctx context.Context, dir string) (string, bool, error) {
	if h.Store == nil || h.Repos == nil {
		return "", false, nil
	}
	f, ok := h.featureFor(ctx, HookEvent{ProjectDir: dir})
	var err error
	if !ok || f.Review == nil {
		f, ok, err = h.latestIn(ctx, dir)
	}
	if err != nil || !ok || f.Review == nil {
		return "", false, err
	}
	return review.Markdown(f), true, nil
}

// ReviewStatus is what each lens did for the repo of dir in the last ReviewStatsDays days.
func (h Hooks) ReviewStatus(ctx context.Context, dir string) ([]domain.LensStat, error) {
	if h.Memory == nil || h.Repos == nil {
		return nil, nil
	}
	since := time.Now().AddDate(0, 0, -ReviewStatsDays).UnixMilli()
	return h.Memory.ReviewStats(ctx, h.Repos.Key(dir), since)
}

// EscapeResult is what ReviewEscape did: with an active feature it raised a candidate for the
// closing question (Candidate, FeatureID); without one it saved Rule right away, and Similar is
// the existing rule it resembles, if any.
type EscapeResult struct {
	Candidate bool
	FeatureID string
	Rule      memory.Rule
	Similar   *memory.Rule
}

// ReviewEscape records a problem the review missed. The question is required: a rule needs it.
func (h Hooks) ReviewEscape(ctx context.Context, dir, text, question, lens string) (EscapeResult, error) {
	if h.Memory == nil || h.Repos == nil {
		return EscapeResult{}, errors.New("no memory available")
	}
	if lens == "" {
		lens = "base"
	}
	text, question = strings.TrimSpace(text), strings.TrimSpace(question)
	if err := memory.ValidateRule(memory.Rule{Text: text, Question: question, Origin: memory.OriginReview}); err != nil {
		return EscapeResult{}, err
	}
	repo := h.Repos.Key(dir)
	f, err := h.addRuleCandidate(ctx, dir, text, question, memory.OriginReview, lens)
	if err == nil {
		if err := h.Memory.RecordEscape(ctx, f.ID, repo, lens, text); err != nil {
			return EscapeResult{}, err
		}
		return EscapeResult{Candidate: true, FeatureID: f.ID}, nil
	}
	// With the closing question already open the escape is saved now, as with no active feature.
	if !errors.Is(err, ErrNoActiveFeature) && !errors.Is(err, errRulesClosed) {
		return EscapeResult{}, err
	}
	var similar *memory.Rule
	if sim, serr := h.Memory.SimilarRules(ctx, repo, text, 1); serr == nil && len(sim) > 0 {
		similar = &sim[0]
	}
	r, err := h.Memory.AddRule(ctx, memory.Rule{Repo: repo, Text: text, Question: question, Origin: memory.OriginReview})
	if err != nil {
		return EscapeResult{}, err
	}
	if err := h.Memory.RecordEscape(ctx, "", repo, lens, text); err != nil {
		return EscapeResult{}, err
	}
	return EscapeResult{Rule: r, Similar: similar}, nil
}
