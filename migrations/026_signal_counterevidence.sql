-- Signals keep bounded counterevidence from scheduled Reader extraction so users can
-- inspect the evidence gaps before accepting a draft into stable context.
ALTER TABLE signals
    ADD COLUMN counterevidence TEXT NOT NULL DEFAULT '[]';
