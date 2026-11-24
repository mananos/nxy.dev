-- One row per session transcript: its parsed summary (JSON) and the signature of the files it was
-- read from. Local to the machine: the memory Bundle never carries it.
CREATE TABLE session_usage (
  path       TEXT    PRIMARY KEY,
  session_id TEXT    NOT NULL,
  stamp      TEXT    NOT NULL,
  mtime_ms   INTEGER NOT NULL,
  data       TEXT    NOT NULL,
  saved_at   INTEGER NOT NULL
);
CREATE INDEX session_usage_mtime ON session_usage (mtime_ms);
