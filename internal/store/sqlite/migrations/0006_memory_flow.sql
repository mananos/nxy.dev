-- Counting a delivery, a citation or a sighting must not reindex FTS: the triggers now fire only
-- when an indexed column is written.
DROP TRIGGER mem_rules_au;
CREATE TRIGGER mem_rules_au AFTER UPDATE OF repo, text, question, deleted_at ON mem_rules BEGIN
  DELETE FROM mem_fts WHERE kind = 'rule' AND row_id = old.id;
  INSERT INTO mem_fts (body, kind, row_id, repo) SELECT new.text || ' ' || new.question, 'rule', new.id, new.repo WHERE new.deleted_at = 0;
END;
DROP TRIGGER mem_learnings_au;
CREATE TRIGGER mem_learnings_au AFTER UPDATE OF repo, text, deleted_at ON mem_learnings BEGIN
  DELETE FROM mem_fts WHERE kind = 'learning' AND row_id = old.id;
  INSERT INTO mem_fts (body, kind, row_id, repo) SELECT new.text, 'learning', new.id, new.repo WHERE new.deleted_at = 0;
END;

-- Review escapes: a problem the review missed. feature_id is empty when no feature was active.
CREATE TABLE review_escapes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  feature_id TEXT NOT NULL DEFAULT '',
  repo TEXT NOT NULL,
  lens TEXT NOT NULL DEFAULT '',
  text TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX review_escapes_repo ON review_escapes (repo, created_at);

-- ReviewStats filters review_picks by repo and created_at.
CREATE INDEX IF NOT EXISTS review_picks_repo ON review_picks (repo, created_at);
