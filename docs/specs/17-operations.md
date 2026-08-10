# 17. Operations

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §17
**ID prefixes in this file:** `OPS-*`
**Related:** [`16-rate-limit-budget.md`](16-rate-limit-budget.md) (metrics feed readiness), [`13-withdrawal-engine.md`](13-withdrawal-engine.md) (OPS-006 alerts on `needs_operator`), [`14-key-management.md`](14-key-management.md) (OPS-012/014 recovery)

---

## 17.1 Health

| ID | Requirement |
|---|---|
| OPS-001 | `/readyz` MUST return 503 when any of: chain lag exceeds 20 blocks; solidified height has not advanced in 5 minutes; prices are older than `price.stale_after`; the database is unwritable; a reorg deeper than `reorg_depth` was detected; clock skew exceeds 30s (OPS-005); quota projection crosses 90% (RL-006); `energy.max_burn_trx` would refuse a worst-case transfer (ENR-017) |
| OPS-002 | `/healthz` MUST return 200 whenever the process is running, regardless of worker state |
| OPS-003 | Prometheus metrics MUST include: `payd_chain_lag_blocks`, `payd_trongrid_requests_total`, `payd_trongrid_errors_total{code}`, `payd_trongrid_stale_reads_total`, `payd_trongrid_quota_projection_ratio`, `payd_reorg_suspected_total`, `payd_reorg_confirmed_total`, `payd_orders_total{status}`, `payd_payments_total{status}`, `payd_payments_orphaned_unresolved`, `payd_ipn_attempts_total{consumer,outcome}`, `payd_ipn_dead_total{consumer}`, `payd_ipn_queue_depth{consumer}`, `payd_price_age_seconds`, `payd_clock_skew_seconds`, `payd_withdrawals_total{status}`, `payd_withdrawals_needs_operator`, `payd_addresses_needing_resources`, `payd_addresses_with_balance`, `payd_balance_drift_addresses`, `payd_energy_purchases_total{outcome}`, `payd_energy_cost_trx_total{source}`, `payd_energy_fee_sun`, `payd_energy_provider_balance_trx` |
| OPS-004 | `payd_energy_cost_trx_total{source}` MUST make the burn-versus-rent split visible, so a silently failing provider shows up as rising burn cost rather than an unnoticed expense |
| OPS-005 | **At startup and every 5 minutes, the service MUST compare the local clock to the latest block header timestamp.** A divergence exceeding 30 seconds MUST be logged as an error, exported as `payd_clock_skew_seconds`, and MUST fail `/readyz`. Undetected skew produces a total withdrawal outage whose signature is indistinguishable from an RPC fault (WDR-010a) |
| OPS-006 | `payd_withdrawals_needs_operator` MUST alert. A withdrawal in `needs_operator` is money in an unknown state and is the one condition that always warrants a human |
| OPS-007 | `payd_addresses_with_balance` MUST be tracked over time, since it is the growth driver behind RES-001a and RL-006 |
| OPS-008 | **Every worker MUST write one `worker_health` row per tick** — `last_tick_at` on every tick, `last_error` and an incremented `error_count` on a failing one — and that write is the table's only writer. `last_tick_at` is what makes a wedged loop detectable: a worker that stops ticking is otherwise indistinguishable from an idle one, and a stalled Confirmation Tracker leaves payments in `seen` and orders never reaching `confirmed` with nothing anywhere reporting a fault. `last_error` MUST be sticky rather than cleared on the next success, so a fault that has already recovered is still visible to the operator who arrives afterwards; `last_tick_at` freshness alongside a flat `error_count` is what distinguishes "failing now" from "failed once". A cancelled tick is shutdown, not a fault, and MUST NOT increment `error_count`. v1.2 defined the table and the `GET /workers` endpoint over it but named no writer, so the endpoint returned an empty list on every deployment |

## 17.2 Transport security

| ID | Requirement |
|---|---|
| OPS-009 | `payd` MUST NOT be exposed directly for remote access because it serves plain HTTP and API keys and TOTP codes travel in headers. Remote clients MUST connect through a TLS-terminating reverse proxy; set `server.trusted_proxy: true` only after that proxy is in place (CFG-016). Loopback-only deployments MUST leave it `false` |

## 17.3 Backup and recovery

| ID | Requirement |
|---|---|
| OPS-010 | The service MUST support `sqlite3 .backup` while running (WAL mode permits this) |
| OPS-011 | A documented recovery procedure MUST exist: restore the database, restart, and let the follower catch up from `crawler_state.last_height`. **The Withdrawal Engine's §13.5 startup resolution MUST run before it processes any new work**, since a restored database may be behind the chain on in-flight withdrawals |
| OPS-012 | Total database loss MUST be recoverable from the mnemonic alone, by re-deriving addresses and replaying **both** `/v1/accounts/{address}/transactions/trc20` **and** `/v1/accounts/{address}/transactions` history, in both directions, with full cursor iteration — though order attribution and metadata would be lost. v1.1 named the TRC-20 endpoint only, which reconstructs none of the native TRX ledger. This MUST be documented explicitly |
| OPS-013 | Logs MUST NOT contain the mnemonic, private keys, API keys, TOTP codes, or the IPN secret |
| OPS-014 | The recovery documentation MUST state that withdrawals in `broadcast`, `signing`, or `needs_operator` at the time of loss MUST be reconciled manually against Tronscan before any new withdrawal is issued from the affected addresses |
