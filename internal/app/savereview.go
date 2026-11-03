package app

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/flow"
	"github.com/mananos/nxy.dev/internal/memory"
	"github.com/mananos/nxy.dev/internal/plan"
	"github.com/mananos/nxy.dev/internal/review"
)

func isReviewerRole(role string) bool {
	return role == roleReviewer || role == roleReviewerQuick
}

// ReviewSaveResult is the outcome of SaveReview: Problems when the submission was refused
// (nothing stored), otherwise how many lenses are saved out of how many.
type ReviewSaveResult struct {
	Problems []plan.Problem
	Saved    int
	Total    int
}

func savedLenses(rv *domain.Review) int {
	n := 0
	for _, l := range rv.Lenses {
		if l.Saved {
			n++
		}
	}
	return n
}

// reviewComplete says every lens saved or every reviewer already stopped.
func reviewComplete(rv *domain.Review) bool {
	return savedLenses(rv) >= len(rv.Lenses) || len(rv.Returned) >= len(rv.Lenses)
}

// reviewInFlight says some dispatched lens is still waiting for its reviewer.
func reviewInFlight(f domain.Feature) bool {
	if f.State.Phase != domain.PhaseReviewing || f.Review == nil {
		return false
	}
	for _, l := range f.Review.Lenses {
		if l.Dispatched && !l.Saved {
			return !reviewComplete(f.Review)
		}
	}
	return false
}

// redeliverPending asks for the dispatch step again when nothing is in flight but some lens was
// never dispatched: otherwise the review would wait forever for reviewers nobody launched.
func redeliverPending(g *domain.Feature) {
	rv := g.Review
	if reviewComplete(rv) || len(pendingLenses(rv)) == 0 {
		return
	}
	for _, l := range rv.Lenses {
		if l.Dispatched && !l.Saved {
			return
		}
	}
	g.Undelivered = true
}

// reviewWait is the line telling the main thread the reviewers are still running.
func reviewWait(f domain.Feature) string {
	return fmt.Sprintf("nxy: feature %s — the reviewers are running (%d of %d lenses saved); wait for them and do not dispatch them again: nxy brings the result when they end.",
		f.ID, savedLenses(f.Review), len(f.Review.Lenses))
}

// pendingLenses are the lenses not yet saved or dispatched.
func pendingLenses(rv *domain.Review) []string {
	var out []string
	for _, l := range rv.Lenses {
		if !l.Saved && !l.Dispatched {
			out = append(out, l.Name)
		}
	}
	return out
}

// reviewDiffs is the diff of the feature since its base; nil when there is no source.
func (h Hooks) reviewDiffs(ctx context.Context, f domain.Feature) []review.FileDiff {
	if h.Diffs == nil {
		return nil
	}
	d, err := h.Diffs.Diff(ctx, f.ProjectDir, f.Base)
	if err != nil {
		return nil
	}
	return d
}

// finishReview puts the lenses together and returns the event that closes the review step.
func finishReview(g *domain.Feature) []flow.Event {
	var criteria []domain.Criterion
	if g.Plan != nil {
		criteria = g.Plan.Criteria
	}
	rv := review.Assemble(*g.Review, criteria)
	g.Review = &rv
	return []flow.Event{{Kind: flow.ReviewDone, Findings: len(review.Offered(rv)) > 0, HighUnproven: review.HighUnproven(rv)}}
}

