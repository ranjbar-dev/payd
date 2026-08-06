# Appendix A — Review findings and where they are addressed

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original Appendix A (+ "Additionally, at the operator's direction")
**ID prefixes in this file:** `F-1` … `F-30`
**Related:** every other spec file — this table is the traceability matrix from the v1.1 adversarial review to the requirements in this spec set that close each finding. Useful when you need to understand *why* a requirement exists, not just what it says.

---

All 30 findings from the v1.1 adversarial review, with the requirements that close them.

| Finding | Severity | Addressed by |
|---|---|---|
| F-1 — no asset filter in attribution | CRITICAL | ORD-002, ORD-002a, ORD-020, TST-017 |
| F-2 — reorg re-inclusion leaves payment orphaned | CRITICAL | CHN-016, CHN-017, TST-003a |
| F-3 — retried broadcast → `failed` → double payout | CRITICAL | §13.0, CHN-024a, WDR-014a, WDR-017, WDR-022a, TST-014, TST-021 |
| F-4 — idempotency key collides with single-use TOTP | CRITICAL | WDR-001a, WDR-003a, API-022, `used_totp` table, TST-015 |
| F-5 — `log_index` undefined across ingest paths | HIGH | DET-002a, DET-005a, DET-010a, DB-004 |
| F-6 — safety net and DR are TRC-20 only | HIGH | DET-010, DET-010b, DET-010c, DET-011, OPS-012 |
| F-7 — "assignment window" undefined | HIGH | ORD-002b, `addresses.assigned_at`/`released_at`, POOL-001, POOL-004, TST-019 |
| F-8 — no owner for expiry, cooldown, pool top-up | HIGH | W-010, LIF-001…LIF-005, `wallet.pool_max_size` |
| F-9 — confirmation by height, not block identity | HIGH | CNF-002, CNF-002a, CNF-002b, CNF-007 |
| F-10 — terminal orders holding funds | HIGH | ORD-005a, ORD-005b, ORD-005d, ORD-011, `expired_funded`/`cancelled_funded` |
| F-11 — outbound transfers never decoded | HIGH | DET-002b, DET-010c, `payments.direction`, WDR-023a |
| F-12 — two balance columns, no owner | HIGH | Three-column `balances`, BAL-001, BAL-002, WDR-005, API-014 |
| F-13 — daily cap counts only two states | HIGH | WDR-006, WDR-006a, WDR-006b, DB-002a, API-016 |
| F-14 — `signing`/`awaiting_energy` unrecoverable | HIGH | WDR-015, WDR-018, WDR-018b, WDR-019, WDR-019a, TST-016 |
| F-15 — `synchronous=NORMAL` power-loss re-sign | HIGH | ARC-006a, WDR-018a, TD-003 |
| F-16 — bandwidth monitored, never sourced | HIGH | §12.4 (RES-006…RES-009, RES-016), WDR-009f, WDR-009h, API-013, TST-018 |
| F-17 — burn table implies 100 sun/energy | HIGH | ENR-016, ENR-017, RES-020…RES-023, `chain_params`, API-010 |
| F-18 — `resource_wallet_index: 0` collides with pool | HIGH | CFG-013, POOL-001, POOL-002, POOL-008 |
| F-19 — load-balanced RPC, fake failover | HIGH | CHN-007a, CHN-011a, CHN-025, CHN-026, CFG-015, CNF-002a |
| F-20 — Tier 2 failure undefined; worker ownership | MEDIUM | §3.1 (W-002 folded into W-001), ARC-007, CHN-006a, DET-005a |
| F-21 — dust has no schema representation | MEDIUM | `payments.is_dust`, DET-007, ORD-005c, TST-020 |
| F-22 — single-flight excludes `awaiting_energy` | MEDIUM | WDR-007, WDR-008, WDR-005, TST-006 |
| F-23 — async acceptance vs sync rejection | MEDIUM | WDR-002a, WDR-002b, API-017 |
| F-24 — clock skew; three different "days" | MEDIUM | WDR-010a, OPS-005, DB-002a |
| F-25 — quota budget incomplete; unbounded growth | MEDIUM | §16 revised table, RES-001a, RL-001, RL-006, OPS-007 |
| F-26 — IPN-021 contradicts snapshotted payload | MEDIUM | IPN-021a, IPN-025 |
| F-27 — ordering key NULL for global events | MEDIUM | `ipn_outbox.sequence_key`, IPN-011, IPN-012, TST-023 |
| F-28 — order bound to a removed consumer | MEDIUM | CFG-006, CFG-010, IPN-002a |
| F-29 — `external_ref` collision returns wrong order | MEDIUM | API-002, ORD-008a, TST-022 |
| F-30 — Tier-1 crediting unsafe for odd tokens | LOW | DET-004a, DET-006, CFG-014 |

## Additionally, at the operator's direction

**No automatic retry anywhere in the withdrawal or transfer path** (see [`13-withdrawal-engine.md`](13-withdrawal-engine.md) §13.0, WDR-000…WDR-000d; [`01-overview-and-goals.md`](01-overview-and-goals.md) NG-008, G-010; [`15-rest-api.md`](15-rest-api.md) API-015; [`18-testing.md`](18-testing.md) TST-021). This goes beyond the review's F-3 recommendation, which only exempted broadcast from the blanket retry. Here every fund-moving action — broadcast, re-sign, bandwidth top-up, delegation — is attempted at most once for the lifetime of the row, and every ambiguous outcome is settled by reading the chain or handed to the operator.
