ALTER TABLE features ADD COLUMN repo TEXT NOT NULL DEFAULT '';
ALTER TABLE features ADD COLUMN done INTEGER NOT NULL DEFAULT 0;
CREATE INDEX features_repo_done ON features (repo, done);
