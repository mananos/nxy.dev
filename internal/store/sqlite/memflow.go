package sqlite

import (
	"context"
	"sort"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/memory"
)

// CountDeliveries adds one delivery to each rule (by ULID). It does not touch updated_at: a counter
// is not a change that travels, and it does not reindex the search.
func (s *Store) CountDeliveries(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE mem_rules SET deliveries=deliveries+1 WHERE id=? AND deleted_at=0`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Cite counts one citation (and last_cited_at = at, in ms) on each live rule of repo named by a
// short reference (K3). It returns the references that exist; updated_at does not move.
func (s *Store) Cite(ctx context.Context, repo string, shorts []string, at int64) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var found []string
	for _, sh := range shorts {
		sh = strings.ToUpper(strings.TrimSpace(sh))
		res, err := tx.ExecContext(ctx,
			`UPDATE mem_rules SET citations=citations+1, last_cited_at=MAX(last_cited_at, ?) WHERE (repo=? OR (repo='' AND short LIKE 'G%')) AND short=? AND deleted_at=0`,
			at, repo, sh)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			found = append(found, sh)
		}
	}
	return found, tx.Commit()
}

// MarkObsolete marks the live learnings of repo named by refs (A7) as obsolete at the given time
// (ms). It moves updated_at: a state change travels in a bundle. Unknown refs are skipped.
func (s *Store) MarkObsolete(ctx context.Context, repo string, refs []string, at int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, ref := range refs {
		ref = strings.ToUpper(strings.TrimSpace(ref))
		if !isShortRef(ref) || ref[0] != 'A' {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE mem_learnings SET obsolete_at=?, updated_at=MAX(updated_at, ?) WHERE repo=? AND short=? AND deleted_at=0 AND obsolete_at=0`,
			at, at, repo, ref); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SimilarRules finds live, not replaced rules of repo whose text overlaps text by SimilarMin or more,
// the most similar first.
func (s *Store) SimilarRules(ctx context.Context, repo, text string, limit int) ([]memory.Rule, error) {
	fts := memory.SimilarQuery(text)
	if fts == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = memory.SearchLimit
	}
	rules, err := queryRules(ctx, s.db, `WHERE deleted_at=0 AND repo=?
		AND NOT EXISTS (SELECT 1 FROM mem_rules n WHERE n.supersedes=mem_rules.id AND n.deleted_at=0)
		AND id IN (SELECT row_id FROM mem_fts WHERE kind='rule' AND mem_fts MATCH ? ORDER BY bm25(mem_fts) LIMIT 50)`,
		repo, fts)
	if err != nil {
		return nil, err
	}
	type scored struct {
		r memory.Rule
		o float64
	}
	var keep []scored
	for _, r := range rules {
		if o := memory.Overlap(text, r.Text); o >= memory.SimilarMin {
			keep = append(keep, scored{r, o})
		}
	}
	sort.SliceStable(keep, func(i, j int) bool {
		if keep[i].o != keep[j].o {
			return keep[i].o > keep[j].o
		}
		return keep[i].r.ID < keep[j].r.ID
	})
	var out []memory.Rule
	for _, k := range keep {
		if len(out) == limit {
			break
		}
		out = append(out, k.r)
	}
	return out, nil
}

// RecordEscape stores a problem the review missed. featureID is empty when no feature was active.
func (s *Store) RecordEscape(ctx context.Context, featureID, repo, lens, text string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO review_escapes (feature_id, repo, lens, text, created_at) VALUES (?, ?, ?, ?, ?)`,
		featureID, repo, lens, text, s.nowMs())
	return err
}

// ReviewStats counts, per lens, the findings chosen and ignored and the escapes recorded for repo
// since the given time (ms). Lenses are sorted by name.
func (s *Store) ReviewStats(ctx context.Context, repo string, since int64) ([]domain.LensStat, error) {
	by := map[string]*domain.LensStat{}
	get := func(l string) *domain.LensStat {
		if by[l] == nil {
			by[l] = &domain.LensStat{Lens: l}
		}
		return by[l]
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT lens, COUNT(*), COALESCE(SUM(chosen),0) FROM review_picks WHERE repo=? AND created_at>=? GROUP BY lens`, repo, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var l string
		var offered, chosen int
		if err := rows.Scan(&l, &offered, &chosen); err != nil {
			rows.Close()
			return nil, err
		}
		st := get(l)
		st.Chosen, st.Ignored = chosen, offered-chosen
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	rows, err = s.db.QueryContext(ctx,
		`SELECT lens, COUNT(*) FROM review_escapes WHERE repo=? AND created_at>=? GROUP BY lens`, repo, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var l string
		var n int
		if err := rows.Scan(&l, &n); err != nil {
			return nil, err
		}
		get(l).Escapes = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]domain.LensStat, 0, len(by))
	for _, v := range by {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Lens < out[j].Lens })
	return out, nil
}
