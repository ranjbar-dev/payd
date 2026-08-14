# 12. Resources and energy (`/resources`)

**Part of:** payd admin dashboard specification v1.0
**ID prefixes in this file:** `WRES-*`
**Consumes:** `GET /energy/status`, `GET /energy/purchases`, `GET /resources/grants`, `GET /resources/wallet`, `GET /chain/params`
**Related:** backend [`12-resource-management.md`](../../../backend/docs/specs/12-resource-management.md), `API-041`, `API-042`, `ENR-*`, `RES-*`

---

## 12.1 Purpose

Withdrawals fail for resource reasons more often than for balance reasons, and
the cost of a withdrawal varies several-fold depending on where its energy came
from. This page answers: can the service currently source resources, what has
it been paying, and is the expensive fallback quietly carrying the load.

Layout: four cards — provider status, resource wallet, chain parameters — over
two tables: purchases and grants.

## 12.2 Energy provider

| ID | Requirement |
|---|---|
| WRES-001 | The provider card MUST render `GET /energy/status`: `provider`, `balance_trx`, `last_checked_at`, `last_error`, `consecutive_failures`, and the purchase outcome counts |
| WRES-002 | `balance_trx` MUST render against the configured `energy.balance_warn_trx`, both echoed from `GET /energy/status`, and the low-balance state MUST be taken from that response's `balance_low` flag. The UI MUST NOT compare the two itself: they are decimal money strings and `INV-2` forbids it. An absent or unparsable balance is reported as not-low by the backend and MUST render as unknown rather than as healthy |
| WRES-003 | `consecutive_failures` MUST be shown, and at 5 or more the card MUST state that tier 1 is being skipped entirely for 10 minutes (backend `ENR-012`). Without this, the operator sees rising burn cost with no visible cause |
| WRES-004 | `last_error` MUST render verbatim, with its age |
| WRES-005 | `last_checked_at` MUST render relative. The balance is checked every 15 minutes (backend `ENR-010`); a much older figure means that worker is stalled |
| WRES-006 | The card MUST state that provider calls do not count against the TronGrid quota (backend `ENR-014`), so a quota problem is never diagnosed as a provider problem |
| WRES-007 | There MUST be no control to top up the provider balance, purchase energy manually, or change the provider. None exists in the API, and the provider is deliberately given no signing authority (backend `ENR-013`) |

## 12.3 Chain parameters

| ID | Requirement |
|---|---|
| WRES-010 | The card MUST render `GET /chain/params`: `getEnergyFee`, `getTransactionFee`, and when they were read |
| WRES-011 | `getEnergyFee` MUST be presented with its consequence, not as a bare number: the burn cost of a worst-case 131,000-energy transfer at the current price. At 100 sun that is ~13 TRX; at 210 sun ~27.5; at 420 sun ~55 (backend §12.2) |
| WRES-012 | The card MUST show `worst_case_burn_trx` against `max_burn_trx`, both from `GET /chain/params`, and MUST take the verdict from that response's `burn_exceeds_ceiling`. The UI MUST NOT compute either figure or compare them. When `burn_exceeds_ceiling` is ABSENT the comparison is unknown and MUST render as unknown, never as within limits |
| WRES-013 | The read age MUST be shown. Parameters are refreshed at startup and every 6 hours (backend `ENR-016`); a much older figure means worker W-011 is stalled |
| WRES-014 | Never-populated parameters MUST render at critical severity with the consequence stated: the service holds withdrawals rather than assuming a price (backend `RES-022`). This is the 503 `chain_params_unavailable` response, not a null field — the endpoint has no row to return |
| WRES-015 | The comparison figures MUST come from the backend where available, and any figure the UI derives for illustration MUST be labelled as illustrative. The authoritative per-address numbers are `estimated_burn_trx` on `/wallets/needs-resources` |

## 12.4 Resource wallet

| ID | Requirement |
|---|---|
| WRES-020 | The card MUST render `GET /resources/wallet`: address, confirmed TRX, available and limit energy, available and limit bandwidth, and the non-failed self-delegation count and staked amount per resource type |
| WRES-021 | It MUST be labelled as the withdrawal path's dependency. Backend `API-042` exists because this wallet being empty blocks every tier-2 delegation and every bandwidth top-up |
| WRES-022 | It MUST state that staking and unstaking are manual chain operations the service never performs (backend `RES-014`), and that unstaking takes 14 days |
| WRES-023 | It MUST show whether the wallet's TRX covers `resources.bandwidth_topup_trx` from `GET /config`, rendering both figures side by side. The comparison itself is not computed here; where the backend offers no verdict the UI states both values and leaves the judgement to the operator |
| WRES-024 | The address MUST link to its (permanently `disabled`) entry on the addresses page (backend `POOL-008`) |

## 12.5 Purchases

| ID | Requirement |
|---|---|
| WRES-030 | The table MUST render `GET /energy/purchases`: id, provider, provider order id, withdrawal, receiver address, resource type, amount, duration, `quoted_trx`, `actual_trx`, status, failure reason, delegation txid, created, delegated |
| WRES-031 | Quoted and actual MUST appear as adjacent columns. A persistent gap between them is a provider problem the operator should see |
| WRES-032 | Statuses `quoted`, `purchased`, `delegated`, `expired`, `failed` MUST be styled per `UI-020`. A `purchased` row that never reached `delegated` is money spent for nothing |
| WRES-033 | Each row MUST link to its withdrawal where `withdrawal_id` is set |
| WRES-034 | `delegation_txid` MUST link to Tronscan |
| WRES-035 | The table MUST be filterable by status and support cursor pagination per `API-025`. The status filter is a server-side query parameter on `GET /energy/purchases`; the UI MUST NOT filter the returned page (`DAT-020`) |
| WRES-036 | Tier D (manual refresh). Purchase history is not a live surface |

## 12.6 Grants

| ID | Requirement |
|---|---|
| WRES-040 | The table MUST render `GET /resources/grants` with the filters backend `API-041` supports: withdrawal, status, resource type |
| WRES-041 | Columns: id, address, resource type (`ENERGY`/`BANDWIDTH`), source (`rented`/`self_delegated`/`topup`), amount, txid, status, created, confirmed |
| WRES-042 | The table MUST be reachable directly from a withdrawal in `awaiting_resources` or `awaiting_energy`, since backend `API-041` exists to expose why a withdrawal is waiting |
| WRES-043 | A grant broadcast is fund-moving and follows the no-retry rule (backend `RES-013`). The table MUST offer no retry, re-broadcast, or resend control, and MUST state that an unresolved grant is resolved on chain rather than re-attempted |
| WRES-044 | An unconfirmed grant older than a few minutes MUST be flagged, with its txid linked to Tronscan |

## 12.7 Cost visibility

| ID | Requirement |
|---|---|
| WRES-050 | The page MUST show the burn-versus-rent split for a recent window, sourced from `GET /reports/fees` rather than computed here (`INV-5`, `UI-001`) |
| WRES-051 | The split MUST be presented as the diagnostic backend `OPS-004` intends: rising burn cost is what a silently failing provider looks like |
| WRES-052 | The page MUST link to the full fee report ([`14`](14-reports-and-exports.md)) rather than growing its own date controls |
