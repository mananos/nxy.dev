package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/jscompat"
	"github.com/mananos/nxy.dev/v2/internal/legacy"
	"github.com/mananos/nxy.dev/v2/internal/memory"
	"github.com/mananos/nxy.dev/v2/internal/report"
)

// Marks of what was imported once.
const (
	LegacyScopeMemory = "memory"                 // key = repo
	LegacyScopeReview = "review"                 // key = checkout
	LegacyScopeGlobal = legacy.ScopeMemoryGlobal // key = "" (once per machine)
)

// Sources of the rows of filter_sessions.
const (
	FilterSource1x = "1x"
	FilterSourceV2 = "v2"
)

// copyFile copies src to dst; the original is only read.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ReadLegacyMemory reads the memories of project (and the global ones, which the conversion
// counts and leaves out) with their edges from the 1.x memory.db. It works on a copy in a
// temporary folder, removed at the end: the original is never opened. A missing memory.db gives
// no rows.
func ReadLegacyMemory(ctx context.Context, dbPath, project string) ([]legacy.Memory, []legacy.Edge, error) {
	if _, err := os.Stat(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	dir, err := os.MkdirTemp("", "nxy-legacy-")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)
	cp := filepath.Join(dir, "memory.db")
	if err := copyFile(dbPath, cp); err != nil {
		return nil, nil, err
	}
	if _, err := os.Stat(dbPath + "-wal"); err == nil {
		if err := copyFile(dbPath+"-wal", cp+"-wal"); err != nil {
			return nil, nil, err
		}
	}
	db, err := sql.Open("sqlite", cp+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT id, scope, COALESCE(project,''), COALESCE(area,''), type, title, body, private, created, updated
		 FROM memories WHERE project=? OR scope='global' ORDER BY created, id`, project)
	if err != nil {
		return nil, nil, fmt.Errorf("no pude leer el memory.db de la 1.x: %w", err)
	}
	var mems []legacy.Memory
	for rows.Next() {
		var m legacy.Memory
		var priv int
		if err := rows.Scan(&m.ID, &m.Scope, &m.Project, &m.Area, &m.Type, &m.Title, &m.Body, &priv, &m.Created, &m.Updated); err != nil {
			rows.Close()
			return nil, nil, err
		}
		m.Private = priv == 1
		mems = append(mems, m)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	erows, err := db.QueryContext(ctx,
		`SELECT src, dst, kind FROM edges WHERE src IN (SELECT id FROM memories WHERE project=? OR scope='global') ORDER BY src, dst, kind`, project)
	if err != nil {
		return nil, nil, fmt.Errorf("no pude leer el memory.db de la 1.x: %w", err)
	}
	defer erows.Close()
	var edges []legacy.Edge
	for erows.Next() {
		var e legacy.Edge
		if err := erows.Scan(&e.Src, &e.Dst, &e.Kind); err != nil {
			return nil, nil, err
		}
		edges = append(edges, e)
	}
	return mems, edges, erows.Err()
}

// legacyULID is a ULID dated at the 1.x row, with the hash of its 1.x id as entropy, so
// importing the same row twice (even into two databases) gives the same id.
func legacyULID(ms int64, legacyID string) string {
	sum := sha256.Sum256([]byte(legacyID))
	return newULID(time.UnixMilli(ms), bytes.NewReader(sum[:10]))
}

// putMark inserts the mark of an import; false means it was already there.
func putMark(ctx context.Context, q queryer, scope, key string, at int64, c legacy.Counts) (bool, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return false, err
	}
	res, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO legacy_imports (scope, key, imported_at, counts) VALUES (?,?,?,?)`,
		scope, key, at, string(b))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ImportLegacyMemory stores what the 1.x knew of repo, once: if the repo was imported before it
// returns false and writes nothing. A learning that already exists (same text and files) is left
// as it is.
// The counts it returns are what was really stored: learnings inserted with their real first and
// last A<n>, the ones already there as Repeated; the mark keeps the same numbers.
func (s *Store) ImportLegacyMemory(ctx context.Context, repo string, imp legacy.Import) (legacy.Counts, bool, error) {
	c, ok, err := s.importLegacyMemory(ctx, repo, imp)
	if err != nil || !ok {
		return legacy.Counts{}, false, err
	}
	return c, true, nil
}

func (s *Store) importLegacyMemory(ctx context.Context, repo string, imp legacy.Import) (legacy.Counts, bool, error) {
	// c: the repo part (no globals); g: the global part. Both are what went in this time.
	c := imp.Counts
	c.Learnings, c.Repeated, c.LearningFirst, c.LearningLast = 0, 0, 0, 0
	c.Globals, c.GlobalFirst, c.GlobalLast = 0, 0, 0
	c.Private -= imp.GlobalLeft.Private
	c.Other -= imp.GlobalLeft.Other
	c.Superseded -= imp.GlobalLeft.Superseded
	repoCounts := imp.Counts
	repoCounts.Globals, repoCounts.GlobalFirst, repoCounts.GlobalLast = 0, 0, 0
	repoCounts.Private -= imp.GlobalLeft.Private
	repoCounts.Other -= imp.GlobalLeft.Other
	repoCounts.Superseded -= imp.GlobalLeft.Superseded
	var g legacy.Counts
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return c, false, err
	}
	defer tx.Rollback()
	now := s.nowMs()
	repoNew, err := putMark(ctx, tx, LegacyScopeMemory, repo, now, repoCounts)
	if err != nil {
		return c, false, err
	}
	globalNew, err := putMark(ctx, tx, LegacyScopeGlobal, "", now, legacy.Counts{})
	if err != nil {
		return c, false, err
	}
	if !repoNew && !globalNew {
		return legacy.Counts{}, false, nil
	}
	if globalNew {
		// the global leftovers belong to the global mark only
		g.Private, g.Other, g.Superseded = imp.GlobalLeft.Private, imp.GlobalLeft.Other, imp.GlobalLeft.Superseded
	}
	// insertLearning stores l unless the same learning is already live; it returns the short
	// number stored (0 when it was already there).
	insertLearning := func(l memory.Learning, legacyID string) (int, error) {
		var one int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM mem_learnings WHERE repo=? AND hash=? AND deleted_at=0`, l.Repo, l.Hash).Scan(&one)
		if err == nil {
			return 0, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		l.ID = legacyULID(l.CreatedAt, legacyID)
		l.Machine = s.machineID
		l.Seen = 1
		if l.LastSeenAt == 0 {
			l.LastSeenAt = l.UpdatedAt
		}
		if l.Short, err = nextShort(ctx, tx, KindLearning, "A"); err != nil {
			return 0, err
		}
		if err := upsertLearning(ctx, tx, l); err != nil {
			return 0, err
		}
		var short string
		if err := tx.QueryRowContext(ctx, `SELECT short FROM mem_learnings WHERE id=?`, l.ID).Scan(&short); err != nil {
			return 0, err
		}
		n, _ := strconv.Atoi(strings.TrimPrefix(short, "A"))
		return n, nil
	}
	for _, src := range imp.Sources {
		switch src.Kind {
		case legacy.SourceGlobalLearning:
			if !globalNew {
				continue
			}
			n, err := insertLearning(imp.Learnings[src.Index], src.LegacyID)
			if err != nil {
				return c, false, err
			}
			if n == 0 {
				g.Repeated++
				continue
			}
			g.Globals++
			if g.GlobalFirst == 0 || n < g.GlobalFirst {
				g.GlobalFirst = n
			}
			g.GlobalLast = max(g.GlobalLast, n)
		case legacy.SourceLearning:
			if !repoNew {
				continue
			}
			n, err := insertLearning(imp.Learnings[src.Index], src.LegacyID)
			if err != nil {
				return c, false, err
			}
			if n == 0 {
				c.Repeated++
				continue
			}
			c.Learnings++
			if c.LearningFirst == 0 || n < c.LearningFirst {
				c.LearningFirst = n
			}
			c.LearningLast = max(c.LearningLast, n)
		case legacy.SourceFeature:
			if !repoNew {
				continue
			}
			f := imp.Features[src.Index]
			f.ID = legacyULID(f.CreatedAt, src.LegacyID)
			f.Machine = s.machineID
			if err := upsertFeature(ctx, tx, f); err != nil {
				return c, false, err
			}
		case legacy.SourceNote:
			if !repoNew {
				continue
			}
			n := imp.Notes[src.Index]
			n.ID = legacyULID(n.CreatedAt, src.LegacyID)
			n.Machine = s.machineID
			if n.Short, err = nextShort(ctx, tx, KindNote, "N"); err != nil {
				return c, false, err
			}
			if err := upsertNote(ctx, tx, n); err != nil {
				return c, false, err
			}
		}
	}
	store := func(scope, key string, cc legacy.Counts) error {
		b, err := json.Marshal(cc)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE legacy_imports SET counts=? WHERE scope=? AND key=?`, string(b), scope, key)
		return err
	}
	out := g
	if repoNew {
		if err := store(LegacyScopeMemory, repo, c); err != nil {
			return c, false, err
		}
		out = c.Add(g)
	}
	if globalNew {
		if err := store(LegacyScopeGlobal, "", g); err != nil {
			return c, false, err
		}
	}
	return out, true, tx.Commit()
}

