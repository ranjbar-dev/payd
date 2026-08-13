# 16. TronGrid rate-limit budget

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §16
**ID prefixes in this file:** `RL-*`
**Related:** [`06-chain-follower.md`](06-chain-follower.md) (CHN-023 soft cap derives from RL-001), [`12-resource-management.md`](12-resource-management.md) (RES-001a tiering keeps the resource-poll line bounded), [`17-operations.md`](17-operations.md) (OPS-003 exposes the counters)

---

Free tier: 15 QPS with an API key, 100,000 requests/day per account. Extra API keys share the account quota rather than multiplying it. Without a key, TronGrid throttles dynamically and applies a 30-second block with HTTP 403.

| Component | Cadence | Calls/day |
|---|---|---|
| Block crawl (`getnowblock`) | 3s | 28,800 |
| **Gap fills (`getblockbynum`)** | steady-state, ~15% of ticks skip | **~4,300** |
| Receipt fetch on hit (`gettransactioninfobyblocknum`) | per hit block | ~100 |
| Solidified head (`walletsolidity/getnowblock`) | 20s | 4,320 |
| Resource poll — fast tier (`getaccountresource`, ≤50 addresses) | 5m | ≤14,400 |
| Resource poll — slow tier (all other funded addresses) | 6h | ~1,000 |
| TRC-20 safety-net reconcile (active orders, both directions) | 5m | ~5,800 |
| **Native TRX safety-net reconcile** (both directions) | 5m | **~5,800** |
| **6-hourly full sweep (`assigned` + `cooling`, both endpoints)** | 6h | **~2,000** |
| **Energy delegation polling (`getaccountresource`, ≤45/withdrawal)** | per withdrawal | **~900** |
| Withdrawal broadcast + tracking | per withdrawal | ~200 |
| Chain parameters (`getchainparameters`) | 6h | 4 |
| **Total** | | **~67,600** |

> v1.1's table totalled ~42,000 and omitted gap fills, energy polling, the 6-hourly sweep, and the entire native-TRX reconcile path. It also assumed a fixed address count while RES-001 polled a set that grows monotonically. The figures above assume RES-001a's tiering is in force; without it the resource-poll line alone grows without bound.

| ID | Requirement |
|---|---|
| RL-001 | Steady-state usage MUST stay under **70%** of the daily quota, leaving headroom for catch-up after downtime. CHN-023's soft cap MUST be derived from this figure rather than stated independently |
| RL-002 | Balances MUST NEVER be polled on a loop. The crawler observes every inbound **and outbound** transfer (DET-002b), so `confirmed_raw` and `pending_raw` are maintained from observed events |
| RL-003 | Full on-chain balance verification MUST run at most every 6 hours, writing `chain_raw` and setting `drift_detected` if the ledger and chain disagree. `drift_detected` MUST be consumed by BAL-002 |
| RL-004 | The client MUST expose a daily request counter at `/metrics` |
| RL-005 | Catch-up mode MUST be capped at 8 requests/second to stay well below the 15 QPS ceiling |
| RL-006 | The daily request counter MUST be **projected forward**: when the 7-day trend crosses 60% of the configured cap, the service MUST emit a warning event and expose `payd_trongrid_quota_projection_ratio`. Crossing 90% MUST degrade `/readyz`. A budget that is comfortable on day one and silently crosses the quota months later fails as a complete detection outage, with missed customer payments as the first symptom |
