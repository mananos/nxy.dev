-- Questions of nxy held by the PreToolUse hook while a TUI may answer them. Local to the machine.
CREATE TABLE pending_asks (
  id          TEXT    PRIMARY KEY,
  session_id  TEXT    NOT NULL DEFAULT '',
  feature_id  TEXT    NOT NULL DEFAULT '',
  tool_use_id TEXT    NOT NULL DEFAULT '',
  questions   TEXT    NOT NULL DEFAULT '[]',
  answer      TEXT,
  answered_by TEXT    NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL DEFAULT 0,
  answered_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX pending_asks_feature ON pending_asks (feature_id);
