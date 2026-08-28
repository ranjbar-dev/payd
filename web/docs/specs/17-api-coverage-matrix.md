# 17. API coverage matrix

**Part of:** payd admin dashboard specification v1.0
**Source of truth:** `backend/internal/api/routes.go` and `backend/internal/api/openapi.yaml`
**Related:** [`16-implementation-phases.md`](16-implementation-phases.md) `WP-002`

---

Every route the backend serves, and the page that consumes it. Two directions
of use: check that nothing is stranded, and find which page breaks when a route
changes.

**49 authenticated routes + 3 public routes. All 52 are consumed.**

## Orders — 8

| Method | Path | Scope | Page | Spec |
|---|---|---|---|---|
| POST | `/api/v1/orders` | `orders:write` | Orders → create | `WORD-030` |
| GET | `/api/v1/orders` | `orders:read` | Orders → list; Orders → create (external_ref conflict lookup); Payments → attribute dialog (order search) | `WORD-010`, `WORD-037`, `WPAY-033` |
| GET | `/api/v1/orders/funded-terminal` | `orders:read` | Orders → funded-terminal worklist | `WORD-060` |
| GET | `/api/v1/orders/{id}` | `orders:read` | Orders → detail; Payments → payment drawer, orphaned worklist; Webhooks → dead letters | `WORD-020`, `WPAY-022`, `WPAY-043`, `WIPN-032` |
| POST | `/api/v1/orders/{id}/extend` | `orders:write` | Orders → detail | `WORD-053` |
| GET | `/api/v1/orders/{id}/events` | `orders:read` | Orders → detail, events tab | `WORD-025` |
| POST | `/api/v1/orders/{id}/cancel` | `orders:write` | Orders → detail | `WORD-050` |
| POST | `/api/v1/orders/{id}/resolve` | `orders:write` | Orders → funded-terminal worklist | `WORD-063` |

## Payments — 4

| Method | Path | Scope | Page | Spec |
|---|---|---|---|---|
| GET | `/api/v1/payments` | `orders:read` | Payments → search | `WPAY-001` |
| GET | `/api/v1/payments/unattributed` | `orders:read` | Payments → unattributed worklist | `WPAY-030` |
| GET | `/api/v1/payments/orphaned` | `orders:read` | Payments → orphaned worklist | `WPAY-040` |
| POST | `/api/v1/payments/{id}/attribute` | `orders:write` | Payments → unattributed worklist | `WPAY-033` |

## Wallets — 7

| Method | Path | Scope | Page | Spec |
|---|---|---|---|---|
| GET | `/api/v1/wallets` | `wallets:read` | Addresses → pool list; Withdrawals → wizard (known-destination check) | `WADR-001`, `WWD-053` |
| GET | `/api/v1/wallets/with-balance` | `wallets:read` | Withdrawals → wizard source; Addresses → filter | `WADR-070` |
| GET | `/api/v1/wallets/needs-resources` | `wallets:read` | Addresses → needs-resources view | `WADR-040` |
| GET | `/api/v1/wallets/{address}` | `wallets:read` | Addresses → detail | `WADR-030` |
| POST | `/api/v1/wallets/{address}/disable` | `wallets:write` | Addresses → detail | `WADR-060` |
| POST | `/api/v1/wallets/{address}/delegate` | `resources:write` + TOTP | Addresses → detail, needs-resources | `WADR-050` |
| POST | `/api/v1/wallets/{address}/clear-drift` | `wallets:write` + TOTP | Addresses → detail | `WADR-022` |

## Withdrawals — 6

| Method | Path | Scope | Page | Spec |
|---|---|---|---|---|
| POST | `/api/v1/withdrawals` | `withdrawals:write` + TOTP | Withdrawals → wizard step 3 | `WWD-070` |
| GET | `/api/v1/withdrawals` | `withdrawals:read` | Withdrawals → list, `needs_operator` worklist | `WWD-020` |
| GET | `/api/v1/withdrawals/limits` | `withdrawals:read` | Withdrawals → limit meter; wizard step 1 | `WWD-025` |
| POST | `/api/v1/withdrawals/estimate` | `withdrawals:read` | Withdrawals → wizard step 2 | `WWD-060` |
| GET | `/api/v1/withdrawals/{id}` | `withdrawals:read` | Withdrawals → detail | `WWD-030` |
| POST | `/api/v1/withdrawals/{id}/resolve` | `withdrawals:write` + TOTP | Withdrawals → resolve dialog | `WWD-040` |

## IPN — 5

| Method | Path | Scope | Page | Spec |
|---|---|---|---|---|
| GET | `/api/v1/ipn/consumers` | `orders:read` | Webhooks → consumers; Orders → create form (consumer picker) | `WIPN-010`, `WORD-032` |
| GET | `/api/v1/ipn/dead` | `orders:read` | Webhooks → dead letters | `WIPN-030` |
| POST | `/api/v1/ipn/{id}/retry` | `orders:write` | Webhooks → dead letters | `WIPN-035` |
| POST | `/api/v1/ipn/test` | `orders:write` | Webhooks → consumers | `WIPN-020` |
| POST | `/api/v1/ipn/replay` | `orders:write` | Webhooks → bulk replay | `WIPN-040` |

