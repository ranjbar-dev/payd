# 9. Payments (`/payments`)

**Part of:** payd admin dashboard specification v1.0
**ID prefixes in this file:** `WPAY-*`
**Consumes:** `GET /payments`, `GET /payments/unattributed`, `GET /payments/orphaned`, `POST /payments/{id}/attribute`
**Related:** backend `API-027`, `ORD-020`–`ORD-024`, `CHN-017` (orphaning), `DET-*` (detection)

---

## 9.1 Purpose

This is the support desk's page. "The customer says they paid" is answered
here, from a txid, an address, an order id, or a date range — without a chain
explorer and without a database query (`WG-003`).

It also holds two worklists: payments the service could not attribute to any
order, and payments that were orphaned by a reorg and never came back.

## 9.2 Search

| ID | Requirement |
|---|---|
| WPAY-001 | Filters MUST cover everything backend `API-027` supports: `txid`, `address`, `order_id`, `status`, `direction`, `asset`, and inclusive Unix `from`/`to` |
| WPAY-002 | A pasted txid MUST work as a direct lookup. This is the most common support entry point and MUST NOT require choosing a filter field first — the search box MUST detect a txid, a TRON address, or a ULID by shape and apply the right filter |
| WPAY-003 | Columns: txid, direction, asset, amount, from, to, status, order, block height, block timestamp, dust flag |
| WPAY-004 | `direction` MUST be rendered as in/out with distinct styling. Outbound rows are withdrawal transfers ingested as ledger entries (backend `WDR-023`/`DET-002b`), and confusing them with deposits inverts the reading of an address's history |
| WPAY-005 | An outbound payment row MUST link to the withdrawal named by its `withdrawal_id`. Null MUST render as "not a service withdrawal", never as a broken or empty link — an outbound transfer the service did not broadcast is the case an operator most needs to see. The link MUST NOT be inferred from any other field |
| WPAY-006 | Status MUST render the four values distinctly: `seen`, `confirmed`, `orphaned`, `unattributed` (`UI-020`) |
| WPAY-007 | `block_timestamp` MUST be the primary time column, `detected_at` secondary and labelled "observed" (backend `ORD-002b`, mirrors `WORD-022`) |
| WPAY-008 | A dust payment MUST be flagged with its asset's `min_deposit` from `GET /assets` in the tooltip, so "why was this ignored" is answerable in place |
| WPAY-009 | Date filters MUST be entered in local time and converted to Unix seconds for the query, with the resolved UTC range shown |
| WPAY-010 | Tier B polling (30s) |

## 9.3 Payment detail

| ID | Requirement |
|---|---|
| WPAY-020 | Payment detail MAY be a drawer rather than a page — there is no `GET /payments/{id}` route, so the detail MUST be rendered from the list row already in hand |
| WPAY-021 | The drawer MUST show the full txid with a Tronscan link, `log_index`, block height, block id, both addresses, the raw and formatted amount, and every timestamp |
| WPAY-022 | Where the payment is attributed, it MUST link to the order and state whether its `block_timestamp` falls inside that order's assignment window |
| WPAY-023 | Where the payment is unattributed, the drawer MUST state **which of the three attribution conditions failed**, read from the backend's `unattributed_reason` field: `no_active_order`, `asset_mismatch`, or `outside_window` (backend `ORD-020`). "Unattributed" alone tells the operator nothing about what to do next; the asset-mismatch case in particular (backend `ORD-002a` — 25 TRX sent against a 25 USDT order) has a completely different remedy from the other two |
| WPAY-023a | The reason MUST NOT be inferred client-side by comparing the payment against the address's current order. That is a backend attribution decision (`INV-5`), and the comparison would be made against state that has since changed — the address may have been released or reassigned and the order expired — so it can produce a different answer than the one actually made |
| WPAY-023b | A null `unattributed_reason` on an unattributed payment MUST render as "reason not recorded", not as one of the three values and not as an error. Payments detected before the field existed report null |

## 9.4 Unattributed worklist (`/payments/unattributed`)

| ID | Requirement |
|---|---|
| WPAY-030 | The worklist MUST render `GET /payments/unattributed`, oldest first |
| WPAY-031 | Each row MUST show the failed-condition reason from `WPAY-023` as a distinct badge, with `asset_mismatch` at warning severity — those are the rows with a customer waiting on a refund decision. There is no backend filter for this field, so the UI MUST NOT offer one: filtering client-side would silently apply to the loaded page only and misrepresent the worklist's true size (`DAT-020`) |
| WPAY-032 | The worklist MUST state that these funds are real and already credited to the address's balance (backend `ORD-022`) — they are unattributed, not lost. An operator who believes the money is missing will look for it on chain |
| WPAY-033 | The attribute action MUST let the operator pick a target order by searching orders on the same address and asset, and MUST show the candidate order's expected/received amounts before submission |
| WPAY-034 | The attribute dialog MUST warn when the chosen order's asset differs from the payment's asset, and MUST require an extra confirmation. Attributing 25 TRX to a 25 USDT order is precisely the loss backend `ORD-002a` exists to prevent, and doing it manually loses the same money |
| WPAY-035 | The attribute dialog MUST warn when the target order is in a terminal state, since attribution will not reopen it (backend `ORD-006`) |
| WPAY-036 | After a successful attribution, the UI MUST invalidate the order, the order list, the funded-terminal list, the address, and the alarm counters |
| WPAY-037 | The worklist count MUST feed the nav alarm counter, combined with orphaned per `WOVW-006` |

## 9.5 Orphaned worklist (`/payments/orphaned`)

| ID | Requirement |
|---|---|
| WPAY-040 | The worklist MUST render `GET /payments/orphaned`: payments orphaned past `reorg_depth` without re-inclusion (backend `CHN-017`) |
| WPAY-041 | It MUST explain what an orphaned payment is: it was seen in a block, that block was reorganised away, and the transaction has not reappeared within the reorg depth. The money is very likely not there |
| WPAY-042 | Each row MUST link to the txid on Tronscan, since confirming the transaction's absence on chain is the operator's actual next step |
| WPAY-043 | An orphaned payment MUST show which order it had been contributing to, read from its own `order_id`, and that order's current status fetched from `GET /orders/{id}`. A reorg that reverts a `paid` order to `partial` is the case that matters, and backend `CHN-014` emits `order.reverted` for it. Orphaning sets the status and leaves the attribution in place, so `order_id` survives the reorg. Where it is null the payment was never attributed, and the row MUST say so rather than search for a candidate order — inferring one from the address and asset would name an order the backend never credited (`INV-5`) |
| WPAY-044 | There MUST be no control to "restore" or "re-confirm" an orphaned payment. Inclusion is a chain fact; the backend re-detects it if it reappears |
| WPAY-045 | A non-empty orphaned list MUST be surfaced at warning severity even when small. Backend `OPS-003` tracks `payd_payments_orphaned_unresolved` as an alerting metric because a single unresolved orphan usually means a customer was credited for money that no longer exists |
