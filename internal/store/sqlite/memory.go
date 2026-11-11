package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/memory"
)

// Kinds accepted by Resolve.
const (
	KindRule     = "rule"
	KindLearning = "learning"
	KindNote     = "note"
)

var kindInfo = map[string]struct{ table, letter string }{
	KindRule:     {"mem_rules", "KG"},
	KindLearning: {"mem_learnings", "A"},
	KindNote:     {"mem_notes", "N"},
}

// queryer is what *sql.DB and *sql.Tx share.
type queryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type scanner interface{ Scan(...any) error }

func (s *Store) nowMs() int64 { return s.now().UTC().UnixMilli() }

func encodeStrings(v []string) string {
	if len(v) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func decodeStrings(s string) []string {
	var v []string
	if s == "" || s == "[]" {
		return nil
	}
	_ = json.Unmarshal([]byte(s), &v)
	return v
}

func cleanPaths(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range in {
		if c := memory.CleanPath(p); c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

// nextShort returns the next free alias ("K4") of a table; it runs inside the write
// transaction (_txlock=immediate), so no other writer can take it first.
func nextShort(ctx context.Context, q queryer, kind, letter string) (string, error) {
	k := kindInfo[kind]
	var n int
	err := q.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(CAST(SUBSTR(short, 2) AS INTEGER)), 0) + 1 FROM `+k.table+` WHERE SUBSTR(short, 1, 1)=?`,
		letter).Scan(&n)
	return fmt.Sprintf("%s%d", letter, n), err
}

// shortLetter is the alias letter a row gets: rules K (repo) or G (global), learnings A, notes N.
func shortLetter(kind, repo string) string {
	if kind == KindRule {
		return string(memory.RuleLetter(repo))
	}
	return kindInfo[kind].letter
}

// shortFor keeps the incoming alias unless it is empty, flagged, of another letter or already taken.
func shortFor(ctx context.Context, q queryer, kind, repo, want string, reassign bool) (string, error) {
	letter := shortLetter(kind, repo)
	if want != "" && !reassign && strings.HasPrefix(want, letter) {
		var one int
		err := q.QueryRowContext(ctx, `SELECT 1 FROM `+kindInfo[kind].table+` WHERE short=?`, want).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return want, nil
		}
		if err != nil {
			return "", err
		}
	}
	return nextShort(ctx, q, kind, letter)
}

func isShortRef(ref string) bool {
	if len(ref) < 2 || !strings.ContainsRune("KANG", rune(ref[0])) {
		return false
	}
	for _, c := range ref[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Resolve turns a reference (K3, A7, N2, a ULID or a unique ULID prefix) into the ULID of a
// live row of the kind.
func (s *Store) Resolve(ctx context.Context, kind, ref string) (string, error) {
	return resolveRef(ctx, s.db, kind, ref)
}

func resolveRef(ctx context.Context, q queryer, kind, ref string) (string, error) {
	return resolveRefState(ctx, q, kind, ref, false)
}

// resolveRefDeleted is resolveRef over the deleted rows only (for restore).
func resolveRefDeleted(ctx context.Context, q queryer, kind, ref string) (string, error) {
	return resolveRefState(ctx, q, kind, ref, true)
}

func resolveRefState(ctx context.Context, q queryer, kind, ref string, deleted bool) (string, error) {
	state := "deleted_at=0"
	if deleted {
		state = "deleted_at<>0"
	}
	k, ok := kindInfo[kind]
	if !ok {
		return "", fmt.Errorf("sqlite: unknown memory kind %q", kind)
	}
	ref = strings.ToUpper(strings.TrimSpace(ref))
	notFound := fmt.Errorf("%w: %s", memory.ErrNotFound, ref)
	if ref == "" {
		return "", notFound
	}
	var rows *sql.Rows
	var err error
	if isShortRef(ref) {
		if !strings.Contains(k.letter, ref[:1]) {
			return "", notFound
		}
		rows, err = q.QueryContext(ctx, `SELECT id FROM `+k.table+` WHERE short=? AND `+state, ref)
	} else {
		for _, c := range ref {
			if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z') {
				return "", notFound
			}
		}
		rows, err = q.QueryContext(ctx, `SELECT id FROM `+k.table+` WHERE id LIKE ? AND `+state+` LIMIT 2`, ref+"%")
	}
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	switch len(ids) {
	case 0:
		return "", notFound
	case 1:
		return ids[0], nil
	}
	return "", fmt.Errorf("%w: %s", memory.ErrAmbiguous, ref)
}

// ---- rules ----

const ruleCols = `id, short, repo, text, question, origin, feature_id, paths, supersedes, deliveries, citations, last_cited_at, created_at, updated_at, deleted_at, machine_id, roles`

func scanRule(sc scanner) (memory.Rule, error) {
	var r memory.Rule
	var paths, roles string
	err := sc.Scan(&r.ID, &r.Short, &r.Repo, &r.Text, &r.Question, &r.Origin, &r.FeatureID, &paths, &r.Supersedes,
		&r.Deliveries, &r.Citations, &r.LastCitedAt, &r.CreatedAt, &r.UpdatedAt, &r.DeletedAt, &r.Machine, &roles)
	r.Paths = decodeStrings(paths)
	r.Roles = decodeStrings(roles)
	return r, err
}

func queryRules(ctx context.Context, q queryer, where string, args ...any) ([]memory.Rule, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+ruleCols+` FROM mem_rules `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []memory.Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// staleRow reports whether the table already holds id with a newer updated_at: an import decided
// on an older snapshot must not overwrite it (the check runs inside the write transaction).
func staleRow(ctx context.Context, q queryer, table, id string, updatedAt int64) (bool, error) {
	var cur int64
	err := q.QueryRowContext(ctx, `SELECT updated_at FROM `+table+` WHERE id=?`, id).Scan(&cur)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return cur > updatedAt, err
}

// upsertRule writes r as given; an existing row keeps its short alias.
func upsertRule(ctx context.Context, q queryer, r memory.Rule) error {
	if stale, err := staleRow(ctx, q, "mem_rules", r.ID, r.UpdatedAt); err != nil || stale {
		return err
	}
	question := r.Question
	if strings.TrimSpace(question) == "" && r.Deleted() {
		question = "(deleted)" // a tombstone may come without it; the table still requires one
	}
	_, err := q.ExecContext(ctx, `INSERT INTO mem_rules (`+ruleCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET repo=excluded.repo, text=excluded.text, question=excluded.question, roles=excluded.roles,
		origin=excluded.origin, feature_id=excluded.feature_id, paths=excluded.paths, supersedes=excluded.supersedes,
		deliveries=MAX(mem_rules.deliveries, excluded.deliveries), citations=MAX(mem_rules.citations, excluded.citations),
		last_cited_at=MAX(mem_rules.last_cited_at, excluded.last_cited_at),
		created_at=excluded.created_at, updated_at=excluded.updated_at, deleted_at=excluded.deleted_at,
		machine_id=excluded.machine_id`,
		r.ID, r.Short, r.Repo, r.Text, question, r.Origin, r.FeatureID, encodeStrings(r.Paths), r.Supersedes,
		r.Deliveries, r.Citations, r.LastCitedAt, r.CreatedAt, r.UpdatedAt, r.DeletedAt, r.Machine, encodeStrings(r.Roles))
	return err
}

// AddRule stores a new rule of a repo. Supersedes may be a reference (K3) or a ULID.
func (s *Store) AddRule(ctx context.Context, r memory.Rule) (memory.Rule, error) {
	if strings.TrimSpace(r.Repo) == "" {
		return memory.Rule{}, errors.New("a rule needs a repo")
	}
	return s.addRule(ctx, r, "K")
}

// AddGlobalRule stores a new global rule (G<n>, empty repo). Supersedes must be another G.
func (s *Store) AddGlobalRule(ctx context.Context, r memory.Rule) (memory.Rule, error) {
	r.Repo = memory.GlobalRepo
	r.Roles = memory.CleanRoles(r.Roles)
	return s.addRule(ctx, r, "G")
}

func (s *Store) addRule(ctx context.Context, r memory.Rule, letter string) (memory.Rule, error) {
	if err := memory.ValidateRule(r); err != nil {
		return memory.Rule{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.Rule{}, err
	}
	defer tx.Rollback()
	if r.Supersedes != "" {
		if r.Supersedes, err = resolveRef(ctx, tx, KindRule, r.Supersedes); err != nil {
			return memory.Rule{}, err
		}
		old, err := queryRules(ctx, tx, `WHERE id=?`, r.Supersedes)
		if err != nil || len(old) == 0 {
			return memory.Rule{}, errors.Join(err, memory.ErrNotFound)
		}
		if !strings.HasPrefix(old[0].Short, letter) {
			return memory.Rule{}, fmt.Errorf("%s cannot replace %s: a %s rule only replaces a %s rule",
				letter, old[0].Short, letter, letter)
		}
	}
	t := s.now()
	r.ID = newULID(t, rand.Reader)
	r.Machine = s.machineID
	r.CreatedAt, r.UpdatedAt, r.DeletedAt = t.UTC().UnixMilli(), t.UTC().UnixMilli(), 0
	if r.Short, err = nextShort(ctx, tx, KindRule, letter); err != nil {
		return memory.Rule{}, err
	}
	if err := upsertRule(ctx, tx, r); err != nil {
		return memory.Rule{}, err
	}
	return r, tx.Commit()
}

// GlobalRules lists the live global rules.
func (s *Store) GlobalRules(ctx context.Context) ([]memory.Rule, error) {
	return s.Rules(ctx, memory.GlobalRepo)
}

// Rules lists the live rules of a repo that no live rule supersedes.
func (s *Store) Rules(ctx context.Context, repo string) ([]memory.Rule, error) {
	return queryRules(ctx, s.db, `WHERE repo=? AND deleted_at=0
		AND NOT EXISTS (SELECT 1 FROM mem_rules n WHERE n.supersedes=mem_rules.id AND n.deleted_at=0)
		ORDER BY created_at, CAST(SUBSTR(short, 2) AS INTEGER)`, repo)
}

// ---- learnings ----

const learningCols = `id, short, repo, text, files, feature_id, hash, seen, last_seen_at, obsolete_at, promoted_to, created_at, updated_at, deleted_at, machine_id`

func scanLearning(sc scanner) (memory.Learning, error) {
	var l memory.Learning
	var files string
	err := sc.Scan(&l.ID, &l.Short, &l.Repo, &l.Text, &files, &l.FeatureID, &l.Hash, &l.Seen, &l.LastSeenAt,
		&l.ObsoleteAt, &l.PromotedTo, &l.CreatedAt, &l.UpdatedAt, &l.DeletedAt, &l.Machine)
	l.Files = decodeStrings(files)
	return l, err
}

func queryLearnings(ctx context.Context, q queryer, where string, args ...any) ([]memory.Learning, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+learningCols+` FROM mem_learnings `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []memory.Learning
	for rows.Next() {
		l, err := scanLearning(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func upsertLearning(ctx context.Context, q queryer, l memory.Learning) error {
	if stale, err := staleRow(ctx, q, "mem_learnings", l.ID, l.UpdatedAt); err != nil || stale {
		return err
	}
	_, err := q.ExecContext(ctx, `INSERT INTO mem_learnings (`+learningCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET repo=excluded.repo, text=excluded.text, files=excluded.files,
		feature_id=excluded.feature_id, hash=excluded.hash, seen=MAX(mem_learnings.seen, excluded.seen),
		last_seen_at=MAX(mem_learnings.last_seen_at, excluded.last_seen_at),
		obsolete_at=excluded.obsolete_at, promoted_to=excluded.promoted_to, created_at=excluded.created_at,
		updated_at=excluded.updated_at, deleted_at=excluded.deleted_at, machine_id=excluded.machine_id`,
		l.ID, l.Short, l.Repo, l.Text, encodeStrings(l.Files), l.FeatureID, l.Hash, l.Seen, l.LastSeenAt,
		l.ObsoleteAt, l.PromotedTo, l.CreatedAt, l.UpdatedAt, l.DeletedAt, l.Machine)
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM mem_learning_files WHERE learning_id=?`, l.ID); err != nil {
		return err
	}
	for _, p := range cleanPaths(l.Files) {
		if _, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO mem_learning_files (learning_id, repo, path) VALUES (?,?,?)`,
			l.ID, l.Repo, p); err != nil {
			return err
		}
	}
	return nil
}

// AddLearning stores a learning, or, when the repo already has a live one with the same hash
// (text and files), counts it as seen once more instead of adding a row.
func (s *Store) AddLearning(ctx context.Context, l memory.Learning) (memory.Learning, error) {
	if strings.TrimSpace(l.Text) == "" || strings.TrimSpace(l.Repo) == "" {
		return memory.Learning{}, errors.New("a learning needs a repo and a text")
	}
	l.Files = cleanPaths(l.Files)
	l.Hash = memory.LearningHash(l.Text, l.Files)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.Learning{}, err
	}
	defer tx.Rollback()
	now := s.nowMs()
	found, err := queryLearnings(ctx, tx, `WHERE repo=? AND hash=? AND deleted_at=0`, l.Repo, l.Hash)
	if err != nil {
		return memory.Learning{}, err
	}
	if len(found) > 0 && found[0].PromotedTo != "" {
		// already a rule: nothing to count, and the learning is not listed any more
		var short string
		if err := tx.QueryRowContext(ctx, `SELECT short FROM mem_rules WHERE id=?`, found[0].PromotedTo).Scan(&short); err != nil {
			short = found[0].PromotedTo
		}
		return found[0], &memory.AlreadyRuleError{Learning: found[0].Short, Rule: short}
	}
	if len(found) > 0 && found[0].ObsoleteAt != 0 {
		// an obsolete learning that comes back is a fresh one: retire the old row so the hash is free
		if _, err := tx.ExecContext(ctx, `UPDATE mem_learnings SET deleted_at=?, updated_at=? WHERE id=?`,
			now, now, found[0].ID); err != nil {
			return memory.Learning{}, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM mem_learning_files WHERE learning_id=?`, found[0].ID); err != nil {
			return memory.Learning{}, err
		}
		found = nil
	}
	if len(found) > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE mem_learnings SET seen=seen+1, last_seen_at=? WHERE id=?`,
			now, found[0].ID); err != nil {
			return memory.Learning{}, err
		}
		got, err := queryLearnings(ctx, tx, `WHERE id=?`, found[0].ID)
		if err != nil || len(got) == 0 {
			return memory.Learning{}, errors.Join(err, memory.ErrNotFound)
		}
		return got[0], tx.Commit()
	}
	l.ID = newULID(s.now(), rand.Reader)
	l.Machine = s.machineID
	l.CreatedAt, l.UpdatedAt, l.LastSeenAt, l.Seen, l.DeletedAt = now, now, now, 1, 0
	if l.Short, err = nextShort(ctx, tx, KindLearning, "A"); err != nil {
		return memory.Learning{}, err
	}
	if err := upsertLearning(ctx, tx, l); err != nil {
		return memory.Learning{}, err
	}
	return l, tx.Commit()
}

// Learnings lists the live learnings of a repo that are neither promoted nor obsolete,
// the most seen first.
func (s *Store) Learnings(ctx context.Context, repo string) ([]memory.Learning, error) {
	return queryLearnings(ctx, s.db, `WHERE repo=? AND deleted_at=0 AND promoted_to='' AND obsolete_at=0
		ORDER BY seen DESC, last_seen_at DESC, id`, repo)
}

// Promote turns a learning into a rule (origin manual) and marks the learning as promoted,
// in one transaction.
func (s *Store) Promote(ctx context.Context, ref, question string, paths []string) (memory.Rule, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.Rule{}, err
	}
	defer tx.Rollback()
	id, err := resolveRef(ctx, tx, KindLearning, ref)
	if err != nil {
		return memory.Rule{}, err
	}
	ls, err := queryLearnings(ctx, tx, `WHERE id=?`, id)
	if err != nil || len(ls) == 0 {
		return memory.Rule{}, errors.Join(err, memory.ErrNotFound)
	}
	l := ls[0]
	// a global learning (empty repo) becomes a G rule, a repo learning a K rule
	if l.PromotedTo != "" {
		return memory.Rule{}, fmt.Errorf("%s is already a rule", l.Short)
	}
	t := s.now()
	r := memory.Rule{
		Meta: memory.Meta{ID: newULID(t, rand.Reader), Machine: s.machineID,
			CreatedAt: t.UTC().UnixMilli(), UpdatedAt: t.UTC().UnixMilli()},
		Repo: l.Repo, Text: l.Text, Question: question, Origin: memory.OriginManual,
		FeatureID: l.FeatureID, Paths: paths,
	}
	if err := memory.ValidateRule(r); err != nil {
		return memory.Rule{}, err
	}
	if r.Short, err = nextShort(ctx, tx, KindRule, string(memory.RuleLetter(r.Repo))); err != nil {
		return memory.Rule{}, err
	}
	if err := upsertRule(ctx, tx, r); err != nil {
		return memory.Rule{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE mem_learnings SET promoted_to=?, updated_at=? WHERE id=?`,
		r.ID, r.UpdatedAt, l.ID); err != nil {
		return memory.Rule{}, err
	}
	return r, tx.Commit()
}

// EditRule changes the text, question, paths or roles of a live rule. The row keeps its ID, alias
// and counters; updated_at moves so the edit travels in export/import.
func (s *Store) EditRule(ctx context.Context, ref string, e memory.RuleEdit) (memory.Rule, error) {
	if e.Empty() {
		return memory.Rule{}, errors.New("nothing to change: give --text, --question, --paths or --roles")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.Rule{}, err
	}
	defer tx.Rollback()
	id, err := resolveRef(ctx, tx, KindRule, ref)
	if err != nil {
		return memory.Rule{}, err
	}
	rs, err := queryRules(ctx, tx, `WHERE id=?`, id)
	if err != nil || len(rs) == 0 {
		return memory.Rule{}, errors.Join(err, memory.ErrNotFound)
	}
	r := rs[0]
	if e.Roles != nil && r.Repo != memory.GlobalRepo {
		return memory.Rule{}, errors.New("--roles only applies to global rules (G<n>)")
	}
	if e.Text != nil {
		r.Text = *e.Text
	}
	if e.Question != nil {
		r.Question = *e.Question
	}
	if e.Paths != nil {
		r.Paths = cleanPaths(*e.Paths)
	}
	if e.Roles != nil {
		r.Roles = memory.CleanRoles(*e.Roles)
	}
	if err := memory.ValidateRule(r); err != nil {
		return memory.Rule{}, err
	}
	r.UpdatedAt = s.nowMs()
	r.Machine = s.machineID
	if err := upsertRule(ctx, tx, r); err != nil {
		return memory.Rule{}, err
	}
	return r, tx.Commit()
}

// EditLearning changes the text or files of a live, unpromoted learning; its hash is recomputed
// and must not clash with another live learning of the repo.
func (s *Store) EditLearning(ctx context.Context, ref string, e memory.LearningEdit) (memory.Learning, error) {
	if e.Empty() {
		return memory.Learning{}, errors.New("nothing to change: give --text or --files")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.Learning{}, err
	}
	defer tx.Rollback()
	id, err := resolveRef(ctx, tx, KindLearning, ref)
	if err != nil {
		return memory.Learning{}, err
	}
	ls, err := queryLearnings(ctx, tx, `WHERE id=?`, id)
	if err != nil || len(ls) == 0 {
		return memory.Learning{}, errors.Join(err, memory.ErrNotFound)
	}
	l := ls[0]
	if l.PromotedTo != "" {
		var short string
		if err := tx.QueryRowContext(ctx, `SELECT short FROM mem_rules WHERE id=?`, l.PromotedTo).Scan(&short); err != nil {
			short = l.PromotedTo
		}
		return memory.Learning{}, fmt.Errorf("%s is already rule %s: edit the rule", l.Short, short)
	}
	if e.Text != nil {
		l.Text = *e.Text
	}
	if e.Files != nil {
		l.Files = *e.Files
	}
	l.Files = cleanPaths(l.Files)
	if strings.TrimSpace(l.Text) == "" {
		return memory.Learning{}, errors.New("a learning needs a text")
	}
	l.Hash = memory.LearningHash(l.Text, l.Files)
	var other string
	err = tx.QueryRowContext(ctx, `SELECT short FROM mem_learnings WHERE repo=? AND hash=? AND deleted_at=0 AND id<>?`,
		l.Repo, l.Hash, l.ID).Scan(&other)
	if err == nil {
		return memory.Learning{}, fmt.Errorf("same text and files as %s", other)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return memory.Learning{}, err
	}
	l.UpdatedAt = s.nowMs()
	l.Machine = s.machineID
	if err := upsertLearning(ctx, tx, l); err != nil {
		return memory.Learning{}, err
	}
	return l, tx.Commit()
}

// memoryKindOf finds which table a reference belongs to: the letter decides (K/G rule, A learning);
// a ULID or prefix is looked up in rules, then learnings.
func memoryKindOf(ctx context.Context, q queryer, ref string, deleted bool) (kind, id string, err error) {
	find := resolveRef
	if deleted {
		find = resolveRefDeleted
	}
	up := strings.ToUpper(strings.TrimSpace(ref))
	if isShortRef(up) {
		switch up[0] {
		case 'K', 'G':
			kind = KindRule
		case 'A':
			kind = KindLearning
		default:
			return "", "", errors.New("notes: nxy note done")
		}
		id, err = find(ctx, q, kind, up)
		return kind, id, err
	}
	rid, rerr := find(ctx, q, KindRule, up)
	lid, lerr := find(ctx, q, KindLearning, up)
	switch {
	case rerr == nil && lerr == nil:
		return "", "", fmt.Errorf("%w: %s", memory.ErrAmbiguous, up)
	case rerr == nil:
		return KindRule, rid, nil
	case lerr == nil:
		return KindLearning, lid, nil
	case errors.Is(rerr, memory.ErrAmbiguous):
		return "", "", rerr
	case errors.Is(lerr, memory.ErrAmbiguous):
		return "", "", lerr
	}
	return "", "", rerr
}

// MemoryKind tells whether a live reference (alias, ULID or prefix) is a "rule" or a "learning".
func (s *Store) MemoryKind(ctx context.Context, ref string) (string, error) {
	kind, _, err := memoryKindOf(ctx, s.db, ref, false)
	return kind, err
}

// DeleteMemory soft-deletes a live rule or learning and returns its alias. The alias is never
// reused and the deletion travels in export/import.
func (s *Store) DeleteMemory(ctx context.Context, ref string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	kind, id, err := memoryKindOf(ctx, tx, ref, false)
	if err != nil {
		return "", err
	}
	now := s.nowMs()
	var short string
	if kind == KindRule {
		if err := tx.QueryRowContext(ctx, `SELECT short FROM mem_rules WHERE id=?`, id).Scan(&short); err != nil {
			return "", err
		}
		_, err = tx.ExecContext(ctx, `UPDATE mem_rules SET deleted_at=?, updated_at=?, machine_id=? WHERE id=? AND deleted_at=0`,
			now, now, s.machineID, id)
	} else {
		if err := tx.QueryRowContext(ctx, `SELECT short FROM mem_learnings WHERE id=?`, id).Scan(&short); err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE mem_learnings SET deleted_at=?, updated_at=?, machine_id=? WHERE id=? AND deleted_at=0`,
			now, now, s.machineID, id); err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM mem_learning_files WHERE learning_id=?`, id)
		}
	}
	if err != nil {
		return "", err
	}
	return short, tx.Commit()
}

// RestoreMemory brings a deleted rule or learning back under the same alias.
func (s *Store) RestoreMemory(ctx context.Context, ref string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	kind, id, err := memoryKindOf(ctx, tx, ref, true)
	if err != nil {
		return "", err
	}
	now := s.nowMs()
	var short string
	if kind == KindRule {
		rs, err := queryRules(ctx, tx, `WHERE id=?`, id)
		if err != nil || len(rs) == 0 {
			return "", errors.Join(err, memory.ErrNotFound)
		}
		r := rs[0]
		if r.Question == "(deleted)" {
			r.Question = ""
		}
		if err := memory.ValidateRule(r); err != nil {
			return "", err
		}
		r.DeletedAt, r.UpdatedAt, r.Machine = 0, now, s.machineID
		if err := upsertRule(ctx, tx, r); err != nil {
			return "", err
		}
		short = r.Short
	} else {
		ls, err := queryLearnings(ctx, tx, `WHERE id=?`, id)
		if err != nil || len(ls) == 0 {
			return "", errors.Join(err, memory.ErrNotFound)
		}
		l := ls[0]
		var other string
		err = tx.QueryRowContext(ctx, `SELECT short FROM mem_learnings WHERE repo=? AND hash=? AND deleted_at=0 AND id<>?`,
			l.Repo, l.Hash, l.ID).Scan(&other)
		if err == nil {
			return "", fmt.Errorf("%s holds the same learning: edit or rm it first", other)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		l.DeletedAt, l.UpdatedAt, l.Machine = 0, now, s.machineID
		if err := upsertLearning(ctx, tx, l); err != nil {
			return "", err
		}
		short = l.Short
	}
	return short, tx.Commit()
}

// ---- closed features ----

func loadFeatures(ctx context.Context, q queryer, where string, args ...any) ([]memory.ClosedFeature, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, title, decisions, findings, created_at, updated_at, deleted_at, machine_id
		FROM mem_features `+where, args...)
	if err != nil {
		return nil, err
	}
	var out []memory.ClosedFeature
	for rows.Next() {
		var f memory.ClosedFeature
		var dec, fin string
		if err := rows.Scan(&f.ID, &f.Title, &dec, &fin, &f.CreatedAt, &f.UpdatedAt, &f.DeletedAt, &f.Machine); err != nil {
			rows.Close()
			return nil, err
		}
		f.Decisions, f.Findings = decodeStrings(dec), decodeStrings(fin)
		out = append(out, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		r, err := q.QueryContext(ctx, `SELECT repo FROM mem_feature_repos WHERE feature_id=? ORDER BY repo`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for r.Next() {
			var repo string
			if err := r.Scan(&repo); err != nil {
				r.Close()
				return nil, err
			}
			out[i].Repos = append(out[i].Repos, repo)
		}
		r.Close()
		fr, err := q.QueryContext(ctx, `SELECT repo, path FROM mem_feature_files WHERE feature_id=? ORDER BY repo, path`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for fr.Next() {
			var f memory.FileRef
			if err := fr.Scan(&f.Repo, &f.Path); err != nil {
				fr.Close()
				return nil, err
			}
			out[i].Files = append(out[i].Files, f)
		}
		fr.Close()
	}
	return out, nil
}

func upsertFeature(ctx context.Context, q queryer, f memory.ClosedFeature) error {
	if stale, err := staleRow(ctx, q, "mem_features", f.ID, f.UpdatedAt); err != nil || stale {
		return err
	}
	_, err := q.ExecContext(ctx, `INSERT INTO mem_features (id, title, decisions, findings, created_at, updated_at, deleted_at, machine_id)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET title=excluded.title, decisions=excluded.decisions, findings=excluded.findings,
		created_at=excluded.created_at, updated_at=excluded.updated_at, deleted_at=excluded.deleted_at, machine_id=excluded.machine_id`,
		f.ID, f.Title, encodeStrings(f.Decisions), encodeStrings(f.Findings), f.CreatedAt, f.UpdatedAt, f.DeletedAt, f.Machine)
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM mem_feature_repos WHERE feature_id=?`, f.ID); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM mem_feature_files WHERE feature_id=?`, f.ID); err != nil {
		return err
	}
	repos := map[string]bool{}
	for _, r := range f.Repos {
		repos[r] = true
	}
	for _, fr := range f.Files {
		repos[fr.Repo] = true
		if p := memory.CleanPath(fr.Path); p != "" {
			if _, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO mem_feature_files (feature_id, repo, path) VALUES (?,?,?)`,
				f.ID, fr.Repo, p); err != nil {
				return err
			}
		}
	}
	for r := range repos {
		if _, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO mem_feature_repos (feature_id, repo) VALUES (?,?)`, f.ID, r); err != nil {
			return err
		}
	}
	return nil
}

// SaveClosedFeature stores what is kept of a closed feature. An empty ID gets a fresh ULID; an
// existing ID is updated in place (the feature id of the v2 flow can be used as is).
func (s *Store) SaveClosedFeature(ctx context.Context, f memory.ClosedFeature) (memory.ClosedFeature, error) {
	if strings.TrimSpace(f.Title) == "" {
		return memory.ClosedFeature{}, errors.New("a closed feature needs a title")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.ClosedFeature{}, err
	}
	defer tx.Rollback()
	t := s.now()
	now := t.UTC().UnixMilli()
	if f.ID == "" {
		f.ID = newULID(t, rand.Reader)
	}
	f.CreatedAt = now
	var created int64
	if err := tx.QueryRowContext(ctx, `SELECT created_at FROM mem_features WHERE id=?`, f.ID).Scan(&created); err == nil {
		f.CreatedAt = created
	} else if !errors.Is(err, sql.ErrNoRows) {
		return memory.ClosedFeature{}, err
	}
	f.UpdatedAt, f.DeletedAt, f.Machine = now, 0, s.machineID
	for i := range f.Files {
		f.Files[i].Path = memory.CleanPath(f.Files[i].Path)
	}
	if err := upsertFeature(ctx, tx, f); err != nil {
		return memory.ClosedFeature{}, err
	}
	got, err := loadFeatures(ctx, tx, `WHERE id=?`, f.ID)
	if err != nil || len(got) == 0 {
		return memory.ClosedFeature{}, errors.Join(err, memory.ErrNotFound)
	}
	return got[0], tx.Commit()
}

// Related returns the closed features and the open learnings of a repo that touched any of
// the paths (relative to the repo root; a directory matches the files under it).
func (s *Store) Related(ctx context.Context, repo string, paths []string) ([]memory.ClosedFeature, []memory.Learning, error) {
	match := func(stored string) bool {
		for _, p := range paths {
			if memory.PathMatches(stored, p) {
				return true
			}
		}
		return false
	}
	collect := func(query string) ([]string, error) {
		rows, err := s.db.QueryContext(ctx, query, repo)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		seen := map[string]bool{}
		var ids []string
		for rows.Next() {
			var id, p string
			if err := rows.Scan(&id, &p); err != nil {
				return nil, err
			}
			if !seen[id] && match(p) {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		return ids, rows.Err()
	}
	fids, err := collect(`SELECT f.feature_id, f.path FROM mem_feature_files f JOIN mem_features m ON m.id=f.feature_id
		WHERE f.repo=? AND m.deleted_at=0`)
	if err != nil {
		return nil, nil, err
	}
	lids, err := collect(`SELECT f.learning_id, f.path FROM mem_learning_files f JOIN mem_learnings m ON m.id=f.learning_id
		WHERE f.repo=? AND m.deleted_at=0 AND m.promoted_to='' AND m.obsolete_at=0`)
	if err != nil {
		return nil, nil, err
	}
	var feats []memory.ClosedFeature
	for _, id := range fids {
		got, err := loadFeatures(ctx, s.db, `WHERE id=?`, id)
		if err != nil {
			return nil, nil, err
		}
		feats = append(feats, got...)
	}
	sort.SliceStable(feats, func(i, j int) bool {
		if feats[i].UpdatedAt != feats[j].UpdatedAt {
			return feats[i].UpdatedAt > feats[j].UpdatedAt
		}
		return feats[i].ID < feats[j].ID
	})
	var learns []memory.Learning
	for _, id := range lids {
		got, err := queryLearnings(ctx, s.db, `WHERE id=?`, id)
		if err != nil {
			return nil, nil, err
		}
		learns = append(learns, got...)
	}
	sort.SliceStable(learns, func(i, j int) bool {
		if learns[i].Seen != learns[j].Seen {
			return learns[i].Seen > learns[j].Seen
		}
		return learns[i].ID < learns[j].ID
	})
	return feats, learns, nil
}

// Search runs a full-text query over rules, learnings and closed features, best match first. It
// looks at one repo unless all is set.
func (s *Store) Search(ctx context.Context, repo string, all bool, q string, limit int) ([]memory.Hit, error) {
	fts := memory.FTSQuery(q)
	if fts == "" {
		return nil, nil
	}
	if limit <= 0 || limit > memory.SearchLimit {
		limit = memory.SearchLimit
	}
	allFlag := 0
	if all {
		allFlag = 1
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT mem_fts.kind, mem_fts.row_id, mem_fts.repo,
		  CASE mem_fts.kind WHEN 'rule' THEN r.text WHEN 'learning' THEN l.text ELSE f.title END,
		  COALESCE(r.short, l.short, '')
		FROM mem_fts
		LEFT JOIN mem_rules r ON mem_fts.kind='rule' AND r.id=mem_fts.row_id
		LEFT JOIN mem_learnings l ON mem_fts.kind='learning' AND l.id=mem_fts.row_id
		LEFT JOIN mem_features f ON mem_fts.kind='feature' AND f.id=mem_fts.row_id
		WHERE mem_fts MATCH ?
		  AND (mem_fts.kind <> 'rule' OR NOT EXISTS
		       (SELECT 1 FROM mem_rules n WHERE n.supersedes=r.id AND n.deleted_at=0))
		  AND (mem_fts.kind <> 'learning' OR (l.promoted_to='' AND l.obsolete_at=0))
		  AND (? = 1 OR mem_fts.repo = ? OR (mem_fts.repo = '' AND mem_fts.kind IN ('rule','learning')) OR (mem_fts.kind='feature' AND EXISTS
		       (SELECT 1 FROM mem_feature_repos fr WHERE fr.feature_id=mem_fts.row_id AND fr.repo=?)))
		ORDER BY bm25(mem_fts), mem_fts.row_id
		LIMIT ?`, fts, allFlag, repo, repo, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []memory.Hit
	for rows.Next() {
		var h memory.Hit
		if err := rows.Scan(&h.Kind, &h.ID, &h.Repo, &h.Text, &h.Short); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ---- notes ----

const noteCols = `id, short, folder, title, body, session, feature_id, created_at, updated_at, deleted_at, machine_id`

func scanNote(sc scanner) (memory.Note, error) {
	var n memory.Note
	err := sc.Scan(&n.ID, &n.Short, &n.Folder, &n.Title, &n.Body, &n.Session, &n.FeatureID,
		&n.CreatedAt, &n.UpdatedAt, &n.DeletedAt, &n.Machine)
	return n, err
}

func queryNotes(ctx context.Context, q queryer, where string, args ...any) ([]memory.Note, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+noteCols+` FROM mem_notes `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []memory.Note
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func upsertNote(ctx context.Context, q queryer, n memory.Note) error {
	if stale, err := staleRow(ctx, q, "mem_notes", n.ID, n.UpdatedAt); err != nil || stale {
		return err
	}
	if n.Deleted() {
		n.Body = "" // a tombstone stays as a small row
	}
	_, err := q.ExecContext(ctx, `INSERT INTO mem_notes (`+noteCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET folder=excluded.folder, title=excluded.title, body=excluded.body,
		session=excluded.session, feature_id=excluded.feature_id, created_at=excluded.created_at,
		updated_at=excluded.updated_at, deleted_at=excluded.deleted_at, machine_id=excluded.machine_id`,
		n.ID, n.Short, n.Folder, n.Title, n.Body, n.Session, n.FeatureID, n.CreatedAt, n.UpdatedAt, n.DeletedAt, n.Machine)
	return err
}

// SaveNote stores a temporary note. With an ID (a ULID or N2) it updates that note.
func (s *Store) SaveNote(ctx context.Context, n memory.Note) (memory.Note, error) {
	if err := memory.ValidateNote(n); err != nil {
		return memory.Note{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.Note{}, err
	}
	defer tx.Rollback()
	t := s.now()
	now := t.UTC().UnixMilli()
	if n.ID != "" {
		id, err := resolveRef(ctx, tx, KindNote, n.ID)
		if err != nil {
			return memory.Note{}, err
		}
		cur, err := queryNotes(ctx, tx, `WHERE id=?`, id)
		if err != nil || len(cur) == 0 {
			return memory.Note{}, errors.Join(err, memory.ErrNotFound)
		}
		old := cur[0]
		old.Title, old.Body, old.UpdatedAt, old.Machine = n.Title, n.Body, now, s.machineID
		if n.Folder != "" {
			old.Folder = n.Folder
		}
		if n.Session != "" {
			old.Session = n.Session
		}
		if n.FeatureID != "" {
			old.FeatureID = n.FeatureID
		}
		if err := upsertNote(ctx, tx, old); err != nil {
			return memory.Note{}, err
		}
		return old, tx.Commit()
	}
	if strings.TrimSpace(n.Folder) == "" {
		return memory.Note{}, errors.New("a note needs a folder")
	}
	n.ID, n.Machine = newULID(t, rand.Reader), s.machineID
	n.CreatedAt, n.UpdatedAt, n.DeletedAt = now, now, 0
	if n.Short, err = nextShort(ctx, tx, KindNote, "N"); err != nil {
		return memory.Note{}, err
	}
	if err := upsertNote(ctx, tx, n); err != nil {
		return memory.Note{}, err
	}
	return n, tx.Commit()
}

// Notes lists the live, not expired notes whose folder contains or is contained by folder
// (an empty folder lists them all), newest first.
func (s *Store) Notes(ctx context.Context, folder string, foldCase bool, now time.Time) ([]memory.Note, error) {
	all, err := queryNotes(ctx, s.db, `WHERE deleted_at=0 ORDER BY updated_at DESC, id`)
	if err != nil {
		return nil, err
	}
	var out []memory.Note
	for _, n := range all {
		if memory.NoteExpired(n, now) {
			continue
		}
		if folder != "" && !memory.FolderMatch(n.Folder, folder, foldCase) {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

// DeleteNote marks a note as deleted (a tombstone, so the deletion can travel).
func (s *Store) DeleteNote(ctx context.Context, ref string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id, err := resolveRef(ctx, tx, KindNote, ref)
	if err != nil {
		return err
	}
	now := s.nowMs()
	if _, err := tx.ExecContext(ctx, `UPDATE mem_notes SET deleted_at=?, updated_at=?, body='' WHERE id=?`, now, now, id); err != nil {
		return err
	}
	return tx.Commit()
}

// Note reads one live note by N3 or ULID.
func (s *Store) Note(ctx context.Context, ref string) (memory.Note, error) {
	id, err := resolveRef(ctx, s.db, KindNote, ref)
	if err != nil {
		return memory.Note{}, err
	}
	got, err := queryNotes(ctx, s.db, `WHERE id=? AND deleted_at=0`, id)
	if err != nil {
		return memory.Note{}, err
	}
	if len(got) == 0 {
		return memory.Note{}, memory.ErrNotFound
	}
	return got[0], nil
}

// DeleteNotesOfFeature turns the live notes of a feature into tombstones and returns how many.
func (s *Store) DeleteNotesOfFeature(ctx context.Context, featureID string) (int, error) {
	if featureID == "" {
		return 0, nil
	}
	stamp := s.nowMs()
	res, err := s.db.ExecContext(ctx,
		`UPDATE mem_notes SET deleted_at=?, updated_at=?, body='' WHERE deleted_at=0 AND feature_id=?`, stamp, stamp, featureID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// DeleteNotesUnder turns the live notes whose folder is folder or inside it into tombstones and
// returns how many.
func (s *Store) DeleteNotesUnder(ctx context.Context, folder string, foldCase bool) (int, error) {
	if folder == "" {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	all, err := queryNotes(ctx, tx, `WHERE deleted_at=0`)
	if err != nil {
		return 0, err
	}
	stamp, count := s.nowMs(), 0
	for _, n := range all {
		if !memory.FolderMatch(n.Folder, folder, foldCase) || len(n.Folder) < len(folder) {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE mem_notes SET deleted_at=?, updated_at=?, body='' WHERE id=?`, stamp, stamp, n.ID); err != nil {
			return 0, err
		}
		count++
	}
	return count, tx.Commit()
}

// filterSessionTTL is how long the filter summary of a session is kept. It is longer than the 30
// days after which Claude Code deletes the transcripts, where trend gets its sessions from: a
// session without transcript no longer shows in trend or stats. Fixed, not configurable.
const filterSessionTTL = 90 * 24 * time.Hour

// agentRunTTL is how long the agent_runs rows are kept (same span as the filter summary).
const agentRunTTL = 90 * 24 * time.Hour

// SweepNotes turns the notes not updated for memory.NoteTTL into tombstones and returns how many.
// In the same pass it deletes the filter summary of the sessions whose last row is older than
// filterSessionTTL (the whole session, never single rows). legacy_filter_cursor is left alone, so
// what was swept is not imported again.
func (s *Store) SweepNotes(ctx context.Context, now time.Time) (int, error) {
	cutoff := now.Add(-memory.NoteTTL).UTC().UnixMilli()
	stamp := s.nowMs()
	res, err := s.db.ExecContext(ctx,
		`UPDATE mem_notes SET deleted_at=?, updated_at=?, body='' WHERE deleted_at=0 AND updated_at<=?`, stamp, stamp, cutoff)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM filter_sessions WHERE (checkout, session_id) IN
		   (SELECT checkout, session_id FROM filter_sessions GROUP BY checkout, session_id HAVING MAX(updated_at) < ?)`,
		now.Add(-filterSessionTTL).UTC().UnixMilli()); err != nil {
		return int(n), err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM agent_runs WHERE ended_at < ?`,
		now.Add(-agentRunTTL).UTC().UnixMilli()); err != nil {
		return int(n), err
	}
	return int(n), nil
}

// ---- export / import ----

func snapshotOf(ctx context.Context, q queryer, withNotes bool) (memory.Bundle, error) {
	var b memory.Bundle
	var err error
	if b.Rules, err = queryRules(ctx, q, `ORDER BY id`); err != nil {
		return b, err
	}
	if b.Features, err = loadFeatures(ctx, q, `ORDER BY id`); err != nil {
		return b, err
	}
	if b.Learnings, err = queryLearnings(ctx, q, `ORDER BY id`); err != nil {
		return b, err
	}
	if withNotes {
		if b.Notes, err = queryNotes(ctx, q, `ORDER BY id`); err != nil {
			return b, err
		}
	}
	return b, nil
}

// ExportBundle reads all the memory, tombstones included. Notes go only when includeNotes is set.
// Learning aliases are local and never exported.
func (s *Store) ExportBundle(ctx context.Context, includeNotes bool) (memory.Bundle, error) {
	b, err := snapshotOf(ctx, s.db, includeNotes)
	if err != nil {
		return memory.Bundle{}, err
	}
	b.Version, b.Machine, b.ExportedAt = memory.BundleVersion, s.machineID, s.nowMs()
	return b, nil
}

// MemorySnapshot is the local state memory.Merge compares an incoming bundle against: every row
// (tombstones included) and the learning aliases.
func (s *Store) MemorySnapshot(ctx context.Context) (memory.Snapshot, error) {
	b, err := snapshotOf(ctx, s.db, true)
	if err != nil {
		return memory.Snapshot{}, err
	}
	snap := memory.Snapshot{Rules: b.Rules, Features: b.Features, Learnings: b.Learnings, Notes: b.Notes}
	rows, err := s.db.QueryContext(ctx, `SELECT alias_id, learning_id, alias_updated_at, alias_seen FROM mem_learning_aliases ORDER BY alias_id`)
	if err != nil {
		return snap, err
	}
	defer rows.Close()
	for rows.Next() {
		var a memory.Alias
		if err := rows.Scan(&a.ID, &a.Into, &a.UpdatedAt, &a.Seen); err != nil {
			return snap, err
		}
		snap.Aliases = append(snap.Aliases, a)
	}
	return snap, rows.Err()
}

// foldLearning adds an incoming duplicate to the local learning that absorbs it. The first time
// the whole incoming Seen is added; if the same ULID comes again with a newer UpdatedAt, only
// the growth since the last fold is added (alias_seen remembers it). The alias is local state.
func (s *Store) foldLearning(ctx context.Context, q queryer, into string, in memory.Learning, now int64) error {
	add := in.Seen
	var prev int
	err := q.QueryRowContext(ctx, `SELECT alias_seen FROM mem_learning_aliases WHERE alias_id=?`, in.ID).Scan(&prev)
	switch {
	case err == nil:
		add = max(0, in.Seen-prev)
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}
	if _, err := q.ExecContext(ctx, `UPDATE mem_learnings SET seen=seen+?, last_seen_at=MAX(last_seen_at, ?) WHERE id=?`,
		add, in.LastSeenAt, into); err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO mem_learning_aliases (alias_id, learning_id, alias_updated_at, alias_seen) VALUES (?,?,?,?)
		ON CONFLICT(alias_id) DO UPDATE SET learning_id=excluded.learning_id, alias_updated_at=excluded.alias_updated_at,
		alias_seen=excluded.alias_seen`, in.ID, into, in.UpdatedAt, in.Seen)
	return err
}

// ApplyImport writes a memory.Diff in a single transaction: either all of it or nothing. Rows keep
// the machine and updated_at they came with; an alias that clashes is replaced by a fresh one.
func (s *Store) ApplyImport(ctx context.Context, d memory.Diff) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.nowMs()
	for _, c := range d.Features {
		if c.Kind == memory.New || c.Kind == memory.Changed {
			if err := upsertFeature(ctx, tx, c.Row); err != nil {
				return err
			}
		}
	}
	for _, c := range d.Rules {
		if c.Kind == memory.Skipped && c.Counters {
			if _, err := tx.ExecContext(ctx, `UPDATE mem_rules SET deliveries=MAX(deliveries, ?), citations=MAX(citations, ?),
				last_cited_at=MAX(last_cited_at, ?) WHERE id=?`, c.Row.Deliveries, c.Row.Citations, c.Row.LastCitedAt, c.Row.ID); err != nil {
				return err
			}
			continue
		}
		if c.Kind != memory.New && c.Kind != memory.Changed {
			continue
		}
		r := c.Row
		if c.Kind == memory.New {
			if r.Short, err = shortFor(ctx, tx, KindRule, r.Repo, r.Short, c.ReassignShort); err != nil {
				return err
			}
		}
		if err := upsertRule(ctx, tx, r); err != nil {
			return err
		}
	}
	for _, c := range d.Learnings {
		l := c.Row
		switch c.Kind {
		case memory.Skipped:
			if c.Counters {
				if _, err := tx.ExecContext(ctx, `UPDATE mem_learnings SET seen=MAX(seen, ?), last_seen_at=MAX(last_seen_at, ?) WHERE id=?`,
					l.Seen, l.LastSeenAt, l.ID); err != nil {
					return err
				}
			}
		case memory.Folded:
			if err := s.foldLearning(ctx, tx, c.FoldInto, l, now); err != nil {
				return err
			}
		case memory.New, memory.Changed:
			if c.Kind == memory.New {
				if !l.Deleted() {
					// two new rows of the same bundle can share a hash: the second folds into the first
					var dup string
					err := tx.QueryRowContext(ctx, `SELECT id FROM mem_learnings WHERE repo=? AND hash=? AND deleted_at=0 AND id<>?`,
						l.Repo, l.Hash, l.ID).Scan(&dup)
					if err == nil {
						if err := s.foldLearning(ctx, tx, dup, l, now); err != nil {
							return err
						}
						continue
					}
					if !errors.Is(err, sql.ErrNoRows) {
						return err
					}
				}
				if l.Short, err = shortFor(ctx, tx, KindLearning, l.Repo, l.Short, c.ReassignShort); err != nil {
					return err
				}
			}
			if c.Kind == memory.Changed && !l.Deleted() {
				// an edit or restore from the other machine may land on a hash a live local row holds:
				// fold it into that row and keep it as a tombstone instead of breaking the unique index
				var dup string
				err := tx.QueryRowContext(ctx, `SELECT id FROM mem_learnings WHERE repo=? AND hash=? AND deleted_at=0 AND id<>?`,
					l.Repo, l.Hash, l.ID).Scan(&dup)
				if err == nil {
					if err := s.foldLearning(ctx, tx, dup, l, now); err != nil {
						return err
					}
					l.DeletedAt = l.UpdatedAt
				} else if !errors.Is(err, sql.ErrNoRows) {
					return err
				}
			}
			if err := upsertLearning(ctx, tx, l); err != nil {
				return err
			}
		}
	}
	for _, c := range d.Notes {
		if c.Kind != memory.New && c.Kind != memory.Changed {
			continue
		}
		n := c.Row
		if c.Kind == memory.New {
			if n.Short, err = shortFor(ctx, tx, KindNote, "", n.Short, c.ReassignShort); err != nil {
				return err
			}
		}
		if err := upsertNote(ctx, tx, n); err != nil {
			return err
		}
	}
	return tx.Commit()
}