// ImportLegacyReviews stores the findings the 1.x offered, once per checkout (false: already
// done, nothing written).
func (s *Store) ImportLegacyReviews(ctx context.Context, checkout string, picks []legacy.Pick, counts legacy.Counts) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	first, err := putMark(ctx, tx, LegacyScopeReview, checkout, s.nowMs(), counts)
	if err != nil || !first {
		return false, err
	}
	for _, p := range picks {
		chosen := 0
		if p.Chosen {
			chosen = 1
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO review_picks (feature_id, finding, repo, lens, severity, chosen, created_at) VALUES (?,?,?,?,?,?,?)`,
			p.FeatureID, p.Finding, p.Repo, p.Lens, p.Severity, chosen, p.CreatedAt); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

// FilterCursor is how far the 1.x filter.jsonl of the checkout was read (zero when never).
func (s *Store) FilterCursor(ctx context.Context, checkout string) (legacy.Cursor, error) {
	var c legacy.Cursor
	err := s.db.QueryRowContext(ctx, `SELECT byte_offset, size, head FROM legacy_filter_cursor WHERE checkout=?`, checkout).
		Scan(&c.Offset, &c.Size, &c.Head)
	if errors.Is(err, sql.ErrNoRows) {
		return legacy.Cursor{}, nil
	}
	return c, err
}

// FilterImport is what ImportLegacyFilter did.
type FilterImport = legacy.FilterResult

func kindText(k any) (string, error) {
	if jscompat.IsUndefined(k) {
		return "", nil
	}
	b, err := json.Marshal(k)
	return string(b), err
}

func kindValue(s string) any {
	if s == "" {
		return jscompat.Undefined
	}
	var v any
	if json.Unmarshal([]byte(s), &v) != nil {
		return jscompat.Undefined
	}
	return v
}

// addGroup adds g to the session's groups: a new key goes at the end, an existing one is summed.
func addGroup(ctx context.Context, q queryer, checkout, session, source string, g report.FilterGroup, now int64) error {
	kind, err := kindText(g.Kind)
	if err != nil {
		return err
	}
	var seq int
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), -1) + 1 FROM filter_sessions WHERE checkout=? AND session_id=?`,
		checkout, session).Scan(&seq); err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO filter_sessions (checkout, session_id, source, seq, engine, reason, kind, rows, chars, tokens, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(checkout, session_id, engine, reason, kind) DO UPDATE SET
		rows=rows+excluded.rows, chars=chars+excluded.chars, tokens=tokens+excluded.tokens,
		source=excluded.source, updated_at=excluded.updated_at`,
		checkout, session, source, seq, g.Engine, g.Reason, kind, g.Rows, g.Chars, g.Tokens, now)
	return err
}

// ImportLegacyFilter adds groups read from the 1.x filter.jsonl, per session, and moves the
// cursor of the checkout from "from" to "to" in one transaction. If the stored cursor is not from
// (another start-up already advanced it) nothing is written. A session the v2 wrote is skipped. With
// reset (the file was truncated or rotated and read again from 0) the 1.x groups of each session
// in the file are replaced; those of sessions no longer in the file stay.
func (s *Store) ImportLegacyFilter(ctx context.Context, checkout string, from, to legacy.Cursor,
	groups map[string][]report.FilterGroup, reset bool) (FilterImport, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return FilterImport{}, err
	}
	defer tx.Rollback()
	var cur legacy.Cursor
	err = tx.QueryRowContext(ctx, `SELECT byte_offset, size, head FROM legacy_filter_cursor WHERE checkout=?`, checkout).
		Scan(&cur.Offset, &cur.Size, &cur.Head)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return FilterImport{}, err
	}
	if cur != from {
		return FilterImport{}, nil
	}
	now := s.nowMs()
	res := FilterImport{Applied: true}
	sessions := make([]string, 0, len(groups))
	for id := range groups {
		sessions = append(sessions, id)
	}
	sort.Strings(sessions)
	for _, id := range sessions {
		var v2 int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM filter_sessions WHERE checkout=? AND session_id=? AND source=?`,
			checkout, id, FilterSourceV2).Scan(&v2); err != nil {
			return FilterImport{}, err
		}
		if v2 > 0 {
			res.V2++
			continue
		}
		if reset {
			if _, err := tx.ExecContext(ctx, `DELETE FROM filter_sessions WHERE checkout=? AND session_id=? AND source=?`,
				checkout, id, FilterSource1x); err != nil {
				return FilterImport{}, err
			}
		}
		for _, g := range groups[id] {
			if err := addGroup(ctx, tx, checkout, id, FilterSource1x, g, now); err != nil {
				return FilterImport{}, err
			}
			res.Rows += g.Rows
		}
		res.Sessions++
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO legacy_filter_cursor (checkout, byte_offset, size, head, updated_at) VALUES (?,?,?,?,?)
		ON CONFLICT(checkout) DO UPDATE SET byte_offset=excluded.byte_offset, size=excluded.size, head=excluded.head, updated_at=excluded.updated_at`,
		checkout, to.Offset, to.Size, to.Head, now); err != nil {
		return FilterImport{}, err
	}
	return res, tx.Commit()
}

// AddFilterGroup adds what the v2 saw (its PostToolUse hook) to the session of the checkout; the
// session then belongs to the v2.
func (s *Store) AddFilterGroup(ctx context.Context, checkout, session string, g report.FilterGroup) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := addGroup(ctx, tx, checkout, session, FilterSourceV2, g, s.nowMs()); err != nil {
		return err
	}
	return tx.Commit()
}

// queryFilterGroups reads the groups of a session in order of first appearance.
func queryFilterGroups(ctx context.Context, q queryer, checkout, session string) ([]report.FilterGroup, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT engine, reason, kind, rows, chars, tokens FROM filter_sessions WHERE checkout=? AND session_id=? ORDER BY seq`,
		checkout, session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []report.FilterGroup
	for rows.Next() {
		var g report.FilterGroup
		var kind string
		if err := rows.Scan(&g.Engine, &g.Reason, &kind, &g.Rows, &g.Chars, &g.Tokens); err != nil {
			return nil, err
		}
		g.Kind = kindValue(kind)
		out = append(out, g)
	}
	return out, rows.Err()
}