// preReviewer answers PreToolUse of a reviewer: its prompt must name a pending lens (`feature <id>
// review <lens>`); nxy marks it dispatched and injects the real prompt.
func (h Hooks) preReviewer(ctx context.Context, ev HookEvent, f domain.Feature) HookResponse {
	if f.State.Phase != domain.PhaseReviewing || f.Review == nil {
		return HookResponse{Deny: "out of order: next step is " + f.Next.Label()}
	}
	if quick := ev.Subagent == roleReviewerQuick; quick != f.Review.Quick {
		want := roleReviewer
		if f.Review.Quick {
			want = roleReviewerQuick
		}
		return HookResponse{Deny: "wrong reviewer: this review needs " + want}
	}
	pending := strings.Join(pendingLenses(f.Review), ", ")
	if pending == "" {
		pending = "none"
	}
	words := strings.Fields(ev.AgentPrompt)
	if len(words) != 4 || words[0] != "feature" || words[1] != f.ID || words[2] != "review" {
		return HookResponse{Deny: "reviewer prompt must be exactly `feature " + f.ID + " review <lens>`; pending lenses: " + pending}
	}
	name := words[3]
	var lens review.Lens
	found := false
	for _, l := range h.reviewLenses(f.ProjectDir) {
		if l.Name == name {
			lens, found = l, true
		}
	}
	if !found {
		for _, l := range f.Review.Lenses {
			if l.Name == name {
				lens, found = review.Lens{Name: l.Name, Title: l.Title}, true
			}
		}
	}
	if !found {
		return HookResponse{Deny: "lens " + name + " is not part of this review; pending lenses: " + pending}
	}
	nf, applied, err := h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
		if g.State.Phase != domain.PhaseReviewing || g.Review == nil {
			return false
		}
		for i := range g.Review.Lenses {
			if l := &g.Review.Lenses[i]; l.Name == name && !l.Saved && !l.Dispatched {
				l.Dispatched = true
				return true
			}
		}
		return false
	})
	if err != nil {
		return HookResponse{}
	}
	if !applied {
		return HookResponse{Deny: "lens " + name + " is already dispatched or saved; pending lenses: " + pending}
	}
	diffs := h.reviewDiffs(ctx, nf)
	var paths []string
	for _, d := range diffs {
		paths = append(paths, d.Path)
	}
	if len(paths) == 0 {
		// An empty diff still has files: the ones the feature changed or its plan names.
		paths = h.changedPaths(ctx, nf)
	}
	pkt := h.memoryFor(ctx, nf, roleReviewer, paths)
	{
		// Delivered holds the K and A ids this reviewer saw: rule and false must come from it.
		ks := pkt.Referenced
		if len(ks) > 0 {
			if mf, ok, err := h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
				if g.Review == nil {
					return false
				}
				for i := range g.Review.Lenses {
					if g.Review.Lenses[i].Name == name {
						g.Review.Lenses[i].Delivered = ks
						return true
					}
				}
				return false
			}); err == nil && ok {
				nf = mf
			}
		}
	}
	return HookResponse{Prompt: review.ReviewerPrompt(nf, lens, diffs, h.Exe, pkt.Text)}
}

// reviewMemory stores what a saved review gives the memory: a citation per K rule it named, the
// learnings (their files cut to the diff) and the learnings it found false. A failing store never
// fails the save.
func (h Hooks) reviewMemory(ctx context.Context, f domain.Feature, sub review.Submission, allowed []string) {
	if h.Memory == nil || h.Repos == nil || f.ProjectDir == "" {
		return
	}
	repo := h.Repos.Key(f.ProjectDir)
	now := time.Now().UnixMilli()
	var cited []string
	seen := map[string]bool{}
	for _, fd := range sub.Findings {
		k := strings.ToUpper(strings.TrimSpace(fd.Rule))
		if k != "" && !seen[k] {
			seen[k] = true
			cited = append(cited, k)
		}
	}
	if len(cited) > 0 {
		_, _ = h.Memory.Cite(ctx, repo, cited, now)
	}
	for _, l := range sub.Learnings {
		files := l.Files
		if len(allowed) > 0 {
			files = l.Within(allowed)
		}
		_, _ = h.Memory.AddLearning(ctx, memory.Learning{Repo: repo, Text: strings.TrimSpace(l.Text), Files: files, FeatureID: f.ID})
	}
	if len(sub.False) > 0 {
		var fs []string
		for _, a := range sub.False {
			fs = append(fs, strings.ToUpper(strings.TrimSpace(a)))
		}
		_ = h.Memory.MarkObsolete(ctx, repo, fs, now)
	}
}

// postReviewer answers PostToolUse of a reviewer: it never repeats the step (deliverPending
// brings it once, when the review is complete); until then it tells the main thread to wait.
func (h Hooks) postReviewer(ctx context.Context, ev HookEvent) HookResponse {
	if ev.AgentID != "" {
		return HookResponse{}
	}
	f, ok := h.activeFeature(ctx, ev)
	if !ok || f.State.Phase != domain.PhaseReviewing || f.Review == nil {
		return HookResponse{}
	}
	if ev.AgentAsync {
		return HookResponse{Context: reviewWait(f)}
	}
	return HookResponse{Context: fmt.Sprintf("nxy: feature %s — %d de %d lentes listas; esperá al resto, nxy trae el resultado cuando terminen.",
		f.ID, savedLenses(f.Review), len(f.Review.Lenses))}
}

