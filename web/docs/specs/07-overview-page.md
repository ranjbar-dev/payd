# 7. Overview page (`/`)

**Part of:** payd admin dashboard specification v1.0
**ID prefixes in this file:** `WOVW-*`
**Consumes:** `GET /stats`, `GET /chain/status`, `GET /chain/quota`, `GET /workers`, `GET /prices`, `GET /readyz`, and `limit=1` probes for the four alarms
**Related:** backend `OPS-001` (readiness conditions), `OPS-003` (metrics), `API-037`/`API-038`/`API-039`

---

## 7.1 Purpose

Answer two questions in under five seconds: **is the service healthy**, and
**is anything waiting for me**. Everything else on this page is secondary.

It is not a metrics dashboard. Prometheus already scrapes `/metrics` and does
that job better. This page shows the current state of the things that stop
payments being detected or funds being moved.

## 7.2 Layout

```
┌─ Alarms ───────────────────────────────────────────────────────┐
│  [!] needs_operator  0   unattributed  2   funded-terminal  1  │
│      dead IPNs  0                                              │
└────────────────────────────────────────────────────────────────┘
┌─ Readiness ────────────────┐  ┌─ Chain ──────────────────────┐
│  ready | degraded          │  │  height / solidified / lag    │
│  + failing condition list  │  │  reorg suspected              │
└────────────────────────────┘  └───────────────────────────────┘
┌─ Quota ────────────────────┐  ┌─ Prices ─────────────────────┐
│  requests today / cap / %  │  │  per asset, age, stale flag   │
└────────────────────────────┘  └───────────────────────────────┘
┌─ Workers ──────────────────────────────────────────────────────┐
│  worker | last tick | errors | restarts | last error           │
└────────────────────────────────────────────────────────────────┘
┌─ Volume today (UTC) ───────────────────────────────────────────┐
└────────────────────────────────────────────────────────────────┘
```

## 7.3 Alarms

| ID | Requirement |
|---|---|
| WOVW-001 | The alarm strip MUST be the topmost element and MUST show four counts: `needs_operator` withdrawals, unattributed payments, funded terminal orders awaiting resolution, and dead IPN events |
| WOVW-002 | `needs_operator` MUST be rendered at critical severity, visually distinct from the other three. Backend `OPS-006` calls it the one condition that always warrants a human: the money is in an unknown state |
| WOVW-003 | Each counter MUST link to its worklist with the filter pre-applied |
| WOVW-004 | **All five counts MUST come from `GET /stats`** — one request, exact figures, no list fetching (`DAT-009`). The mapping is: `needs_operator` → `needs_operator`; unattributed → `payments["unattributed"]`; orphaned → `orphaned_unresolved`; dead IPNs → the sum of the `ipn_dead` map; funded-terminal → `funded_terminal_unresolved`. The list endpoints expose no total, so a `limit=1` probe can only establish zero versus non-zero and MUST NOT be used for a counter |
| WOVW-004a | `funded_terminal_unresolved` MUST be read as its own field and MUST NOT be derived by adding `orders["expired_funded"]` and `orders["cancelled_funded"]`. That sum keeps counting orders whose `resolution` is already set, so the alarm would never return to zero — and a counter that is permanently lit is one the operator learns to ignore (`ORD-005b`, `WORD-066`, `UI-023`). The backend field applies the `resolution IS NULL` filter; the client MUST NOT attempt to reconstruct it |
| WOVW-005 | A zero count MUST render as a quiet zero, never hidden (`UI-072`) |
| WOVW-006 | Orphaned payments MUST also be counted, folded into the unattributed counter with a breakdown on hover. Backend `CHN-017` produces them and `payd_payments_orphaned_unresolved` alerts on them; they are rarer than unattributed but mean the same thing to the operator — a payment that belongs to nobody |

## 7.4 Readiness

| ID | Requirement |
|---|---|
| WOVW-010 | The readiness card MUST call `/readyz` and render `ready` or `degraded` with each failing condition listed |
| WOVW-011 | `/readyz` requires no API key and MUST NOT be counted against the rate-limit budget, but MUST still be proxied — the browser has no route to payd (`BFF-001`) |
| WOVW-012 | `/readyz` returns `{status, reasons[]}` where each reason is a bare code. Each code MUST render as named human text, and its **figure MUST be composed from the endpoint that owns it** — every one of which the Overview page already polls, so this costs no extra request: `chain_lag` → `/chain/status.lag_blocks` against 20; `solidified_stale` → `/chain/status.solidified_height` and its age; `price_stale` → the oldest `fetched_at` in `/prices`; `trongrid_quota_projection` → `/chain/quota.percent_used` against 90%; `reorg_depth_exceeded` → `/chain/status.reorg_suspected`; `energy_burn_ceiling` → `/chain/params.getEnergyFee` against `/config`'s `energy.max_burn_trx` |
| WOVW-012a | `clock_skew` and `clock_unavailable` MUST render as named text **without a figure**. The magnitude exists only as `payd_clock_skew_seconds` in `/metrics`, and `WSYS-062` forbids parsing that. The text MUST still carry the consequence from backend `OPS-005`/`WDR-010a` — skew causes withdrawals to be rejected or to expire immediately, with a signature indistinguishable from an RPC fault — because that is the whole reason the condition is surfaced |
| WOVW-012b | An unrecognised reason code MUST render as its raw string at warning severity, never be hidden. A new backend readiness condition must be visible before the dashboard is taught about it |
| WOVW-013 | Each degraded condition MUST link to the page that explains it: lag and reorg to Chain, quota to System, prices to Resources, burn ceiling to Resources |
| WOVW-014 | Clock skew MUST be called out explicitly when present, with the note that it causes withdrawal failures indistinguishable from RPC faults (backend `OPS-005`, `WDR-010a`). It is the degraded condition most likely to be misdiagnosed |

