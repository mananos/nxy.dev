-- One active feature per checkout (not per repo): a feature can hold several checkouts and a
-- paused feature holds none. Local to this machine like the rest of the features table.
CREATE TABLE feature_holds (
  checkout TEXT PRIMARY KEY,
  feature_id TEXT NOT NULL
);
CREATE INDEX feature_holds_feature ON feature_holds (feature_id);
ALTER TABLE features ADD COLUMN home TEXT NOT NULL DEFAULT '';
CREATE INDEX features_home ON features (home, done);
DROP INDEX features_one_active;
-- Features from before this migration keep holding their repo key until a hook adopts them.
INSERT INTO feature_holds (checkout, feature_id) SELECT repo, id FROM features WHERE done = 0;
ALTER TABLE sessions ADD COLUMN feature_id TEXT NOT NULL DEFAULT '';
