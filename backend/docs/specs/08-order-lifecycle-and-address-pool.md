# 8. Order lifecycle and address pool

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §8
**ID prefixes in this file:** `POOL-*`, `ORD-*`, `LIF-*`
**Related:** [`07-payment-detection.md`](07-payment-detection.md) (dust re-check ORD-005c), [`10-ipn-dispatcher.md`](10-ipn-dispatcher.md) (order.* events), [`06-chain-follower.md`](06-chain-follower.md) (CHN-014 order.reverted on reorg)

---

## 8.1 Address pool

States: `free → assigned → cooling → free`.

| ID | Requirement |
|---|---|
| POOL-001 | On order creation, the API MUST atomically select the lowest-`hd_index` address in `free` state **within the deposit pool range (0–999)**, set it to `assigned`, set `assigned_at = now`, clear `released_at`, and link it to the order |
| POOL-002 | If no `free` address exists, the service MUST derive the next unused HD index **within the pool range** via `AddressIndex(hdwallet.TRX, n)`, insert it, and assign it. Derivation MUST NOT cross into the operational band at 1000+ |
| POOL-003 | The Lifecycle Worker MUST derive additional addresses when the count of `free` addresses drops below `wallet.pool_min_free` (see LIF-003) |
| POOL-004 | On an order reaching a terminal state (`confirmed`, `expired`, `expired_funded`, `cancelled`, `cancelled_funded`), the address MUST move to `cooling` with `cooling_until = now + wallet.cooldown` and `released_at = now`. **`assigned_order_id` MUST NOT be cleared here** — it is cleared only at POOL-005, so the assignment window remains reconstructible throughout the cooldown |
| POOL-005 | An address in `cooling` whose `cooling_until` has passed MUST return to `free`, clearing `assigned_order_id`, `assigned_at`, and `released_at` (see LIF-002) |
| POOL-006 | An address MUST NOT be assigned to two orders simultaneously. This MUST be enforced by a transaction with a `WHERE state = 'free'` guard, not by application-level checking |
| POOL-007 | Addresses MAY be manually set to `disabled` via API to permanently remove them from rotation without deleting history |
| POOL-008 | The resource wallet address MUST be inserted at `state = 'disabled'` at startup and MUST NEVER be selectable by POOL-001 (see CFG-013) |

The cooldown exists because reuse plus late payment is the primary attribution hazard: without it, a customer who pays twenty minutes after their order expired would have their funds credited to whoever holds that address next.

## 8.2 Order state machine

```
pending ──payment(partial)──> partial ──payment(sufficient)──> paid ──solidified──> confirmed
   │                             │                               │
   │                             │                               └──reorg──> partial
   │                             │
   ├──ttl expired──> expired     ├──ttl expired, received=0 ──> expired
   │                             └──ttl expired, received>0 ──> expired_funded
   │
   ├──API cancel (received=0)──> cancelled
   └──API cancel (received>0, force)──> cancelled_funded
```