## 7.5 Chain

| ID | Requirement |
|---|---|
| WOVW-020 | The chain card MUST render `last_height`, `solidified_height`, `lag_blocks`, `lag_seconds`, `reorg_suspected`, and `last_block_timestamp` from `GET /chain/status` |
| WOVW-021 | `lag_blocks` MUST be shown against the 20-block readiness threshold, so the operator sees proximity rather than a bare number |
| WOVW-022 | `reorg_suspected: true` MUST render at warning severity with a link to the orphaned-payments worklist |
| WOVW-023 | `last_block_timestamp` MUST render as relative age. A frozen follower shows as a growing age before it shows as anything else |

## 7.6 Quota

| ID | Requirement |
|---|---|
| WOVW-030 | The quota card MUST render `requests_today`, `daily_request_quota`, and `percent_used` from `GET /chain/quota` |
| WOVW-031 | It MUST be labelled UTC, since the counter resets at UTC midnight (`UI-010`) |
| WOVW-032 | It MUST render at warning severity above 75% and critical above 90%, the latter matching the readiness threshold |
| WOVW-033 | It MUST link to the 7-day history on the System page. Quota exhaustion is a trend, and the trend is the diagnosis — backend `RES-001a` describes it growing silently for months before detection stops |

## 7.7 Workers

| ID | Requirement |
|---|---|
| WOVW-040 | The workers table MUST render `worker`, `last_tick_at`, `seconds_since_tick`, `last_error`, `error_count`, `restarts` from `GET /workers` |
| WOVW-041 | `seconds_since_tick` MUST be the primary signal, rendered relative and highlighted when it exceeds three times that worker's `expected_interval_seconds` **as returned by the API**. Backend `OPS-008`: a stalled worker is otherwise indistinguishable from an idle one |
| WOVW-041a | The client MUST NOT hold its own table of worker intervals. Cadences span three orders of magnitude — the IPN dispatcher ticks every second, the chain-parameter worker every six hours — and two of them (`follower`, `price`) are configurable at runtime, so a hardcoded copy is wrong the moment config changes and wrong silently (`INV-5`) |
| WOVW-041b | A null `expected_interval_seconds` MUST render the tick age with no stall verdict, rather than assuming a default. An unknown cadence is unknown, and a guessed threshold produces either a false alarm or a missed one |
| WOVW-042 | `last_error` is **sticky** — the backend deliberately does not clear it on the next success. The UI MUST render it as "last error (may be resolved)" alongside a fresh `last_tick_at`, so a recovered fault is not read as a live one |
| WOVW-043 | A worker with a null `last_tick_at` MUST render as "never ticked" at warning severity, not as "—". It means the worker has not run since deployment |
| WOVW-044 | A stalled Confirmation Tracker MUST be called out by name when detected, because its failure mode is silent: payments stay `seen`, orders never reach `confirmed`, and nothing else reports a fault (backend `OPS-008`) |

## 7.8 Prices and volume

| ID | Requirement |
|---|---|
| WOVW-050 | The prices card MUST render each asset's cached price and its age from `GET /prices` |
| WOVW-051 | A price older than `/config`'s `price.stale_after_seconds` MUST render at warning severity with the consequence stated: order creation returns 503 (backend `ORD-009`) and withdrawal creation returns 503 (`WDR-006b`). Stale prices stop the business, and the operator should learn that here rather than from a consumer's error report |
| WOVW-052 | The volume card MUST render today's figures from `GET /reports/volume?from=<UTC midnight>&to=<now>&group_by=day`, labelled UTC. **Not from `/stats`** — that endpoint reports all-time counts grouped by status and has no per-day dimension, so it cannot answer "today". The volume report already returns order count, paid-or-confirmed count, received volume per asset, the exact snapshotted USD total, and `unpriced_paid_count` |
| WOVW-052a | `unpriced_paid_count` MUST be shown on the card whenever non-zero, not only in the full report. The USD total is exact but incomplete by exactly that many orders (backend `API-044`), and a figure presented as complete when it is not is worse than no figure |
| WOVW-053 | `/stats` returns the shared operational metrics model with open-ended fields. The UI MUST render a known subset and MUST NOT break on unknown keys — new metrics MUST be ignored, not crash the page |
| WOVW-054 | The volume card MUST link to the full volume report ([`14`](14-reports-and-exports.md)) rather than growing chart controls here |

## 7.9 Polling

| ID | Requirement |
|---|---|
| WOVW-060 | Overview queries are tier B (30s): `/stats`, `/chain/status`, `/chain/quota`, `/workers`, `/readyz`, `/prices`, `/chain/params`, and today's `/reports/volume` — 16 req/min total. `/config` is fetched once per session and cached, not polled: its allowlisted thresholds change only on restart |
| WOVW-061 | The nav alarm counters are tier C (60s) and MUST NOT be duplicated by the alarm strip. The strip MUST read the same cached queries |
| WOVW-062 | The page MUST NOT poll anything at tier A. Nothing here changes in 5 seconds that matters in 5 seconds |
