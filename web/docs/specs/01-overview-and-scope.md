# 1. Overview, goals, non-goals, page map

**Part of:** payd admin dashboard specification v1.0
**ID prefixes in this file:** `WG-*` (goals), `WNG-*` (non-goals)
**Related:** [`03-architecture-and-bff.md`](03-architecture-and-bff.md) for how these goals map to the proxy layer; [`16-implementation-phases.md`](16-implementation-phases.md) for build order

---

## 1.1 Overview

A single-operator web dashboard for the `payd` TRON payment processor. It is the
only human interface to a service that is otherwise API-only: the backend
explicitly excludes a built-in frontend (backend `NG-005`), so everything an
operator does by hand — investigating a missing payment, resolving a stuck
withdrawal, moving funds out — happens here or in `curl`.

The backend surface is 49 authenticated routes across 8 scopes, plus 3
unauthenticated ones. This dashboard covers all 52 across ten pages — see
[`17-api-coverage-matrix.md`](17-api-coverage-matrix.md).

The design pressure is not feature count. It is that **several of these screens
show money in an ambiguous state, and the wrong affordance costs the full
amount.** A withdrawal in `needs_operator` means funds may or may not have
moved; a button labelled "retry" next to it is a duplicate payout waiting for a
tired operator at 3am. Most of what follows is about which controls must *not*
exist, and about making the backend's guardrails visible rather than
re-deciding them.

## 1.2 Goals

| ID | Goal |
|---|---|
| WG-001 | Give the operator one place to see whether the service is healthy: chain lag, worker heartbeats, TronGrid quota, price staleness, readiness |
| WG-002 | Surface the four alarm conditions — `needs_operator` withdrawals, unattributed payments, funded terminal orders, dead IPNs — permanently and unmissably, since each is money or a customer obligation sitting unresolved |
| WG-003 | Let support answer "where is my customer's payment" from a txid, an address, an order id, or a consumer's `external_ref`, without a chain explorer or a database query |
| WG-004 | Let the operator move funds out safely: estimate first, see the real resource cost, confirm explicitly, supply a fresh TOTP code, and never be able to submit the same movement twice by accident |
| WG-005 | Make every terminal withdrawal outcome explicable from the UI alone — `failure_reason`, `txid`, `resolved_by`, raw `broadcast_response` — without a Tronscan lookup (backend `API-017`) |
| WG-006 | Keep the payd API key server-side at all times, so the backend can stay on loopback behind a TLS-terminating proxy as backend `OPS-009` requires |
| WG-007 | Stay inside the backend's rate limit (100 req/min per key, 10 on withdrawal routes) with several tabs open, by budgeting every poll |
| WG-008 | Render every amount exactly as the backend sent it, so the number on screen is the number in the database |
| WG-009 | Make the resource economics visible — burn versus rent, per address, at the live `getEnergyFee` — so a silently failed energy provider shows up as rising cost rather than an unnoticed expense |
| WG-010 | Be operable from a phone for reading and triage, so an on-call operator can assess an alarm without a laptop |

## 1.3 Non-goals

| ID | Non-goal |
|---|---|
| WNG-001 | Multi-tenant or multi-merchant views. The backend is single-tenant (backend `NG-001`); so is this |
| WNG-002 | **Any business logic.** No client-side determination of order status, payment sufficiency, withdrawal permissibility, resource sufficiency, or fee estimation. The backend decides; this renders |
| WNG-003 | **Any retry, resume, or re-broadcast affordance for a withdrawal.** Not as a button, not as a hidden endpoint, not as an automatic refetch of a failed mutation. See [`11-withdrawals.md`](11-withdrawals.md) §11.0 |
| WNG-004 | A second data store. The dashboard has no database. Session state lives in a signed cookie; everything else is fetched from payd on demand |
| WNG-005 | User management, roles, or permissions. One operator, one credential, one payd key. Scope enforcement stays in the backend |
| WNG-006 | Editing configuration. `GET /config` is read-only by design (backend `API-043`); config changes are a YAML edit and a restart |
| WNG-007 | Direct chain interaction. No web3 provider, no wallet connect, no client-side signing. The dashboard never holds a key of any kind |
| WNG-008 | Real-time push. The backend exposes no websocket or SSE; polling is the only mechanism and its cost is budgeted in [`05-data-fetching.md`](05-data-fetching.md) |
| WNG-009 | Historical charting beyond what `/reports/*` and `/chain/quota` return. No time-series store, no metric scraping — Prometheus already exists for that |
| WNG-010 | Customer-facing payment pages. This is an operator tool; the checkout UI is the consumer service's problem |

## 1.4 The ten pages

| Page | Route | Purpose | Spec |
|---|---|---|---|
| Overview | `/` | Health, alarms, chain, quota at a glance | [`07`](07-overview-page.md) |
| Orders | `/orders` | Order search, detail, lifecycle actions, funded-terminal worklist | [`08`](08-orders.md) |
| Payments | `/payments` | Payment search, unattributed and orphaned worklists | [`09`](09-payments.md) |
| Addresses | `/addresses` | Pool state, balances, resource health, address actions | [`10`](10-addresses.md) |
| Withdrawals | `/withdrawals` | Payout list, detail, create wizard, `needs_operator` worklist | [`11`](11-withdrawals.md) |
| Resources | `/resources` | Energy provider, purchases, grants, resource wallet, chain params | [`12`](12-resources-and-energy.md) |
| Webhooks | `/webhooks` | Consumers, dead letters, replay, test ping | [`13`](13-webhooks.md) |
| Reports | `/reports` | Volume, fees, CSV export | [`14`](14-reports-and-exports.md) |
| System | `/system` | Workers, quota history, effective config, audit log | [`15`](15-system-and-audit.md) |
| Login | `/login` | Dashboard authentication | [`04`](04-auth-and-session.md) |

| ID | Requirement |
|---|---|
| WG-020 | Every page MUST be reachable from the persistent navigation. There MUST be no page reachable only by typing a URL |
| WG-021 | Every backend route MUST be consumed by at least one page, or explicitly listed as unconsumed with a reason in [`17-api-coverage-matrix.md`](17-api-coverage-matrix.md). An endpoint the operator cannot reach is an endpoint that will rot |
| WG-022 | The four worklists (funded-terminal orders, unattributed payments, orphaned payments, `needs_operator` withdrawals) MUST each show a count in the navigation when non-zero, and MUST NOT be buried as a filter value inside a general list. They exist to reach zero, not to be browsed |

## 1.5 Deliberately absent controls

Recorded here so they are not "added as an obvious omission" later. Each was
considered and rejected for a stated reason.

| Absent control | Why |
|---|---|
| Retry / resume / re-broadcast a withdrawal | Backend `WDR-000`. The endpoint does not exist and must not be invented client-side |
| Bulk-approve withdrawals | Each withdrawal needs its own TOTP code and its own deliberate decision. Bulk approval defeats both |
| Edit an order's amount, asset, or consumer | Backend `ORD-*` has no such transition; `consumer` is immutable after creation (`API-005`) |
| Delete anything | The backend exposes no DELETE route. History is the audit trail |
| Stake / unstake TRX from the resource wallet | Backend `RES-014` explicitly forbids the service from doing this. It is a manual chain operation |
| Manually mark a payment confirmed | Confirmation is a chain fact, tracked by the Confirmation Tracker. There is no override |
| Client-side amount arithmetic ("withdraw max", "50%") | Would require float math on money. If a max-withdraw figure is wanted, the backend must expose it |
