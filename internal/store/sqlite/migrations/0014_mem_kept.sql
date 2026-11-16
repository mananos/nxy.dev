-- "Dejarla": the user kept a rule or learning that nxy proposed to delete or promote. Local to the machine.
ALTER TABLE mem_rules ADD COLUMN kept_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE mem_learnings ADD COLUMN kept_at INTEGER NOT NULL DEFAULT 0;
