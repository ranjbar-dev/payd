# 9. Confirmation tracking

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §9
**ID prefixes in this file:** `CNF-*`
**Related:** [`06-chain-follower.md`](06-chain-follower.md) (CHN-011a reorg suspicion blocks promotion), [`10-ipn-dispatcher.md`](10-ipn-dispatcher.md) (confirmations field in IPN payload)

---

| ID | Requirement |
|---|---|
| CNF-001 | The tracker MUST poll `POST /walletsolidity/getnowblock` every 20 seconds to obtain the current solidified height |
| CNF-002 | A payment MUST transition `seen → confirmed` only when **all** of the following hold: (a) `solidified_height >= payment.block_height`; (b) `payment.block_id` equals the `block_id` currently stored in `blocks` for that height; (c) that block's ancestry has been verified unbroken to the current tip; and (d) `last_height - payment.block_height >= tron.confirmations_required`. **Height is a position, not an identity** — if the block recorded at height H was orphaned, the solidified chain contains a *different* block at H, and a height-only predicate promotes a payment that no longer exists on-chain. Payments below `solidified_height` whose `block_id` no longer matches MUST be set to `orphaned`, never `confirmed` |
| CNF-002a | `solidified_height` MUST be stored monotonically: `UPDATE crawler_state SET solidified_height = MAX(solidified_height, ?)`. A load-balanced solidity endpoint can report a lower height than a previous call (CHN-007a) |
| CNF-002b | The confirmation tracker MUST NOT promote a payment whose block lies in a range with an unresolved reorg suspicion (CHN-011a). Reorg reconciliation takes precedence over promotion |
| CNF-003 | An order in `paid` MUST transition to `confirmed` when all of its contributing payments are `confirmed` |
| CNF-004 | The tracker MUST NOT poll per-payment endpoints; one solidity-head call serves all pending payments |
| CNF-005 | Tron solidification requires approximately 19 blocks (~57 seconds) given 27 Super Representatives and a two-thirds-plus-one threshold. The value MUST remain configurable via `tron.confirmations_required`, and CNF-002(d) MUST actually read it — in v1.1 the key existed but nothing consumed it |
| CNF-006 | If `solidified_height` fails to advance for more than 5 minutes, `/readyz` MUST report unhealthy |
| CNF-007 | The IPN payload's `confirmations` field MUST be computed as `last_height - payment.block_height`, clamped at zero. v1.1 emitted a literal `3` with no defined computation anywhere, so any consumer gating shipment on it was gating on an undefined number |
