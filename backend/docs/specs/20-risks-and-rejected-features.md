# 20-22. Open risks, considered-and-rejected, deferred

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §20 (Open risks), §21 (Considered and rejected), §22 (Deferred to a later version)
**ID prefixes in this file:** `R-*`
**Related:** [`01-overview-and-goals.md`](01-overview-and-goals.md) (NG-003/NG-007 non-goals mirror the rejections here), [`13-withdrawal-engine.md`](13-withdrawal-engine.md) (R-013 no-retry trade-off), [`12-resource-management.md`](12-resource-management.md) (R-014 address-pool growth)

---

## 20. Open risks

| ID | Risk | Mitigation |
|---|---|---|
| R-001 | TronGrid free tier changes its quota or pricing | Endpoint list is configurable and now requires genuinely distinct hosts (CHN-025); a paid provider drops in without code changes |
| R-002 | Tier 1 calldata screening misses contract-internal transfers | 5-minute safety-net reconcile across both assets and both directions (DET-010); logged as a warning when it fires |
| R-003 | Address reuse exposes payment history to customers on Tronscan | Accepted by the operator. Reversible by switching to fresh-address-per-order — the pool code supports it by setting `cooldown` to infinity |
| R-004 | `payd` holds the seed and signs inline, so any application-level auth bypass in the API layer is a fund-loss event | `withdrawal.daily_limit_usd` now counts every non-terminal state under a UTC boundary and is enforced in one transaction (WDR-006/006a), so it is an actual cap rather than an advisory one; audit logging records every attempt; API-022's persisted single-use TOTP throttles an adversary to roughly two attempts per minute |
| R-005 | SQLite write contention under high order volume | Single-writer design plus WAL handles hundreds of orders/minute; ARC-007 keeps network I/O out of write transactions; Postgres migration path exists behind the `store` interface |
| R-006 | Energy price volatility makes withdrawal costs unpredictable | `estimated_burn_trx` is computed from the live `getEnergyFee` (ENR-016) and surfaced before the operator commits; ENR-017 makes an unreachable burn ceiling a visible degraded condition rather than a silent outage |
| R-007 | `hd-wallet` is a young library (v0.12.x) with a small user base | Pinned version; testnet soak in P14 before mainnet; the library's own test suite verifies against Trust Wallet Core vectors |
| R-008 | Binance geo-restrictions or downtime | `price.Provider` interface allows a fallback source without touching callers |
| R-009 | Energy provider goes down, raises prices, or exit-scams with the prepaid balance | Prepaid balance kept small; burn fallback means outages degrade cost rather than availability — **which now holds, because the burn ceiling is validated against the live chain parameter**; `max_price_trx` caps gouging; the provider never holds keys |
| R-010 | Energy rental silently fails and every withdrawal quietly burns TRX instead | `payd_energy_cost_trx_total{source}` makes the split visible; `energy.purchase_failed` and `energy.balance_low` alert actively |
| R-011 | A misconfigured consumer secret dead-letters an entire consumer's events | Per-consumer queue depth and dead counts exposed at `/ipn/consumers` and as metrics; head-of-line blocking scoped per `sequence_key` |
| R-012 | Energy requirements change with TRON network parameters | `min_energy` is configurable and SIGHUP-reloadable; `getEnergyFee` is read from the chain every 6 hours (ENR-016); actual `energy_used` from receipts is recorded per withdrawal |
| R-013 | **No-retry means a transient RPC fault costs an operator decision rather than resolving itself** | Accepted deliberately (§13.0 in the withdrawal-engine spec). `needs_operator` is alerted (OPS-006) and carries the txid and last error so resolution is a Tronscan lookup, not an investigation. The alternative — automatic retry — has an unbounded downside and this has a bounded one |
| R-014 | **The address pool grows monotonically because sweeping is rejected, driving polling cost and quota use up over months** | RES-001a caps the fast-polling set; RL-006 projects the quota forward and degrades readiness before the wall; `payd_addresses_with_balance` makes the trend visible. If the curve becomes a problem, sweeping is the deferred remedy (§22 below) |
| R-015 | **A funded terminal order is never resolved and the customer's money sits in the pool indefinitely** | ORD-005a/005b make it a distinct state with a dedicated view, and ORD-005d records the resolution. Detection is automatic; the refund itself remains an operator action (NG-006) |

---

## 21. Considered and rejected

| Feature | Decision | Reasoning |
|---|---|---|
| **Automatic retry of withdrawals and transfers** | **Rejected** | A lost broadcast response is indistinguishable from a lost broadcast, and `DUP_TRANSACTION_ERROR` on retry is evidence of success rather than failure. Any retry policy on a fund-moving action eventually pays twice, with a clean audit trail on both. Reconciliation against the chain gives the same recovery with none of the downside. See [`13-withdrawal-engine.md`](13-withdrawal-engine.md) §13.0 |
| **Automatic sweeping to cold storage** | Rejected | Adds a recurring per-address energy cost and a second signing path for no benefit under pool reuse, where balances already concentrate naturally. Funds stay in deposit addresses until the operator withdraws. **Note:** this decision is what makes [`12-resource-management.md`](12-resource-management.md) §12.4's bandwidth sourcing mandatory — with no sweeping, nothing else ever sends TRX to a deposit address, and a USDT-only address cannot pay for its own bandwidth |
| **Fresh-address-per-order mode** | Rejected | Scatters funds across hundreds of addresses, each needing its own energy and bandwidth to withdraw from, and multiplies resource-monitoring cost. Only viable paired with sweeping, which was also rejected |
| **Address-based energy purchase** | Rejected | No order ID to reconcile against and no failure signal; arrival detectable only by polling one's own resource state |
| **Energy aggregator routing** | Rejected as premature | Adds a dependency on top of the dependencies it manages. One provider plus a burn fallback already caps the downside |
| **Full webhook subscriptions table** | Rejected | Config-based named consumers deliver multi-consumer routing without a new table, and keep secrets out of API requests |
| **Rebuilding IPN payloads at send time** | Rejected | Contradicts the snapshotted `payload` column and produces bodies whose `event_type` disagrees with their own contents after a reorg. Replaced by an immutable snapshot plus `current_status` (IPN-021a) |

---

## 22. Deferred to a later version

- Automatic sweeping, should transaction volume grow enough to change the economics — and should the address-growth curve in R-014 become the binding constraint
- Fresh-address-per-order mode, should customer payment privacy become a requirement
- Multi-signature withdrawal approval
- Self-staking automation (`FreezeBalanceV2` via raw_json) to reduce reliance on rented energy
- Consumer registration via API rather than config
- An operator-facing "resolve withdrawal" workflow that records a Tronscan-verified outcome for `needs_operator` rows, rather than leaving them terminal
- Additional chains via the same `hd-wallet` foundation
