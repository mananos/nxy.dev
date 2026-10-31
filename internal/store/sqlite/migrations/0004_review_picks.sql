CREATE TABLE review_picks (
  feature_id TEXT NOT NULL,
  finding TEXT NOT NULL,
  repo TEXT NOT NULL DEFAULT '',
  lens TEXT NOT NULL,
  severity TEXT NOT NULL,
  chosen INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (feature_id, finding)
);
