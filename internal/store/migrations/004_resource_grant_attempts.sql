ALTER TABLE resource_grants ADD COLUMN withdrawal_id TEXT REFERENCES withdrawals(id);
ALTER TABLE resource_grants ADD COLUMN receiver_address TEXT;
ALTER TABLE resource_grants ADD COLUMN broadcast_attempted_at INTEGER;
ALTER TABLE resource_grants ADD COLUMN broadcast_response TEXT;
ALTER TABLE resource_grants ADD COLUMN expiration_at INTEGER;
ALTER TABLE resource_grants ADD COLUMN failure_reason TEXT;
ALTER TABLE resource_grants ADD COLUMN lookup_failures INTEGER NOT NULL DEFAULT 0;
ALTER TABLE resource_grants ADD COLUMN last_lookup_error TEXT;

CREATE UNIQUE INDEX idx_resource_grants_withdrawal_resource
    ON resource_grants(withdrawal_id, resource_type)
    WHERE withdrawal_id IS NOT NULL;
CREATE INDEX idx_resource_grants_txid ON resource_grants(txid) WHERE txid IS NOT NULL;
