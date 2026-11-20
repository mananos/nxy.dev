CREATE TABLE mem_rules (
  id TEXT PRIMARY KEY,
  short TEXT NOT NULL UNIQUE,
  repo TEXT NOT NULL,
  text TEXT NOT NULL,
  question TEXT NOT NULL CHECK (length(trim(question)) > 0),
  origin TEXT NOT NULL,
  feature_id TEXT NOT NULL DEFAULT '',
  paths TEXT NOT NULL DEFAULT '[]',
  supersedes TEXT NOT NULL DEFAULT '',
  deliveries INTEGER NOT NULL DEFAULT 0,
  citations INTEGER NOT NULL DEFAULT 0,
  last_cited_at INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  deleted_at INTEGER NOT NULL DEFAULT 0,
  machine_id TEXT NOT NULL DEFAULT ''
);
CREATE INDEX mem_rules_repo ON mem_rules (repo, deleted_at);
CREATE INDEX mem_rules_supersedes ON mem_rules (supersedes) WHERE supersedes <> '';

CREATE TABLE mem_features (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  decisions TEXT NOT NULL DEFAULT '[]',
  findings TEXT NOT NULL DEFAULT '[]',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  deleted_at INTEGER NOT NULL DEFAULT 0,
  machine_id TEXT NOT NULL DEFAULT ''
);
CREATE TABLE mem_feature_repos (
  feature_id TEXT NOT NULL,
  repo TEXT NOT NULL,
  PRIMARY KEY (feature_id, repo)
);
CREATE INDEX mem_feature_repos_repo ON mem_feature_repos (repo);
CREATE TABLE mem_feature_files (
  feature_id TEXT NOT NULL,
  repo TEXT NOT NULL,
  path TEXT NOT NULL,
  PRIMARY KEY (feature_id, repo, path)
);
CREATE INDEX mem_feature_files_path ON mem_feature_files (repo, path);

CREATE TABLE mem_learnings (
  id TEXT PRIMARY KEY,
  short TEXT NOT NULL UNIQUE,
  repo TEXT NOT NULL,
  text TEXT NOT NULL,
  files TEXT NOT NULL DEFAULT '[]',
  feature_id TEXT NOT NULL DEFAULT '',
  hash TEXT NOT NULL,
  seen INTEGER NOT NULL DEFAULT 1,
  last_seen_at INTEGER NOT NULL DEFAULT 0,
  obsolete_at INTEGER NOT NULL DEFAULT 0,
  promoted_to TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  deleted_at INTEGER NOT NULL DEFAULT 0,
  machine_id TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX mem_learnings_live_hash ON mem_learnings (repo, hash) WHERE deleted_at = 0;
CREATE TABLE mem_learning_files (
  learning_id TEXT NOT NULL,
  repo TEXT NOT NULL,
  path TEXT NOT NULL,
  PRIMARY KEY (learning_id, path)
);
CREATE INDEX mem_learning_files_path ON mem_learning_files (repo, path);

-- Local only, never exported: ULIDs of foreign learnings that were folded into a local one,
-- so importing the same bundle again changes nothing.
CREATE TABLE mem_learning_aliases (
  alias_id TEXT PRIMARY KEY,
  learning_id TEXT NOT NULL,
  alias_updated_at INTEGER NOT NULL,
  alias_seen INTEGER NOT NULL
);

CREATE TABLE mem_notes (
  id TEXT PRIMARY KEY,
  short TEXT NOT NULL UNIQUE,
  folder TEXT NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  session TEXT NOT NULL DEFAULT '',
  feature_id TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  deleted_at INTEGER NOT NULL DEFAULT 0,
  machine_id TEXT NOT NULL DEFAULT ''
);

CREATE VIRTUAL TABLE mem_fts USING fts5(
  body,
  kind UNINDEXED,
  row_id UNINDEXED,
  repo UNINDEXED,
  tokenize = 'unicode61 remove_diacritics 2'
);

CREATE TRIGGER mem_rules_ai AFTER INSERT ON mem_rules WHEN new.deleted_at = 0 BEGIN
  INSERT INTO mem_fts (body, kind, row_id, repo) VALUES (new.text || ' ' || new.question, 'rule', new.id, new.repo);
END;
CREATE TRIGGER mem_rules_au AFTER UPDATE ON mem_rules BEGIN
  DELETE FROM mem_fts WHERE kind = 'rule' AND row_id = old.id;
  INSERT INTO mem_fts (body, kind, row_id, repo) SELECT new.text || ' ' || new.question, 'rule', new.id, new.repo WHERE new.deleted_at = 0;
END;
CREATE TRIGGER mem_rules_ad AFTER DELETE ON mem_rules BEGIN
  DELETE FROM mem_fts WHERE kind = 'rule' AND row_id = old.id;
END;

CREATE TRIGGER mem_learnings_ai AFTER INSERT ON mem_learnings WHEN new.deleted_at = 0 BEGIN
  INSERT INTO mem_fts (body, kind, row_id, repo) VALUES (new.text, 'learning', new.id, new.repo);
END;
CREATE TRIGGER mem_learnings_au AFTER UPDATE ON mem_learnings BEGIN
  DELETE FROM mem_fts WHERE kind = 'learning' AND row_id = old.id;
  INSERT INTO mem_fts (body, kind, row_id, repo) SELECT new.text, 'learning', new.id, new.repo WHERE new.deleted_at = 0;
END;
CREATE TRIGGER mem_learnings_ad AFTER DELETE ON mem_learnings BEGIN
  DELETE FROM mem_fts WHERE kind = 'learning' AND row_id = old.id;
END;

-- A feature spans repos, so its index row has no repo: searches filter through mem_feature_repos.
CREATE TRIGGER mem_features_ai AFTER INSERT ON mem_features WHEN new.deleted_at = 0 BEGIN
  INSERT INTO mem_fts (body, kind, row_id, repo) VALUES (new.title || ' ' || new.decisions || ' ' || new.findings, 'feature', new.id, '');
END;
CREATE TRIGGER mem_features_au AFTER UPDATE ON mem_features BEGIN
  DELETE FROM mem_fts WHERE kind = 'feature' AND row_id = old.id;
  INSERT INTO mem_fts (body, kind, row_id, repo) SELECT new.title || ' ' || new.decisions || ' ' || new.findings, 'feature', new.id, '' WHERE new.deleted_at = 0;
END;
CREATE TRIGGER mem_features_ad AFTER DELETE ON mem_features BEGIN
  DELETE FROM mem_fts WHERE kind = 'feature' AND row_id = old.id;
END;