## Chain and resources — 7

| Method | Path | Scope | Page | Spec |
|---|---|---|---|---|
| GET | `/api/v1/chain/status` | `wallets:read` | Overview → chain card; System → health tab | `WOVW-020`, `WSYS-061` |
| GET | `/api/v1/chain/quota` | `wallets:read` | Overview → quota card; System → quota tab, health tab | `WOVW-030`, `WSYS-010`, `WSYS-061` |
| GET | `/api/v1/chain/params` | `wallets:read` | Resources → chain params card; Overview → readiness card | `WRES-010`, `WOVW-012` |
| GET | `/api/v1/resources/wallet` | `wallets:read` | Resources → resource wallet; delegate dialog; Addresses → pool list (resource-wallet row) | `WRES-020`, `WADR-052`, `WADR-005` |
| GET | `/api/v1/resources/grants` | `wallets:read` | Resources → grants | `WRES-040` |
| GET | `/api/v1/energy/status` | `wallets:read` | Resources → provider card | `WRES-001` |
| GET | `/api/v1/energy/purchases` | `wallets:read` | Resources → purchases | `WRES-030` |

## Reports and exports — 4

| Method | Path | Scope | Page | Spec |
|---|---|---|---|---|
| GET | `/api/v1/reports/volume` | `orders:read` | Reports → volume; Overview → volume card | `WRPT-001`, `WOVW-052` |
| GET | `/api/v1/reports/fees` | `wallets:read` | Reports → fees; Resources → cost split | `WRPT-020`, `WRES-050` |
| GET | `/api/v1/export/orders.csv` | `orders:read` | Reports; Orders → export | `WRPT-030` |
| GET | `/api/v1/export/withdrawals.csv` | `withdrawals:read` | Reports; Withdrawals → export | `WRPT-030` |

## Operations and identity — 8

| Method | Path | Scope | Page | Spec |
|---|---|---|---|---|
| GET | `/api/v1/stats` | any key | Overview → alarms strip, nav-shell alarm counters; Addresses → pool health | `WOVW-004`, `UI-071`, `WADR-008a` |
| GET | `/api/v1/prices` | any key | Overview → prices card | `WOVW-050` |
| GET | `/api/v1/assets` | any key | System → assets; every amount input | `WSYS-030` |
| GET | `/api/v1/auth/whoami` | any key | System → session; startup scope check | `WSYS-050`, `AUTH-030` |
| GET | `/api/v1/workers` | `wallets:read` | Overview → workers; System → workers tab | `WOVW-040`, `WSYS-001` |
| GET | `/api/v1/audit` | `admin:read` | System → audit tab | `WSYS-040` |
| GET | `/api/v1/config` | `admin:read` | System → config tab; thresholds elsewhere | `WSYS-020` |
| GET | `/metrics` | any key | System → health tab, **link only, never parsed** | `WSYS-062` |

## Public — 3

| Method | Path | Page | Spec |
|---|---|---|---|
| GET | `/healthz` | System → health tab | `WSYS-060` |
| GET | `/readyz` | Overview → readiness card; System → health tab | `WOVW-010` |
| GET | `/openapi.yaml` | System → health tab, link | `WSYS-063` |

## Routes deliberately not consumed

None. Every route above is reachable from the UI (`WG-021`).

## Routes the dashboard needs and the backend does not have

None as specified. Recorded here as the place to note a gap rather than work
around one client-side (`WP-001`).

## Reverse index: change this route → check these specs

| Change | Affected specs |
|---|---|
| Any amount field's format | [`06`](06-conventions.md) `UI-001`–`UI-008`, every page |
| Cursor pagination shape | [`05`](05-data-fetching.md) `DAT-020`–`DAT-026` |
| Error envelope or codes | [`05`](05-data-fetching.md) `DAT-030`–`DAT-036`, [`08`](08-orders.md) `WORD-037`, [`11`](11-withdrawals.md) `WWD-080`–`WWD-087` |
| TOTP transport | [`04`](04-auth-and-session.md) `AUTH-040`–`AUTH-045` |
| Withdrawal status enum | [`11`](11-withdrawals.md) `WWD-010`–`WWD-013`, [`06`](06-conventions.md) `UI-020` |
| `blocked_by` enum | [`11`](11-withdrawals.md) `WWD-063` |
| Order status enum | [`08`](08-orders.md) `WORD-001`–`WORD-003`, [`06`](06-conventions.md) `UI-020` |
| Rate limits | [`05`](05-data-fetching.md) §5.2, every polling tier |
| A new route | This file, plus the page that will own it |
