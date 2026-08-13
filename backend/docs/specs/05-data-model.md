# 5. Data model

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §5
**ID prefixes in this file:** `DB-*`, `BAL-*`
**Related:** every functional spec references these tables. [`13-withdrawal-engine.md`](13-withdrawal-engine.md) and [`08-order-lifecycle-and-address-pool.md`](08-order-lifecycle-and-address-pool.md) are the heaviest consumers.

---

```sql
-- Chain state -----------------------------------------------------------

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
    solidified_height  INTEGER NOT NULL,   -- written monotonically; see CNF-002a
    updated_at         INTEGER NOT NULL
);

-- Governance-controlled chain parameters, refreshed from the chain (ENR-016)
CREATE TABLE chain_params (
    name        TEXT PRIMARY KEY,          -- getEnergyFee | getTransactionFee | ...
    value       INTEGER NOT NULL,          -- sun
    fetched_at  INTEGER NOT NULL
);

-- Wallet ----------------------------------------------------------------

CREATE TABLE addresses (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    hd_index            INTEGER NOT NULL UNIQUE,
    address             TEXT NOT NULL UNIQUE,
    state               TEXT NOT NULL DEFAULT 'free',
        -- free | assigned | cooling | disabled
    assigned_order_id   TEXT REFERENCES orders(id),
    assigned_at         INTEGER,           -- set by POOL-001, cleared by POOL-005
    released_at         INTEGER,           -- set by POOL-004; closes the assignment window
    cooling_until       INTEGER,
    is_activated        INTEGER NOT NULL DEFAULT 0,
    energy_limit        INTEGER NOT NULL DEFAULT 0,
    energy_used         INTEGER NOT NULL DEFAULT 0,
    bandwidth_limit     INTEGER NOT NULL DEFAULT 0,
    bandwidth_used      INTEGER NOT NULL DEFAULT 0,
    needs_resources     INTEGER NOT NULL DEFAULT 0,
    resources_checked_at INTEGER,
    created_at          INTEGER NOT NULL
);
CREATE INDEX idx_addresses_state ON addresses(state);
CREATE INDEX idx_addresses_needs_resources ON addresses(needs_resources);

-- Three explicit balance columns, each with a single named writer (BAL-001).
-- v1.1's amount_raw / ledger_amount pair had no defined owner or consumer and
-- could not represent a confirmed balance at all.
CREATE TABLE balances (
    address_id       INTEGER NOT NULL REFERENCES addresses(id),
    asset            TEXT NOT NULL,
    confirmed_raw    TEXT NOT NULL DEFAULT '0',  -- confirmed credits minus confirmed debits.
                                                 -- THE ONLY SPENDABLE NUMBER.
    pending_raw      TEXT NOT NULL DEFAULT '0',  -- status='seen' credits. Visible, never spendable.
    chain_raw        TEXT,                       -- last direct on-chain reading; RL-003 only
    last_verified_at INTEGER,
    drift_detected   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (address_id, asset)
);

-- Orders and payments ---------------------------------------------------

CREATE TABLE orders (
    id                 TEXT PRIMARY KEY,          -- ULID
    external_ref       TEXT,
    address_id         INTEGER NOT NULL REFERENCES addresses(id),
    address            TEXT NOT NULL,
    asset              TEXT NOT NULL,
    expected_raw       TEXT NOT NULL,
    received_raw       TEXT NOT NULL DEFAULT '0',
    overpaid_raw       TEXT NOT NULL DEFAULT '0',
    status             TEXT NOT NULL DEFAULT 'pending',
        -- pending | partial | paid | confirmed
        -- | expired | expired_funded | cancelled | cancelled_funded
    resolution         TEXT,                      -- NULL | refunded | written_off | reattributed
    resolution_note    TEXT,
    resolved_at        INTEGER,
    price_usd          TEXT,
    price_at           INTEGER,
    metadata           TEXT,                      -- opaque JSON, echoed in IPNs
    consumer           TEXT,                      -- NULL = ipn.default_consumer
    expires_at         INTEGER NOT NULL,
    created_at          INTEGER NOT NULL,
    updated_at         INTEGER NOT NULL
);
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_orders_address ON orders(address);
CREATE INDEX idx_orders_expires ON orders(expires_at) WHERE status IN ('pending','partial');
CREATE UNIQUE INDEX idx_orders_external_ref ON orders(external_ref) WHERE external_ref IS NOT NULL;
CREATE INDEX idx_orders_funded_terminal ON orders(status)
    WHERE status IN ('expired_funded','cancelled_funded') AND resolution IS NULL;

CREATE TABLE payments (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    txid           TEXT NOT NULL,
    log_index      INTEGER NOT NULL DEFAULT 0,   -- canonical definition in DET-002a
    direction      TEXT NOT NULL DEFAULT 'in'
                   CHECK (direction IN ('in','out')),
    block_height   INTEGER NOT NULL,
    block_id       TEXT NOT NULL,
    block_timestamp INTEGER NOT NULL,            -- chain time; used for attribution
    from_address   TEXT NOT NULL,
    to_address     TEXT NOT NULL,
    address_id     INTEGER REFERENCES addresses(id),  -- destination for 'in', source for 'out'
    order_id       TEXT REFERENCES orders(id),
    asset          TEXT NOT NULL,
    amount_raw     TEXT NOT NULL,
    is_dust        INTEGER NOT NULL DEFAULT 0,   -- below asset.min_deposit (DET-007)
    status         TEXT NOT NULL DEFAULT 'seen',
        -- seen | confirmed | orphaned | unattributed
    detected_at    INTEGER NOT NULL,             -- service-local; observability ONLY,
                                                 -- MUST NOT participate in attribution
    confirmed_at   INTEGER,
    UNIQUE (txid, log_index)
);
CREATE INDEX idx_payments_status ON payments(status);
CREATE INDEX idx_payments_order ON payments(order_id);
CREATE INDEX idx_payments_height ON payments(block_height);
CREATE INDEX idx_payments_addr_asset ON payments(address_id, asset, direction, status);
CREATE INDEX idx_payments_orphaned ON payments(status, block_height) WHERE status = 'orphaned';

-- Notifications ---------------------------------------------------------

CREATE TABLE ipn_outbox (
    id             TEXT PRIMARY KEY,              -- ULID, used as X-Event-Id
    order_id       TEXT REFERENCES orders(id),    -- NULL for global events
    sequence_key   TEXT NOT NULL,                 -- NEVER NULL; see IPN-011
        -- 'order:'||order_id | 'withdrawal:'||withdrawal_id
        -- | 'energy:'||provider | 'address:'||address_id | 'global'
    consumer       TEXT NOT NULL,
    target_url     TEXT NOT NULL,                 -- snapshotted at enqueue time
    event_type     TEXT NOT NULL,
    payload        TEXT NOT NULL,                 -- immutable snapshot (IPN-021a)
    status         TEXT NOT NULL DEFAULT 'pending',
        -- pending | delivered | failed | dead
    attempts       INTEGER NOT NULL DEFAULT 0,
    next_attempt_at INTEGER NOT NULL,
    last_error     TEXT,
    last_status_code INTEGER,
    created_at     INTEGER NOT NULL,
    delivered_at   INTEGER
);
CREATE INDEX idx_ipn_pending ON ipn_outbox(next_attempt_at) WHERE status = 'pending';
CREATE INDEX idx_ipn_sequence ON ipn_outbox(sequence_key, consumer, created_at);

-- Withdrawals -----------------------------------------------------------

CREATE TABLE withdrawals (
    id               TEXT PRIMARY KEY,            -- ULID
    idempotency_key  TEXT NOT NULL UNIQUE,
    address_id       INTEGER NOT NULL REFERENCES addresses(id),
    from_address     TEXT NOT NULL,
    to_address       TEXT NOT NULL,
    asset            TEXT NOT NULL,
    amount_raw       TEXT NOT NULL,
    amount_usd       TEXT,
    status           TEXT NOT NULL DEFAULT 'requested',
        -- rejected                    : refused before any on-chain action (WDR-002b)
        -- requested → awaiting_resources → awaiting_energy → signing
        --   → broadcast → confirmed
        -- failed                      : attempted, resolved absent from chain (WDR-022a)
        -- needs_operator              : outcome unresolvable automatically (WDR-018)
    txid             TEXT,                        -- persisted BEFORE broadcast (WDR-015)
    broadcast_attempted_at INTEGER,               -- persisted with txid, same commit
    broadcast_response TEXT,                      -- raw node response, for audit
    fee_raw          TEXT,
    energy_used      INTEGER,
    energy_source    TEXT,                        -- existing | rented | self_delegated | burned
    energy_cost_trx  TEXT,
    bandwidth_source TEXT,                        -- free | topup | delegated | burned
    failure_reason   TEXT,
    resolved_by      TEXT,                        -- chain_lookup | expiration | operator
    requested_by     TEXT NOT NULL,
    created_at       INTEGER NOT NULL,
    broadcast_at     INTEGER,
    confirmed_at     INTEGER
);
CREATE INDEX idx_withdrawals_status ON withdrawals(status);
CREATE INDEX idx_withdrawals_created ON withdrawals(created_at);
CREATE INDEX idx_withdrawals_txid ON withdrawals(txid) WHERE txid IS NOT NULL;
-- Supports WDR-006's daily cap and WDR-007's per-address exclusivity check.
CREATE INDEX idx_withdrawals_addr_active ON withdrawals(address_id, status);

-- Single-use TOTP state must survive a restart, or the replay window reopens.
CREATE TABLE used_totp (
    code       TEXT NOT NULL,
    step       INTEGER NOT NULL,          -- 30s step number the code was valid for
    used_at    INTEGER NOT NULL,
    PRIMARY KEY (code, step)
);
CREATE INDEX idx_used_totp_used_at ON used_totp(used_at);

-- Resource delegation ---------------------------------------------------

CREATE TABLE resource_grants (
    id             TEXT PRIMARY KEY,
    address_id     INTEGER NOT NULL REFERENCES addresses(id),
    resource_type  TEXT NOT NULL,                 -- ENERGY | BANDWIDTH
    source         TEXT NOT NULL,                 -- rented | self_delegated | topup
    amount_sun     TEXT NOT NULL,
    txid           TEXT,
    status         TEXT NOT NULL DEFAULT 'requested',
    created_at     INTEGER NOT NULL,
    confirmed_at   INTEGER
);

CREATE TABLE energy_purchases (
    id                TEXT PRIMARY KEY,           -- ULID
    provider          TEXT NOT NULL,
    provider_order_id TEXT,
    withdrawal_id     TEXT REFERENCES withdrawals(id),
    address_id        INTEGER NOT NULL REFERENCES addresses(id),
    receiver_address  TEXT NOT NULL,
    resource_type     TEXT NOT NULL DEFAULT 'ENERGY',  -- ENERGY | BANDWIDTH
    energy_amount     INTEGER NOT NULL,
    duration_seconds  INTEGER NOT NULL,
    quoted_trx        TEXT NOT NULL,
    actual_trx        TEXT,
    status            TEXT NOT NULL DEFAULT 'quoted',
        -- quoted | purchased | delegated | expired | failed
    delegation_txid   TEXT,
    failure_reason    TEXT,
    created_at        INTEGER NOT NULL,
    delegated_at      INTEGER
);
CREATE INDEX idx_energy_purchases_status ON energy_purchases(status);
CREATE INDEX idx_energy_purchases_withdrawal ON energy_purchases(withdrawal_id);

CREATE TABLE energy_provider_state (
    provider        TEXT PRIMARY KEY,
    balance_trx     TEXT NOT NULL DEFAULT '0',
    last_checked_at INTEGER,
    last_error      TEXT,
    consecutive_failures INTEGER NOT NULL DEFAULT 0
);

-- Prices ----------------------------------------------------------------

CREATE TABLE prices (
    symbol      TEXT PRIMARY KEY,
    price_usd   TEXT NOT NULL,
    source      TEXT NOT NULL,
    fetched_at  INTEGER NOT NULL
);

-- Operational -----------------------------------------------------------

CREATE TABLE worker_health (
    worker        TEXT PRIMARY KEY,
    last_tick_at  INTEGER,
    last_error    TEXT,
    error_count   INTEGER NOT NULL DEFAULT 0,
    restarts      INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE audit_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    actor       TEXT NOT NULL,
    action      TEXT NOT NULL,
    subject     TEXT,
    detail      TEXT,
    ip          TEXT,
    created_at  INTEGER NOT NULL
);
```

