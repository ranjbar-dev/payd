-- DB-001/002: monetary values are decimal-string base units; timestamps are UTC Unix seconds.
CREATE TABLE blocks (
    height        INTEGER PRIMARY KEY,
    block_id      TEXT NOT NULL,
    parent_id     TEXT NOT NULL,
    timestamp     INTEGER NOT NULL,
    tx_count      INTEGER NOT NULL DEFAULT 0,
    processed_at  INTEGER NOT NULL
);
CREATE INDEX idx_blocks_block_id ON blocks(block_id);

CREATE TABLE crawler_state (
    id                 INTEGER PRIMARY KEY CHECK (id = 1),
    last_height        INTEGER NOT NULL,
    solidified_height  INTEGER NOT NULL,
    updated_at         INTEGER NOT NULL
);

CREATE TABLE chain_params (
    name        TEXT PRIMARY KEY,
    value       INTEGER NOT NULL,
    fetched_at  INTEGER NOT NULL
);

CREATE TABLE addresses (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    hd_index             INTEGER NOT NULL UNIQUE,
    address              TEXT NOT NULL UNIQUE,
    state                TEXT NOT NULL DEFAULT 'free',
    assigned_order_id    TEXT REFERENCES orders(id),
    assigned_at          INTEGER,
    released_at          INTEGER,
    cooling_until        INTEGER,
    is_activated         INTEGER NOT NULL DEFAULT 0,
    energy_limit         INTEGER NOT NULL DEFAULT 0,
    energy_used          INTEGER NOT NULL DEFAULT 0,
    bandwidth_limit      INTEGER NOT NULL DEFAULT 0,
    bandwidth_used       INTEGER NOT NULL DEFAULT 0,
    needs_resources      INTEGER NOT NULL DEFAULT 0,
    resources_checked_at INTEGER,
    created_at           INTEGER NOT NULL
);
CREATE INDEX idx_addresses_state ON addresses(state);
CREATE INDEX idx_addresses_needs_resources ON addresses(needs_resources);

CREATE TABLE balances (
    address_id       INTEGER NOT NULL REFERENCES addresses(id),
    asset            TEXT NOT NULL,
    confirmed_raw    TEXT NOT NULL DEFAULT '0',
    pending_raw      TEXT NOT NULL DEFAULT '0',
    chain_raw        TEXT,
    last_verified_at INTEGER,
    drift_detected   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (address_id, asset)
);

CREATE TABLE orders (
    id                 TEXT PRIMARY KEY,
    external_ref       TEXT,
    address_id         INTEGER NOT NULL REFERENCES addresses(id),
    address            TEXT NOT NULL,
    asset              TEXT NOT NULL,
    expected_raw       TEXT NOT NULL,
    received_raw       TEXT NOT NULL DEFAULT '0',
    overpaid_raw       TEXT NOT NULL DEFAULT '0',
    status             TEXT NOT NULL DEFAULT 'pending',
    resolution         TEXT,
    resolution_note    TEXT,
    resolved_at        INTEGER,
    price_usd          TEXT,
    price_at           INTEGER,
    metadata           TEXT,
    consumer           TEXT,
    expires_at         INTEGER NOT NULL,
    created_at         INTEGER NOT NULL,
    updated_at         INTEGER NOT NULL
);
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_orders_address ON orders(address);
CREATE INDEX idx_orders_expires ON orders(expires_at) WHERE status IN ('pending','partial');
CREATE UNIQUE INDEX idx_orders_external_ref ON orders(external_ref) WHERE external_ref IS NOT NULL;
CREATE INDEX idx_orders_funded_terminal ON orders(status)
    WHERE status IN ('expired_funded','cancelled_funded') AND resolution IS NULL;

CREATE TABLE payments (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    txid            TEXT NOT NULL,
    log_index       INTEGER NOT NULL DEFAULT 0,
    direction       TEXT NOT NULL DEFAULT 'in' CHECK (direction IN ('in','out')),
    block_height    INTEGER NOT NULL,
    block_id        TEXT NOT NULL,
    block_timestamp INTEGER NOT NULL,
    from_address    TEXT NOT NULL,
    to_address      TEXT NOT NULL,
    address_id      INTEGER REFERENCES addresses(id),
    order_id        TEXT REFERENCES orders(id),
    asset           TEXT NOT NULL,
    amount_raw      TEXT NOT NULL,
    is_dust         INTEGER NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'seen',
    detected_at     INTEGER NOT NULL,
    confirmed_at    INTEGER,
    UNIQUE (txid, log_index)
);
CREATE INDEX idx_payments_status ON payments(status);
CREATE INDEX idx_payments_order ON payments(order_id);
CREATE INDEX idx_payments_height ON payments(block_height);
CREATE INDEX idx_payments_addr_asset ON payments(address_id, asset, direction, status);
CREATE INDEX idx_payments_orphaned ON payments(status, block_height) WHERE status = 'orphaned';

