ALTER TABLE withdrawals ADD COLUMN expiration_at INTEGER;
ALTER TABLE withdrawals ADD COLUMN last_lookup_at INTEGER;
ALTER TABLE withdrawals ADD COLUMN lookup_failures INTEGER NOT NULL DEFAULT 0;
ALTER TABLE withdrawals ADD COLUMN last_lookup_error TEXT;
ALTER TABLE withdrawals ADD COLUMN requested_ip TEXT;
ALTER TABLE withdrawals ADD COLUMN status_updated_at INTEGER;

CREATE INDEX idx_withdrawals_lookup ON withdrawals(status, last_lookup_at);
