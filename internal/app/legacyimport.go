package app

import (
	"context"
	"errors"
	"time"

	"github.com/mananos/nxy.dev/internal/legacy"
	"github.com/mananos/nxy.dev/internal/report"
)

// Marks of what was imported once (the same text as the store's).
const (
	legacyScopeMemory = "memory"
	legacyScopeReview = "review"
	legacyScopeGlobal = legacy.ScopeMemoryGlobal // key "": the global memories, once per machine
)

// ImportLegacy copies what the 1.x knows of the repo of dir into the v2 without touching a 1.x
// file: memory (conventions, live handoffs as notes, archived handoffs as closed features) once
// per repo, the review picks once per checkout, and the filter summary every time, from where the
// last read stopped. It returns the counts of this call and whether the memory or the review went
// in for the first time. A failure leaves marks and cursor as they were, so the next call retries.
func (h Hooks) ImportLegacy(ctx context.Context, dir string) (legacy.Counts, bool, error) {
	return h.importLegacy(ctx, dir, false)
}

// ImportLegacyNow is ImportLegacy for the user's own command: it retries the memory copy even when
// it failed less than a day ago.
func (h Hooks) ImportLegacyNow(ctx context.Context, dir string) (legacy.Counts, bool, error) {
	return h.importLegacy(ctx, dir, true)
}

// legacyRetryAfter is how long a failed memory import waits before a start-up tries it again.
const legacyRetryAfter = 24 * time.Hour

func (h Hooks) importLegacy(ctx context.Context, dir string, force bool) (legacy.Counts, bool, error) {
	d, s := h.LegacyData, h.LegacyStore
	var total legacy.Counts
	if d == nil || s == nil || dir == "" {
		return total, false, nil
	}
	hasMem, hasReview := d.HasMemory(), d.HasReview(dir)
	size, hasFilter := d.FilterSize(dir)
	if !hasMem && !hasReview && !hasFilter {
		return total, false, nil
	}
	marks, cursors, err := s.LegacyImports(ctx)
	if err != nil {
		return total, false, err
	}
	repo, checkout := d.RepoKey(dir), d.Checkout(dir)
	marked := func(scope, key string) bool {
		for _, m := range marks {
			if m.Scope == scope && m.Key == key {
				return true
			}
		}
		return false
	}
	var errs []error
	first := false

	failedRecently := false
	hadError := false
	for _, m := range marks {
		if m.Scope == legacy.ScopeMemoryError && m.Key == repo {
			hadError = true
			failedRecently = h.now().UnixMilli()-m.ImportedAt < legacyRetryAfter.Milliseconds()
		}
	}
	if hasMem && (!marked(legacyScopeMemory, repo) || !marked(legacyScopeGlobal, "")) && (force || !failedRecently) {
		mems, edges, err := d.Memory(ctx, d.ProjectKey(dir))
		if err == nil {
			imp := legacy.ConvertMemory(mems, edges, repo, d.Folder(dir), h.now().UnixMilli())
			var ok bool
			var stored legacy.Counts
			if stored, ok, err = s.ImportLegacyMemory(ctx, repo, imp); ok {
				first = true
				total = total.Add(stored)
			}
		}
		if err != nil {
			errs = append(errs, err)
			_ = s.SetLegacyError(ctx, repo, h.now().UnixMilli(), err.Error())
		} else if hadError {
			_ = s.SetLegacyError(ctx, repo, 0, "")
		}
	}

	if hasReview && !marked(legacyScopeReview, checkout) {
		picks, c := legacy.ConvertReviews(d.Review(dir), repo)
		ok, err := s.ImportLegacyReviews(ctx, checkout, picks, c)
		if ok {
			first = true
			total = total.Add(c)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}

	if hasFilter && size > 0 {
		var cur legacy.Cursor
		for _, c := range cursors {
			if c.Checkout == checkout {
				cur = c.Cursor
			}
		}
		if cur.Offset == 0 || size != cur.Offset {
			res, noSess, err := h.importFilter(ctx, dir, checkout, cur, size)
			if res.Applied {
				total.FilterRows += res.Rows
				total.FilterSessions += res.Sessions
				total.FilterV2 += res.V2
				total.FilterNoSess += noSess
			}
			if err != nil {
				errs = append(errs, err)
			}
		}
	}
	return total, first, errors.Join(errs...)
}

// importFilter reads the new lines of filter.jsonl (all of it again when the file was truncated or
// rotated) and adds them per session; the second result is the rows without a session id.
func (h Hooks) importFilter(ctx context.Context, dir, checkout string, cur legacy.Cursor, size int64) (legacy.FilterResult, int, error) {
	d := h.LegacyData
	reset := cur.Offset > size || (cur.Offset > 0 && d.FilterHead(dir, cur.Offset) != cur.Head)
	from := cur.Offset
	if reset {
		from = 0
	}
	rows, next, err := d.FilterFrom(dir, from)
	if err != nil {
		return legacy.FilterResult{}, 0, err
	}
	if next.Offset == from {
		return legacy.FilterResult{}, 0, nil // no complete line yet
	}
	var order []string
	bySession := map[string][]any{}
	noSess := 0
	for _, r := range rows {
		m, _ := r.(map[string]any)
		id, _ := m["session_id"].(string)
		if id == "" {
			noSess++
			continue
		}
		if _, seen := bySession[id]; !seen {
			order = append(order, id)
		}
		bySession[id] = append(bySession[id], r)
	}
	groups := make(map[string][]report.FilterGroup, len(order))
	for _, id := range order {
		groups[id] = report.GroupFilterRows(bySession[id])
	}
	res, err := h.LegacyStore.ImportLegacyFilter(ctx, checkout, cur, next, groups, reset)
	return res, noSess, err
}
