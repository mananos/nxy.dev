---
name: database
title: SQL and migrations
paths: **/*.sql, **/migrations/**, **/migration/**, **/db/changelog/**, **/evolutions/**, **/flyway/**, **/liquibase/**
content: CREATE TABLE, ALTER TABLE, CREATE INDEX, DROP TABLE, DROP COLUMN, DELETE FROM, INSERT INTO
---
- **Indexes** for the new queries, foreign keys and filters; none duplicated.
- **Locks**: `ALTER TABLE` / backfills on big tables that block writes; batches for large updates.
- **Reversibility and order**: the migration runs on a database that already has data, in the order the app deploys; a rollback path when the repo uses them (Play evolutions `!Downs`).
- **Data**: `NOT NULL` columns added with a default or a backfill; no destructive change (drop, rename) without the plan saying so.
