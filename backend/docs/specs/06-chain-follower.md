# 6. Chain follower

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §6
**ID prefixes in this file:** `CHN-*`
**Related:** [`07-payment-detection.md`](07-payment-detection.md) (decoding runs inside the same worker), [`09-confirmation-tracking.md`](09-confirmation-tracking.md) (consumes `blocks`/`crawler_state`), [`03-architecture-and-workers.md`](03-architecture-and-workers.md) (W-001, ARC-006a/ARC-007)

---

## 6.1 Polling and gap detection

| ID | Requirement |
|---|---|
| CHN-001 | The follower MUST poll `POST /wallet/getnowblock` every 3 seconds |
| CHN-002 | `getnowblock` returns the full block including its transaction list; the follower MUST NOT make a second call to fetch the same block's transactions |
| CHN-003 | After each poll, the follower MUST compare the returned height to `crawler_state.last_height`. If the gap is greater than 1, it MUST fetch each missing block via `POST /wallet/getblockbynum` before processing the new tip |
| CHN-004 | If the gap exceeds 100 blocks (e.g. after downtime), the follower MUST enter catch-up mode: fetch sequentially at up to 8 requests/second until caught up, then resume the 3s tick |
| CHN-005 | Blocks MUST be processed strictly in ascending height order |
| CHN-006 | The crawler cursor MUST be advanced in the same transaction that writes the block's payments, so a crash cannot skip a block. This is now achievable because the matcher is an in-process stage of the follower (§3.1), not a separate worker |
| CHN-006a | All RPC for a block — the block fetch and any Tier 2 receipt fetch — MUST complete **before** the write transaction opens (ARC-007) |
| CHN-007 | A 3s poll against 3s blocks drifts; some ticks return the same block and some skip one. Returning an already-processed height MUST be a no-op, not an error |
| CHN-007a | A poll returning a height **lower** than `crawler_state.last_height` MUST be discarded as a stale read from a lagging backend and MUST NOT trigger reorg handling. `api.trongrid.io` is itself load-balanced across many nodes; consecutive requests routinely land on backends at different heights. Height regressions MUST be counted in `payd_trongrid_stale_reads_total` |

Expected detection latency: 1.5–4.5 seconds from block production.

## 6.2 Reorg detection

| ID | Requirement |
|---|---|
| CHN-010 | Every processed block's `block_id` and `parent_id` MUST be stored; the last `reorg_depth` (64) entries retained |
| CHN-011 | Before processing block N, the follower MUST verify that N's `parent_id` equals the stored `block_id` of N-1. On mismatch, a reorg is **suspected** |
| CHN-011a | A parent mismatch MUST be **confirmed before reorg handling begins**: the follower MUST re-fetch block N-1 by number and observe the same divergence **twice, at least one poll interval apart**. A single mismatch is more often a read from a lagging or briefly diverged backend than a real reorg of the canonical chain. Unconfirmed mismatches MUST be counted in `payd_reorg_suspected_total` and MUST NOT invoke CHN-012 |
| CHN-012 | On a **confirmed** reorg, the follower MUST walk backwards until it finds a block whose stored `block_id` matches the chain, then re-process forward from there |
| CHN-013 | Payments in orphaned blocks MUST transition to `orphaned`, and their orders MUST recalculate `received_raw` from non-orphaned payments only |
| CHN-014 | An order that drops from `paid` back to `partial` or `pending` because of a reorg MUST emit an `order.reverted` IPN |
| CHN-015 | If a reorg is deeper than `reorg_depth`, the service MUST log a critical error, mark itself unhealthy on `/readyz`, and halt ingest rather than guess |
| CHN-016 | **Payment ingest MUST use an upsert, not an insert-or-ignore.** On Tron a reorg'd transaction is almost always re-included in the replacement block — that is the normal outcome, not the exception. The statement MUST be: `INSERT … ON CONFLICT (txid, log_index) DO UPDATE SET block_height = excluded.block_height, block_id = excluded.block_id, block_timestamp = excluded.block_timestamp, status = CASE WHEN payments.status = 'orphaned' THEN 'seen' ELSE payments.status END, detected_at = COALESCE(payments.detected_at, excluded.detected_at)`. Any row whose status changes from `orphaned` back to `seen` MUST re-enter the matcher **in the same transaction**, and its order MUST recalculate. `ON CONFLICT DO NOTHING` is explicitly forbidden here: it would leave a re-included payment permanently `orphaned` and the customer permanently uncredited, and the DET-010 safety net cannot recover it because the row already exists |
| CHN-017 | A payment that has been `orphaned` for more than `reorg_depth` blocks below the current tip **without re-inclusion** MUST be surfaced at `GET /api/v1/payments/orphaned`. This is the genuinely-vanished case — a customer whose transfer disappeared — and it MUST be distinguishable from the ordinary re-inclusion path |

Reorgs deeper than a few blocks are essentially unheard of on Tron, but CHN-015 makes the failure loud rather than silent.

## 6.3 Endpoint management

| ID | Requirement |
|---|---|
| CHN-020 | The client MUST send the `TRON-PRO-API-KEY` header on every request when a key is configured |
| CHN-021 | On HTTP 429 or 403, the client MUST back off exponentially (1s → 30s) and fail over to the next configured endpoint |
| CHN-022 | A circuit breaker MUST open after 5 consecutive failures on one endpoint and retry it after 60s |
| CHN-023 | The client MUST track requests-per-day (UTC, per DB-002a) against a configurable soft cap and emit a warning at 80%. The soft cap MUST be derived from RL-001's 50% steady-state target rather than stated independently — v1.1 specified 50% in RL-001 and a 90,000-of-100,000 soft cap in CHN-023, which are different thresholds for the same control |
| CHN-024 | All **read** requests MUST have a 10s timeout and MAY be retried at most twice on network error. This applies to `getnowblock`, `getblockbynum`, `gettransactioninfobyblocknum`, `gettransactionbyid`, `getaccountresource`, `getchainparameters`, and the account-history endpoints — operations with no side effect, where a repeat is free |
| CHN-024a | **`/wallet/broadcasthex` MUST be exempt from CHN-024 entirely.** It MUST have no automatic retry under any circumstance. A broadcast has an irreversible external side effect and a lost response does not mean a lost transaction. See [`13-withdrawal-engine.md`](13-withdrawal-engine.md) §13.0 and WDR-017 |
| CHN-025 | Configured endpoints MUST be **distinct hosts**. Startup MUST reject a configuration in which two enabled endpoints share a hostname. Same-host "failover" provides no protection: TronGrid's quota is per account, and the keyless path is throttled per source IP from the same server — so failing over on a 429 fails over to the same rate limiter from the same machine |
| CHN-026 | `solidity_url` SHOULD also be an independent host where available, and its readings MUST be applied monotonically per CNF-002a |
