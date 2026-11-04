-- Bootstrap of the first rules of a repo: offered once, with the candidates while they are confirmed
-- and the request/engineer flag of the feature that waits for it. Local to this machine: the memory
-- Bundle never carries it.
CREATE TABLE mem_bootstrap (
  repo TEXT PRIMARY KEY,
  offered_at INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT '',
  candidates TEXT NOT NULL DEFAULT '[]',
  pending_request TEXT NOT NULL DEFAULT '',
  pending_engineer INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL DEFAULT 0
);
