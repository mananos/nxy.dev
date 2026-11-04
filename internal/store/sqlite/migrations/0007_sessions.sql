-- Per-session state of the hooks (notes question, chained note, Stop asked once, edits, read hint).
-- Local to this machine: the memory Bundle never carries it. Pruned after 7 days.
CREATE TABLE sessions (
  session_id TEXT PRIMARY KEY,
  folder TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT '',
  started_at INTEGER NOT NULL DEFAULT 0,
  notes_pending INTEGER NOT NULL DEFAULT 0,
  recommended_note TEXT NOT NULL DEFAULT '',
  note_id TEXT NOT NULL DEFAULT '',
  stop_asked INTEGER NOT NULL DEFAULT 0,
  last_edit_at INTEGER NOT NULL DEFAULT 0,
  read_hint_at INTEGER NOT NULL DEFAULT 0,
  ended_at INTEGER NOT NULL DEFAULT 0,
  end_reason TEXT NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX sessions_ended ON sessions (ended_at);
