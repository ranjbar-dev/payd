# 3. Architecture

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §3
**ID prefixes in this file:** `W-0xx` (worker table), `ARC-*` (worker lifecycle requirements)
**Related:** every other spec file — this is the structural map. [`19-implementation-phases.md`](19-implementation-phases.md) builds these packages in order.

---

One process, one SQLite file, ten supervised workers communicating over buffered channels. Each worker owns one responsibility and one set of tables.

```
cmd/
  payd/          — the daemon
  seedtool/      — one-shot mnemonic encryptor (see key-management spec)
internal/
  chain/         — TronGrid client, failover, rate budget, chain parameters
  follower/      — block polling, gap detection, reorg detection, ingest + matching
  decode/        — TRX + TRC-20 transaction/log decoding
  matcher/       — payment attribution to orders (in-process stage of follower)
  confirm/       — solidified-height tracking, seen → confirmed promotion
  lifecycle/     — clock-driven transitions: expiry, cooldown return, pool top-up
  ipn/           — outbox dispatcher, HMAC signing, retry
  price/         — Binance poller
  wallet/        — address pool, balances, resource monitoring
  withdraw/      — request queue, signing, broadcast, on-chain resolution
  api/           — HTTP handlers, auth middleware
  store/         — SQLite access layer, migrations
  config/        — config loading and validation
```

## 3.1 Workers

| ID | Worker | Cadence | Owns |
|---|---|---|---|
| W-001 | Chain Follower (includes the Ingest/Matcher stage) | 3s tick | `blocks`, crawler cursor, `payments`, `orders` |
| W-003 | Confirmation Tracker | 20s tick | payment status transitions |
| W-004 | IPN Dispatcher | 1s tick + signal | `ipn_outbox` |
| W-005 | Price Poller | 60s tick | `prices` |
| W-006 | Wallet Monitor | tiered: 5m / 6h | `addresses` resource fields |
| W-007 | Reconciler | 5m tick (active), 6h (full) | balance verification |
| W-008 | Withdrawal Engine | 2s tick + signal | `withdrawals` |
| W-009 | HTTP API | request-driven | none (reads/writes via store) |
| W-010 | **Lifecycle Worker** | 10s tick / 60s pool check | clock-driven transitions on `orders` and `addresses` |
| W-011 | **Chain Parameter Poller** | startup + 6h | `chain_params` |

> **W-002 is gone.** The Ingest/Matcher is now an in-process stage of W-001, not a separate worker. This resolves the contradiction in v1.1 where CHN-006 required the cursor and the block's payments to advance in one transaction while §3.1 assigned them to two workers communicating over a channel — one SQLite transaction cannot span a channel boundary. "Worker" in this table denotes a supervised goroutine; `internal/matcher` remains a separate **package** so it stays independently testable.

## 3.2 Worker lifecycle requirements

| ID | Requirement |
|---|---|
| ARC-001 | Every worker MUST accept a `context.Context` and shut down cleanly on cancellation |
| ARC-002 | A panicking worker MUST be logged, recorded in a `worker_health` table, and restarted with exponential backoff (1s → 60s cap). A restarted Withdrawal Engine MUST run the §13.5 startup resolution before processing any new work |
| ARC-003 | Workers MUST NOT call each other directly; communication is via channels or the store |
| ARC-004 | The process MUST NOT exit because a single worker fails, except for the Chain Follower failing to start |
| ARC-005 | All writes MUST go through `internal/store`; no worker opens its own database handle |
| ARC-006 | SQLite MUST be opened with `_journal_mode=WAL`, `_busy_timeout=5000`, `_foreign_keys=on`, `_synchronous=NORMAL` |
| ARC-006a | Writes that record an **irreversible external side effect** MUST be committed on a dedicated store connection opened with `_synchronous=FULL`. This applies to exactly two writes: the WDR-015 txid persist, and the `signing → broadcast` transition. All other work MAY use the `NORMAL` connection. Rationale: `NORMAL` in WAL mode does not fsync on commit; SQLite permits loss of recently committed transactions on OS crash or power loss. For ingest that is harmless (the block is simply reprocessed), but a lost "we broadcast this" commit against an irreversible on-chain transfer is a double-spend window |
| ARC-007 | **No network I/O may occur while a SQLite write transaction is held.** All RPC for a unit of work MUST complete before the write transaction opens. A 10s request timeout inside a write transaction would hold the single writer far past the 5,000ms `_busy_timeout`, failing every concurrent API write with `SQLITE_BUSY` |
