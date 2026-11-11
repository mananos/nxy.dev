-- One row per subagent of a feature. Local to the machine: the memory Bundle never carries it.
CREATE TABLE agent_runs (
  feature_id     TEXT    NOT NULL,
  agent_id       TEXT    NOT NULL,
  batch          INTEGER NOT NULL DEFAULT 0,
  role           TEXT    NOT NULL DEFAULT '',
  model          TEXT    NOT NULL DEFAULT '',
  effort         TEXT    NOT NULL DEFAULT '',
  calls          INTEGER NOT NULL DEFAULT 0,
  tokens_in      INTEGER NOT NULL DEFAULT 0,
  tokens_out     INTEGER NOT NULL DEFAULT 0,
  cache_read     INTEGER NOT NULL DEFAULT 0,
  cache_write    INTEGER NOT NULL DEFAULT 0,
  cost_usd       REAL    NOT NULL DEFAULT 0,
  cost_estimated INTEGER NOT NULL DEFAULT 0,
  started_at     INTEGER NOT NULL DEFAULT 0,
  ended_at       INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (feature_id, agent_id)
);
CREATE INDEX agent_runs_ended ON agent_runs (ended_at);
