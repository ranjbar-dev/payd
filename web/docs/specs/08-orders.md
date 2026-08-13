# 8. Orders (`/orders`)

**Part of:** payd admin dashboard specification v1.0
**ID prefixes in this file:** `WORD-*`
**Consumes:** `GET /orders`, `GET /orders/{id}`, `GET /orders/{id}/events`, `GET /orders/funded-terminal`, `POST /orders`, `POST /orders/{id}/cancel`, `POST /orders/{id}/extend`, `POST /orders/{id}/resolve`
**Related:** backend [`08-order-lifecycle-and-address-pool.md`](../../../backend/docs/specs/08-order-lifecycle-and-address-pool.md), `API-001`–`API-006`, `API-028`–`API-030`

---

## 8.1 The state machine the UI must render

```
pending ──payment(partial)──> partial ──payment(sufficient)──> paid ──solidified──> confirmed
   │                             │                               │
   │                             │                               └──reorg──> partial
   ├──ttl expired──> expired     ├──ttl expired, received=0 ──> expired
   │                             └──ttl expired, received>0 ──> expired_funded
   ├──cancel (received=0)──> cancelled
   └──cancel (received>0, force)──> cancelled_funded
```

| ID | Requirement |
|---|---|
| WORD-001 | The order detail MUST render this machine with the current state marked, and MUST show which transitions are still possible. An operator deciding whether to cancel needs to know that `paid` can still revert to `partial` on a reorg |
| WORD-002 | `expired` and `expired_funded` MUST NOT be rendered as the same status, nor `cancelled` and `cancelled_funded`. The `_funded` variants mean customer money is sitting in a released address (backend `ORD-005a`) |
| WORD-003 | The UI MUST NOT compute an order's status from its amounts. `received_raw >= expected_raw` is the backend's determination, made against the assignment window and the dust rules (`INV-5`) |

## 8.2 List

| ID | Requirement |
|---|---|
| WORD-010 | Columns: id, `external_ref`, status, asset, expected, received, consumer, address, created, expires |
| WORD-011 | Filters MUST cover everything the backend supports: `status`, `asset`, date range, `external_ref`, `consumer`, `address` (backend `API-028`). Filtering by `external_ref` is what lets support answer a consumer's question without a write-path idempotency probe |
| WORD-012 | Expected and received MUST be adjacent columns so a shortfall is visible without arithmetic (`UI-001` forbids computing the difference) |
| WORD-013 | A `partial` order MUST show a shortfall indicator derived from the backend's own fields, never from client-side subtraction |
| WORD-014 | An order approaching `expires_at` MUST show the remaining time relative. An expired-but-unfunded order is routine; an expiring *partial* order is a customer about to lose money |
| WORD-015 | Overpaid orders MUST show `overpaid` where non-zero. It is a refund obligation |
| WORD-016 | Tier B polling (30s) |

## 8.3 Detail

| ID | Requirement |
|---|---|
| WORD-020 | The detail MUST render every field `GET /orders/{id}` returns, including the opaque `metadata` object, pretty-printed. `metadata` is what the consumer service put there and is often the only link back to a customer |
| WORD-021 | The payments table MUST list every payment against this order: txid, from address, amount, status, block height, block timestamp, `is_dust` |
| WORD-022 | `block_timestamp` MUST be labelled as chain time and MUST be the timestamp shown for attribution purposes. `detected_at` is service-local and MUST be labelled "observed" and visually secondary — backend `ORD-002b` explicitly excludes it from attribution, and an operator comparing it against a deadline will reach the wrong conclusion |
| WORD-023 | The assignment window MUST be shown explicitly: order `created_at` to the address's `released_at` (or "still assigned"). This is the interval that decides attribution, and it is otherwise invisible |
| WORD-024 | A dust payment MUST be flagged. Backend `ORD-005c` re-checks dust before expiry, so a dust row can be the difference between `expired` and `paid` |
| WORD-025 | The events tab MUST render `GET /orders/{id}/events`: consumer, event type, status, attempts, last response code, last error, created, delivered. Backend `API-030` exists to make "the webhook never arrived" self-service |
| WORD-026 | A `dead` event in the events tab MUST link to the webhooks page with a retry control. The retry is on the IPN, not on the order |
| WORD-027 | The detail MUST link to the assigned address's page and to each payment's txid on Tronscan |
| WORD-028 | Tier A polling (5s) while the order is `pending` or `partial`; drops to manual on any terminal status (`DAT-002`) |

## 8.4 Create

