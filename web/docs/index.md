# Documentation index — payd admin dashboard (`web/`) v1.0

Entry point for the dashboard specification. `web/` is the operator UI for the
`payd` backend in [`../../backend/`](../../backend). It is a **client**, not a
second implementation: every business rule lives in the backend, and this
document set describes only what the operator sees, clicks, and is prevented
from doing.

**How to use this file:** find the row matching your task, open only that file
and its `Related` links. Requirement IDs are stable (e.g. `WWD-012`) — grep
`web/docs/specs/*.md` for an ID if you know it but not where it lives. Backend
IDs (`API-*`, `ORD-*`, `WDR-*`, `OPS-*`) are referenced but never redefined
here; grep `backend/docs/specs/*.md` for those.

## Spec files

| # | File | Covers | ID prefix | Read this when… |
|---|---|---|---|---|
| 1 | [`specs/01-overview-and-scope.md`](specs/01-overview-and-scope.md) | Purpose, goals, non-goals, the ten pages | `WG-*`, `WNG-*` | You need the big picture, or to check whether something is in scope |
| 2 | [`specs/02-tech-stack.md`](specs/02-tech-stack.md) | Framework, libraries, project layout | `WST-*` | Adding a dependency, deciding where a file goes |
| 3 | [`specs/03-architecture-and-bff.md`](specs/03-architecture-and-bff.md) | The server-side proxy, route handlers, why no browser ever holds an API key | `BFF-*` | Adding any route that talks to payd |
| 4 | [`specs/04-auth-and-session.md`](specs/04-auth-and-session.md) | Dashboard login, session cookie, payd TOTP handling | `AUTH-*` | Touching login, sessions, or any TOTP-gated action |
| 5 | [`specs/05-data-fetching.md`](specs/05-data-fetching.md) | Polling tiers, cursor pagination, the rate-limit budget, error envelope | `DAT-*` | Adding any query, or changing a refresh interval |
| 6 | [`specs/06-conventions.md`](specs/06-conventions.md) | Money, time, status badges, tables, empty/error states, layout, responsive rules | `UI-*` | Rendering any amount, timestamp, or status anywhere |
| 7 | [`specs/07-overview-page.md`](specs/07-overview-page.md) | `/` — health, alarms, chain, quota, workers | `WOVW-*` | Working on the landing page |
| 8 | [`specs/08-orders.md`](specs/08-orders.md) | `/orders` — list, detail, create, cancel, extend, resolve, funded-terminal queue | `WORD-*` | Working on order screens |
| 9 | [`specs/09-payments.md`](specs/09-payments.md) | `/payments` — search, unattributed queue, orphaned queue, attribute | `WPAY-*` | Working on payment screens |
| 10 | [`specs/10-addresses.md`](specs/10-addresses.md) | `/addresses` — pool, balances, resource health, disable, delegate, clear-drift | `WADR-*` | Working on address screens |
| 11 | [`specs/11-withdrawals.md`](specs/11-withdrawals.md) | `/withdrawals` — list, detail, the create wizard, limits, `needs_operator` queue | `WWD-*` | **Read §11.0 first, always.** Any withdrawal UI |
| 12 | [`specs/12-resources-and-energy.md`](specs/12-resources-and-energy.md) | `/resources` — energy provider, purchases, grants, resource wallet, chain params | `WRES-*` | Working on resource screens |
| 13 | [`specs/13-webhooks.md`](specs/13-webhooks.md) | `/webhooks` — consumers, dead letters, replay, test ping | `WIPN-*` | Working on IPN screens |
| 14 | [`specs/14-reports-and-exports.md`](specs/14-reports-and-exports.md) | `/reports` — volume, fees, CSV downloads | `WRPT-*` | Working on reporting |
| 15 | [`specs/15-system-and-audit.md`](specs/15-system-and-audit.md) | `/system` — workers, quota history, effective config, audit log, session info | `WSYS-*` | Working on system screens |
| 16 | [`specs/16-implementation-phases.md`](specs/16-implementation-phases.md) | The four build phases and each phase's gate | `WP1`–`WP4` | Deciding what to build next |
| 17 | [`specs/17-api-coverage-matrix.md`](specs/17-api-coverage-matrix.md) | Every backend route → the page that consumes it | — | Checking nothing is stranded, or finding which page breaks on an API change |

Backend contract: [`../../backend/internal/api/openapi.yaml`](../../backend/internal/api/openapi.yaml)
is the authority. When it and these docs disagree, the OpenAPI document wins and
this set is wrong.

## Non-negotiable invariants

These are not style preferences. Each one exists because violating it loses
money or hides a loss.

| ID | Invariant | Enforced in |
|---|---|---|
| **INV-1** | **No retry control exists anywhere in the withdrawal UI.** No "retry", "resume", "re-broadcast", or "try again" button, link, or automatic refetch on any withdrawal mutation. Moving funds again is a *new* request with a *new* `Idempotency-Key`, made deliberately by a human who read the failure reason | [`11`](specs/11-withdrawals.md) §11.0, backend `WDR-000`/`WDR-000c` |
| **INV-2** | **Money is a string, start to finish.** Never `Number()`, `parseFloat`, `+`, `toFixed`, or arithmetic on any amount. Compare with the backend's own figures; if a number must be derived, the backend already exposes it | [`06`](specs/06-conventions.md) `UI-001` |
| **INV-3** | **`confirmed` and `pending` balances are never merged into one figure.** Pending funds are reorg-reversible and unspendable | [`06`](specs/06-conventions.md) `UI-004`, backend `API-014` |
| **INV-4** | **No payd API key, TOTP code, or IPN secret ever reaches the browser**, in a response body, a cookie readable by JS, a URL, `localStorage`, an error message, or a client-side log | [`03`](specs/03-architecture-and-bff.md) `BFF-002` |
| **INV-5** | **The dashboard never re-implements a backend rule.** No client-side computation of whether an order is paid, a withdrawal is permitted, or a balance suffices. It renders what the API said | [`01`](specs/01-overview-and-scope.md) `WNG-002` |
| **INV-6** | **Anything scoped to a UTC day is labelled UTC.** Daily withdrawal limit, volume report grouping, quota history. Everything else renders in local time | [`06`](specs/06-conventions.md) `UI-010` |

## Glossary

Backend terms (deposit address, order, payment, seen, confirmed, solidified,
IPN, sun, assignment window) are defined in
[`../../backend/docs/specs/01-overview-and-goals.md`](../../backend/docs/specs/01-overview-and-goals.md)
§1.3 and are **not** redefined here. Dashboard-only terms:

| Term | Meaning |
|---|---|
| **BFF** | Backend-for-frontend: the Next.js server layer that holds the payd API key and proxies every call. The browser talks only to it |
| **Operator** | The single human user of this dashboard. There is one account |
| **Session TOTP** | The dashboard login's second factor. Distinct from payd's withdrawal TOTP and never interchangeable with it |
| **payd TOTP** | The code payd requires in `X-TOTP` for fund-moving actions. Entered per action, single-use |
| **Alarm** | A backend condition that always warrants a human: `needs_operator`, unattributed payments, funded-terminal orders, dead IPNs |
| **Worklist** | A page whose purpose is to reach zero rows, not to be browsed |

## See also

- [`../AGENTS.md`](../AGENTS.md) — how a coding agent should work in `web/`
- [`../../AGENTS.md`](../../AGENTS.md) — repo root, both subprojects
- [`../../backend/docs/index.md`](../../backend/docs/index.md) — the backend spec set