| ID | Requirement |
|---|---|
| ORD-001 | An order MUST be created with `expected_raw`, `asset`, and `expires_at = now + orders.default_ttl` (or a per-request override) |
| ORD-002 | `received_raw` MUST be the sum of all non-orphaned payments to the order's address **whose `direction` is `'in'`**, **whose `asset` equals the order's `asset`**, and whose `block_timestamp` falls within the order's assignment window |
| ORD-002a | A payment to an owned address whose `asset` **differs** from the asset of the order currently assigned to that address MUST be recorded with `status = 'unattributed'` and `order_id = NULL`, MUST emit `payment.unattributed`, and MUST NOT contribute to any order's `received_raw`. Without this, a customer who pastes the deposit address into the wrong wallet tab and sends 25 TRX against a 25 USDT order is credited in full — roughly a 70% loss per event, from a routine user error rather than an attack |
| ORD-002b | **The assignment window is `[orders.created_at, COALESCE(addresses.released_at, ∞))`, measured against `payments.block_timestamp`.** `detected_at` is service-local wall-clock and MUST NOT participate in attribution: it moves with catch-up and restarts, so replaying the same blocks after downtime would attribute payments differently than the live path did. Using chain time also means a payment sent at 10:29:58 against a 10:30:00 deadline is credited even though the follower did not observe it until 10:30:04 |
| ORD-003 | `received_raw < expected_raw` → status `partial` |
| ORD-004 | `received_raw >= expected_raw` → status `paid`, with `overpaid_raw = received_raw - expected_raw` |
| ORD-005 | An order in `pending` or `partial` past `expires_at` MUST transition to a terminal expired state (see LIF-001) |
| ORD-005a | An order transitioning to expiry or cancellation with `received_raw > 0` MUST be recorded as **`expired_funded`** / **`cancelled_funded`** and MUST NOT be treated as a clean expiry. The `order.expired` payload MUST carry `received`, `refundable: true`, and the `from_address` of each contributing payment, so a refund is actionable without a manual chain lookup |
| ORD-005b | `GET /api/v1/orders/funded-terminal` MUST list all orders in a terminal state with `received_raw > 0` and `resolution IS NULL`, with the payer address for each. Without this view these accumulate invisibly: `/payments/unattributed` does not surface them, because the payments *are* attributed — to a dead order — and the address returns to rotation with the funds still in it |
| ORD-005c | Before expiring a `partial` order, the Lifecycle Worker MUST re-evaluate ORD-004 **including `is_dust` rows**, so a customer who topped up a shortfall with a small second send is credited rather than expired |
| ORD-005d | `POST /api/v1/orders/{id}/resolve` MUST set `resolution` (`refunded` | `written_off` | `reattributed`), `resolution_note`, and `resolved_at`, and MUST be audit-logged. This is the operator's acknowledgement that a funded terminal order has been dealt with |
| ORD-006 | An `expired` order that subsequently receives a payment MUST NOT reopen. The payment MUST be recorded as `unattributed` |
| ORD-007 | A `partial` order MUST remain payable until `expires_at`; a top-up transfer MUST accumulate into `received_raw` |
| ORD-008 | Orders MUST accept an optional `external_ref` unique per order, so the caller can create idempotently |
| ORD-008a | Order creation MUST rely on the unique index for idempotency, **not on a pre-check**: the insert MUST be attempted and `ON CONFLICT (external_ref)` handled by re-reading the existing row and applying API-002's comparison. A read-then-write pre-check lets two concurrent creations with the same `external_ref` both pass |
| ORD-009 | Order creation MUST be rejected with HTTP 503 if the price for the requested asset is older than `price.stale_after` |
| ORD-010 | Order `metadata` MUST be an opaque JSON object stored verbatim and echoed in every IPN for that order |
| ORD-011 | `POST /orders/{id}/cancel` MUST return HTTP 409 for an order in `partial`, `paid`, or `confirmed` unless the request carries `force: true`, in which case ORD-005a applies and the order becomes `cancelled_funded` |

## 8.3 Unattributed payments

| ID | Requirement |
|---|---|
| ORD-020 | A payment to an owned address with no **matching** active order MUST be recorded with `status = 'unattributed'` and `order_id = NULL`. "Matching" means: the address has an order in a non-terminal state, the payment's `asset` equals that order's `asset` (ORD-002a), and the payment's `block_timestamp` falls within the assignment window (ORD-002b). Failing any of the three makes the payment unattributed |
| ORD-021 | Unattributed payments MUST be exposed at `GET /api/v1/payments/unattributed` for manual reconciliation |
| ORD-022 | An `unattributed` payment MUST still update the address's balance — the funds are real regardless of attribution. It MUST credit `pending_raw` while `seen` and `confirmed_raw` once `confirmed`, per BAL-001 |
| ORD-023 | An `unattributed` payment MUST emit a `payment.unattributed` IPN so the consumer can alert |
| ORD-024 | The API MUST provide `POST /api/v1/payments/{id}/attribute` to manually bind an unattributed payment to an order |

## 8.4 Lifecycle worker (W-010)

v1.1 described three time-triggered transitions and assigned an owner to none of them. The nearest candidate, the matcher, runs on new blocks — a chain event, not a clock event — so a quiet night with no payments to owned addresses would expire nothing, return no addresses to the pool, and leave the top-up check unrun while POOL-002 derived a fresh address for every incoming order.

| ID | Requirement |
|---|---|
| LIF-001 | Every 10 seconds, the Lifecycle Worker MUST transition orders in `pending` or `partial` with `expires_at <= now` to `expired` or `expired_funded` per ORD-005a, after applying ORD-005c's dust re-check, writing the `order.expired` outbox row **in the same transaction** (IPN-001) and moving the address to `cooling` (POOL-004) |
| LIF-002 | Every 10 seconds, it MUST return addresses in `cooling` with `cooling_until <= now` to `free`, clearing `assigned_order_id`, `assigned_at`, and `released_at` |
| LIF-003 | Every 60 seconds, it MUST evaluate POOL-003 and derive addresses up to `wallet.pool_initial_size`, subject to `wallet.pool_max_size`, beyond which **order creation MUST fail with HTTP 503** rather than deriving without bound. Unbounded derivation compounds RES-001's polling cost permanently (F-25) |
| LIF-004 | Every 60 seconds, it MUST prune `used_totp` rows older than 5 minutes (DB-007) |
| LIF-005 | The Lifecycle Worker MUST NOT perform any network I/O. All of its work is clock-driven local state |