| ID | Requirement |
|---|---|
| WORD-030 | The create form MUST accept `asset`, either `amount` or `amount_usd` (not both), `external_ref`, `consumer`, `ttl_seconds`, and `metadata` as raw JSON |
| WORD-031 | The asset list MUST come from `GET /assets`, with the decimals it reports used to validate input precision. Backend `API-034` exists so clients stop hardcoding precision |
| WORD-032 | The consumer list MUST come from `GET /ipn/consumers`, and selecting one MUST be explicit. Backend `API-004` routes an omitted consumer to `ipn.default_consumer`, which for a dashboard-created order is usually not what anyone wanted |
| WORD-033 | **The form MUST warn that a dashboard-created order has no consumer service expecting its IPNs.** An operator creating a manual invoice is producing events that will be delivered to a service that never asked for them |
| WORD-034 | Orders created here MUST be marked in `metadata` with a `created_by: "dashboard"` field, so they are distinguishable in reports and exports from consumer-created ones |
| WORD-035 | Amount input MUST be a text field with a string-preserving mask. A numeric input coerces and rounds (`UI-001`) |
| WORD-036 | `external_ref` MUST be described as an idempotency key, with the exact behaviour of backend `API-002` stated: an exact match on `asset`, `expected_raw`, and `consumer` returns the existing order with 200; any mismatch returns 409 `external_ref_conflict` |
| WORD-037 | On 409 `external_ref_conflict`, the UI MUST render the conflicting fields from `details` side by side — requested versus stored — and MUST link to the existing order. It MUST NOT silently show the stored order as if creation succeeded: that is the exact failure backend `API-002` was written to close, where a 500 USDT request rendered a 25 USDT order |
| WORD-038 | On 200 (exact idempotent match), the UI MUST state clearly that an existing order was returned rather than a new one created |
| WORD-039 | On 503 `pool_exhausted` (backend `API-006`/`LIF-003`), the UI MUST explain that the address pool has reached `wallet.pool_max_size` with no free address, and link to the addresses page. It MUST NOT present this as a transient error to be retried |
| WORD-040 | On 503 from stale prices (backend `ORD-009`), the UI MUST name price staleness as the cause and link to the prices card |
| WORD-041 | On 400 for an unknown or disabled consumer (backend `API-004`/`IPN-015`), the UI MUST name the consumer and link to the webhooks page |

## 8.5 Cancel, extend

| ID | Requirement |
|---|---|
| WORD-050 | Cancel MUST use `<ConfirmDialog>` restating the order, its status, and its received amount |
| WORD-051 | On 409 for an order in `partial`, `paid`, or `confirmed`, the UI MUST NOT auto-set `force: true`. It MUST surface the received amount and require a second, explicit confirmation whose text states that the order becomes `cancelled_funded` and the funds remain in the deposit address awaiting a resolution record (backend `ORD-011`, `ORD-005a`) |
| WORD-052 | Force-cancel MUST warn that the address returns to the pool after cooldown with the funds still in it, and that the order will appear in the funded-terminal worklist |
| WORD-053 | Extend MUST accept `ttl_seconds` and MUST show the resulting expiry before submission |
| WORD-054 | Extend MUST enforce the backend's ceiling in the UI as well: no expiry later than 24 hours after `created_at` (backend `API-029`). The input MUST cap rather than submit a value that will 400 |
| WORD-055 | Extend MUST be disabled for terminal orders with the reason stated, since the backend returns 409 |
| WORD-056 | Neither cancel nor extend requires TOTP. The UI MUST NOT prompt for one — an unnecessary code prompt trains the operator to generate codes reflexively, which is the habit the withdrawal flow depends on them not having |

## 8.6 Funded-terminal worklist (`/orders/funded-terminal`)

The highest-value screen on this page. Backend `ORD-005b`: these accumulate
invisibly, because the payments *are* attributed — to a dead order — so they
never appear in the unattributed queue, and the address returns to rotation
with the funds still in it.

| ID | Requirement |
|---|---|
| WORD-060 | The worklist MUST render `GET /orders/funded-terminal`: order, status, asset, received, the payer address for each contributing payment, and age |
| WORD-061 | The payer address MUST be prominent and copyable. It is what makes a refund actionable without a chain lookup — backend `ORD-005a` puts it in the payload for exactly this reason |
| WORD-062 | Rows MUST be sorted oldest first. Age is the risk: an unresolved funded order from three weeks ago is a customer who has already complained |
| WORD-063 | The resolve action MUST record `resolution` (`refunded` \| `written_off` \| `reattributed`), a `resolution_note`, and MUST require the note to be non-empty. The note is the audit trail's only explanation |
| WORD-064 | The resolve dialog MUST state plainly that **this records a decision; it does not move any funds.** Choosing `refunded` does not issue a refund — a refund is a separate withdrawal the operator makes from the deposit address (backend `NG-006`) |
| WORD-065 | For `refunded`, the dialog MUST offer a link that pre-fills the withdrawal wizard with the deposit address as source and the payer address as destination — as a convenience for a *separate* deliberate action, never as part of the same submission |
| WORD-066 | A resolved order MUST leave the worklist immediately and MUST remain findable in the main list with its resolution and note displayed |
| WORD-067 | The worklist count MUST feed the nav alarm counter |
| WORD-068 | Resolve requires no TOTP (backend `ORD-005d`), but MUST still be audit-visible: the dialog MUST state that the action is written to `audit_log` |