// reviewerStop counts a reviewer that stopped (saved or not); the last one closes the review.
func (h Hooks) reviewerStop(ctx context.Context, ev HookEvent) HookResponse {
	f, ok := h.activeFeature(ctx, ev)
	if !ok || f.State.Phase != domain.PhaseReviewing || f.Review == nil {
		return HookResponse{}
	}
	_, _, _ = h.advanceSteps(ctx, f.ID, func(g *domain.Feature) ([]flow.Event, error) {
		if g.State.Phase != domain.PhaseReviewing || g.Review == nil {
			return nil, errSkipEvent
		}
		id := ev.AgentID
		if id == "" {
			id = fmt.Sprintf("anon-%d", len(g.Review.Returned))
		}
		seen := false
		for _, r := range g.Review.Returned {
			seen = seen || r == id
		}
		if !seen {
			g.Review.Returned = append(g.Review.Returned, id)
		}
		if !reviewComplete(g.Review) {
			redeliverPending(g)
			return nil, errSkipEvent
		}
		g.Undelivered = true
		return finishReview(g), nil
	})
	return HookResponse{}
}

// SaveReview decodes and validates what a reviewer found for one lens and stores it. An invalid
// submission leaves the store untouched; the last lens closes the review in the same transition.
func (h Hooks) SaveReview(ctx context.Context, featureID, lens string, r io.Reader) (ReviewSaveResult, error) {
	if h.Store == nil {
		return ReviewSaveResult{}, fmt.Errorf("no store")
	}
	f, err := h.Store.Feature(ctx, featureID)
	if err != nil {
		return ReviewSaveResult{}, fmt.Errorf("feature %s: %w", featureID, err)
	}
	if err := lensSavable(f, lens); err != nil {
		return ReviewSaveResult{}, err
	}
	sub, err := review.Decode(r)
	if err != nil {
		return ReviewSaveResult{}, err
	}
	var criteria []domain.Criterion
	if f.Plan != nil {
		criteria = f.Plan.Criteria
	}
	engineer := f.Mode == domain.ModeEngineer
	probs := review.Validate(sub, lens, criteria, lens == review.BaseLens, review.DesignDecisions(f), engineer)
	var delivered []string
	for _, l := range f.Review.Lenses {
		if l.Name == lens {
			delivered = l.Delivered
		}
	}
	probs = append(probs, review.ValidateMemory(sub, delivered)...)
	if len(probs) > 0 {
		return ReviewSaveResult{Problems: probs}, nil
	}
	var diffPaths []string
	for i := range sub.Findings {
		sub.Findings[i].Lens = lens
	}
	// Without a diff nothing can be told apart: every finding counts as caused by the change.
	if diffs := h.reviewDiffs(ctx, f); diffs != nil {
		hunks := map[string][][2]int{}
		for _, d := range diffs {
			hunks[d.Path] = review.Hunks(d.Text)
			diffPaths = append(diffPaths, d.Path)
		}
		sub.Findings = review.Classify(sub.Findings, hunks)
	} else {
		for i := range sub.Findings {
			sub.Findings[i].Cause = domain.CauseChange
		}
	}
	var out domain.Feature
	nf, _, err := h.advanceSteps(ctx, featureID, func(g *domain.Feature) ([]flow.Event, error) {
		if err := lensSavable(*g, lens); err != nil {
			return nil, err
		}
		rv := g.Review
		for i := range rv.Lenses {
			if rv.Lenses[i].Name == lens {
				rv.Lenses[i].Saved = true
			}
		}
		if lens == review.BaseLens {
			rv.Criteria = sub.Criteria
			rv.Decisions = sub.Decisions
		}
		rv.Findings = append(rv.Findings, sub.Findings...)
		if savedLenses(rv) < len(rv.Lenses) {
			redeliverPending(g)
			return nil, errSkipEvent
		}
		g.Undelivered = true
		return finishReview(g), nil
	})
	if err != nil {
		return ReviewSaveResult{}, err
	}
	out = nf
	h.reviewMemory(ctx, out, sub, diffPaths)
	return ReviewSaveResult{Saved: savedLenses(out.Review), Total: len(out.Review.Lenses)}, nil
}

// lensSavable checks the feature is in its review and the lens is selected and not saved yet.
func lensSavable(f domain.Feature, lens string) error {
	if f.State.Phase != domain.PhaseReviewing || f.Review == nil {
		return fmt.Errorf("feature %s is not reviewing (state %s); next step %s", f.ID, f.State.Phase, f.Next.Label())
	}
	for _, l := range f.Review.Lenses {
		if l.Name != lens {
			continue
		}
		if l.Saved {
			return fmt.Errorf("lens %s is already saved", lens)
		}
		return nil
	}
	return fmt.Errorf("lens %s is not part of this review", lens)
}