CREATE TABLE ipn_outbox (
    id               TEXT PRIMARY KEY,
    order_id         TEXT REFERENCES orders(id),
    sequence_key     TEXT NOT NULL,
    consumer         TEXT NOT NULL,
    target_url       TEXT NOT NULL,
    event_type       TEXT NOT NULL,
    payload          TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'pending',
    attempts         INTEGER NOT NULL DEFAULT 0,
    next_attempt_at  INTEGER NOT NULL,
    last_error       TEXT,
    last_status_code INTEGER,
    created_at       INTEGER NOT NULL,
    delivered_at     INTEGER
);
CREATE INDEX idx_ipn_pending ON ipn_outbox(next_attempt_at) WHERE status = 'pending';
CREATE INDEX idx_ipn_sequence ON ipn_outbox(sequence_key, consumer, created_at);

CREATE TABLE withdrawals (
    id                       TEXT PRIMARY KEY,
    idempotency_key          TEXT NOT NULL UNIQUE,
    address_id              INTEGER NOT NULL REFERENCES addresses(id),
    from_address            TEXT NOT NULL,
    to_address              TEXT NOT NULL,
    asset                   TEXT NOT NULL,
    amount_raw              TEXT NOT NULL,
    amount_usd              TEXT,
    status                  TEXT NOT NULL DEFAULT 'requested',
    txid                    TEXT,
    broadcast_attempted_at  INTEGER,
    broadcast_response      TEXT,
    fee_raw                 TEXT,
    energy_used             INTEGER,
    energy_source           TEXT,
    energy_cost_trx         TEXT,
    bandwidth_source        TEXT,
    failure_reason          TEXT,
    resolved_by             TEXT,
    requested_by            TEXT NOT NULL,
    created_at              INTEGER NOT NULL,
    broadcast_at            INTEGER,
    confirmed_at            INTEGER
);
CREATE INDEX idx_withdrawals_status ON withdrawals(status);
CREATE INDEX idx_withdrawals_created ON withdrawals(created_at);
CREATE INDEX idx_withdrawals_txid ON withdrawals(txid) WHERE txid IS NOT NULL;
CREATE INDEX idx_withdrawals_addr_active ON withdrawals(address_id, status);

CREATE TABLE used_totp (
    code    TEXT NOT NULL,
    step    INTEGER NOT NULL,
    used_at INTEGER NOT NULL,
    PRIMARY KEY (code, step)
);
CREATE INDEX idx_used_totp_used_at ON used_totp(used_at);

CREATE TABLE resource_grants (
    id            TEXT PRIMARY KEY,
    address_id    INTEGER NOT NULL REFERENCES addresses(id),
    resource_type TEXT NOT NULL,
    source        TEXT NOT NULL,
    amount_sun    TEXT NOT NULL,
    txid          TEXT,
    status        TEXT NOT NULL DEFAULT 'requested',
    created_at    INTEGER NOT NULL,
    confirmed_at  INTEGER
);

CREATE TABLE energy_purchases (
    id                TEXT PRIMARY KEY,
    provider          TEXT NOT NULL,
    provider_order_id TEXT,
    withdrawal_id     TEXT REFERENCES withdrawals(id),
    address_id        INTEGER NOT NULL REFERENCES addresses(id),
    receiver_address  TEXT NOT NULL,
    resource_type     TEXT NOT NULL DEFAULT 'ENERGY',
    energy_amount     INTEGER NOT NULL,
    duration_seconds  INTEGER NOT NULL,
    quoted_trx        TEXT NOT NULL,
    actual_trx        TEXT,
    status            TEXT NOT NULL DEFAULT 'quoted',
    delegation_txid   TEXT,
    failure_reason    TEXT,
    created_at        INTEGER NOT NULL,
    delegated_at      INTEGER
);
CREATE INDEX idx_energy_purchases_status ON energy_purchases(status);
CREATE INDEX idx_energy_purchases_withdrawal ON energy_purchases(withdrawal_id);

CREATE TABLE energy_provider_state (
    provider             TEXT PRIMARY KEY,
    balance_trx          TEXT NOT NULL DEFAULT '0',
    last_checked_at      INTEGER,
    last_error           TEXT,
    consecutive_failures INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE prices (
    symbol     TEXT PRIMARY KEY,
    price_usd  TEXT NOT NULL,
    source     TEXT NOT NULL,
    fetched_at INTEGER NOT NULL
);

CREATE TABLE worker_health (
    worker       TEXT PRIMARY KEY,
    last_tick_at INTEGER,
    last_error   TEXT,
    error_count  INTEGER NOT NULL DEFAULT 0,
    restarts     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE audit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    actor      TEXT NOT NULL,
    action     TEXT NOT NULL,
    subject    TEXT,
    detail     TEXT,
    ip         TEXT,
    created_at INTEGER NOT NULL
);
