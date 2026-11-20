-- What the v2 copies from the 1.x. All three tables are local to this machine: the memory Bundle
-- never carries them.
-- Summary of the command filter per session, folded to groups. source is "1x" (read from the 1.x
-- filter.jsonl) or "v2" (written by the PostToolUse hook); a session written by the v2 never gets
-- rows of the 1.x. kind is empty without command_kind, else the JSON of the value; seq keeps the
-- order of first appearance.
CREATE TABLE filter_sessions (
  checkout TEXT NOT NULL,
  session_id TEXT NOT NULL,
  source TEXT NOT NULL,
  seq INTEGER NOT NULL,
  engine TEXT NOT NULL,
  reason TEXT NOT NULL,
  kind TEXT NOT NULL,
  rows INTEGER NOT NULL,
  chars REAL NOT NULL,
  tokens REAL NOT NULL,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (checkout, session_id, engine, reason, kind)
);
CREATE INDEX filter_sessions_session ON filter_sessions (checkout, session_id, seq);
-- Marks of what was imported once: scope "memory" (key = repo) and "review" (key = checkout).
CREATE TABLE legacy_imports (
  scope TEXT NOT NULL,
  key TEXT NOT NULL,
  imported_at INTEGER NOT NULL,
  counts TEXT NOT NULL,
  PRIMARY KEY (scope, key)
);
-- How far the 1.x filter.jsonl of a checkout was read.
CREATE TABLE legacy_filter_cursor (
  checkout TEXT PRIMARY KEY,
  byte_offset INTEGER NOT NULL,
  size INTEGER NOT NULL,
  head TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);
