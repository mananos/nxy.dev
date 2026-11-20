-- The hook that holds a question renews touched_at on every reread; a row nobody renews belongs to a dead hook.
ALTER TABLE pending_asks ADD COLUMN touched_at INTEGER NOT NULL DEFAULT 0;