| ID | Requirement |
|---|---|
| DB-001 | All monetary amounts MUST be stored as decimal strings representing base units (`big.Int`), never floats or integers |
| DB-002 | All timestamps MUST be Unix seconds (integer), UTC |
| DB-002a | **All date-boundary logic MUST use UTC midnight** — the withdrawal daily limit, the TronGrid requests-per-day counter, and every bandwidth-reset assumption. TRON's own free-bandwidth allowance resets at 00:00 UTC, and the withdrawal path depends on it (RES-006); a local-time boundary would put the cap hours out of step with the resource it interacts with |
| DB-003 | Order IDs, withdrawal IDs, and IPN event IDs MUST be ULIDs (sortable, collision-resistant) |
| DB-004 | `payments` MUST be unique on `(txid, log_index)` — this is the idempotency guarantee for the whole ingest pipeline. It holds **only** because DET-002a defines `log_index` canonically across all three ingest paths; without that definition two paths can write the same economic event under different keys |
| DB-005 | Schema migrations MUST be embedded via `go:embed` and applied automatically at startup, tracked in a `schema_migrations` table |
| DB-006 | The database file MUST be created mode `0600` |
| DB-007 | `used_totp` rows older than 5 minutes MUST be pruned by the Lifecycle Worker |
| BAL-001 | `confirmed_raw` and `pending_raw` MUST be recomputed from `payments` **inside the same transaction that changes any payment's status or inserts any payment row**. `chain_raw` MUST be written only by the 6-hour reconcile (RL-003). No other writer for any of the three |
| BAL-002 | A withdrawal from an address with `drift_detected = 1` MUST be rejected with HTTP 409 `balance_drift` until an operator clears the flag via `POST /wallets/{address}/clear-drift`, which MUST require a single-use `X-TOTP` and be audit-logged. The request MUST name one asset and echo its current `chain_raw`; the store MUST compare that value and clear only that asset in the same write transaction, returning 409 on a stale or absent acknowledgement. Detected drift MUST also emit a `balance.drift_detected` global IPN event. A flag nothing reads is not a control |
