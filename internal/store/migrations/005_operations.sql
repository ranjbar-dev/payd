-- OPS-001: readiness needs the time at which solidity actually advanced,
-- not the unrelated crawler write timestamp.
ALTER TABLE crawler_state ADD COLUMN solidified_updated_at INTEGER NOT NULL DEFAULT 0;
UPDATE crawler_state SET solidified_updated_at = updated_at WHERE solidified_height > 0;
