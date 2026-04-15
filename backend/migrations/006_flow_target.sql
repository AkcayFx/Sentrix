-- Add dedicated target column to flows so agents always know the exact target
-- even when the user sends follow-up messages without re-specifying it.
ALTER TABLE flows ADD COLUMN IF NOT EXISTS target TEXT NOT NULL DEFAULT '';