// FilterGroups is the filter summary of a session, in order of first appearance.
func (s *Store) FilterGroups(ctx context.Context, checkout, session string) ([]report.FilterGroup, error) {
	return queryFilterGroups(ctx, s.db, checkout, session)
}

// LegacyMark is the record of an import done once.
type LegacyMark = legacy.Mark

// LegacyCursor is a stored read position of a checkout's filter.jsonl.
type LegacyCursor = legacy.StoredCursor

// SetLegacyError keeps the last failure of the memory import of repo and when it happened, so the
// copy of memory.db is not retried at every start; an empty msg clears it.
func (s *Store) SetLegacyError(ctx context.Context, repo string, at int64, msg string) error {
	if msg == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM legacy_imports WHERE scope=? AND key=?`, legacy.ScopeMemoryError, repo)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO legacy_imports (scope, key, imported_at, counts) VALUES (?,?,?,?)
		ON CONFLICT(scope, key) DO UPDATE SET imported_at=excluded.imported_at, counts=excluded.counts`,
		legacy.ScopeMemoryError, repo, at, msg)
	return err
}

// MarkLegacyPlan marks a half-finished 1.x plan (key = legacy.HalfPlanKey) as answered, once: the
// raw answer (legacy.Answer*) goes in counts. False when it was already marked. The mark stays
// out of the Bundle like the rest of legacy_imports.
func (s *Store) MarkLegacyPlan(ctx context.Context, key, answer string, at int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO legacy_imports (scope, key, imported_at, counts) VALUES (?,?,?,?)`,
		legacy.ScopeHalfPlan, key, at, answer)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// LegacyImports lists the marks (by scope and key) and the filter cursors (by checkout). For the
// scope legacy.ScopeHalfPlan, counts holds the plain answer text (Mark.Answer), not JSON.
func (s *Store) LegacyImports(ctx context.Context) ([]LegacyMark, []LegacyCursor, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT scope, key, imported_at, counts FROM legacy_imports ORDER BY scope, key`)
	if err != nil {
		return nil, nil, err
	}
	var marks []LegacyMark
	for rows.Next() {
		var m LegacyMark
		var raw string
		if err := rows.Scan(&m.Scope, &m.Key, &m.ImportedAt, &raw); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if m.Scope == legacy.ScopeMemoryError {
			m.Error = raw
		} else if m.Scope == legacy.ScopeHalfPlan {
			m.Answer = raw
		} else {
			_ = json.Unmarshal([]byte(raw), &m.Counts)
		}
		marks = append(marks, m)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	crows, err := s.db.QueryContext(ctx, `SELECT checkout, byte_offset, size, head, updated_at FROM legacy_filter_cursor ORDER BY checkout`)
	if err != nil {
		return nil, nil, err
	}
	defer crows.Close()
	var curs []LegacyCursor
	for crows.Next() {
		var c LegacyCursor
		if err := crows.Scan(&c.Checkout, &c.Cursor.Offset, &c.Cursor.Size, &c.Cursor.Head, &c.UpdatedAt); err != nil {
			return nil, nil, err
		}
		curs = append(curs, c)
	}
	return marks, curs, crows.Err()
}
