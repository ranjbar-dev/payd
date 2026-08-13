# 16. Implementation phases

**Part of:** payd admin dashboard specification v1.0
**ID prefixes in this file:** `WP1`–`WP4`
**Related:** every page spec; backend [`19-implementation-phases.md`](../../../backend/docs/specs/19-implementation-phases.md) for the equivalent on the other side

---

Four phases. Each has a gate: a condition that must hold before the next phase
starts. The ordering is by *what breaks if it is missing*, not by what is
easiest to build.

## WP1 — Foundation and read-only money

**Build:** BFF proxy, login/session, navigation shell with alarm counters,
Overview, Orders (list + detail, no mutations), Payments (search + detail),
Addresses (list + detail, no mutations), Withdrawals (list + detail, **no
create, no resolve**).

Why this order: the dashboard is useful the moment it can answer "what is the
state of things", and every mutation is safer to build against screens that
already render the entity correctly.

| Gate | |
|---|---|
| G1-1 | No API key, TOTP code, or secret appears in any browser-visible artefact: response bodies, cookies, URLs, storage, bundle, or console. Verified by inspection of the built bundle, not by assertion (`INV-4`) |
| G1-2 | Every amount rendered on screen is byte-identical to the API response. Verified by a test that fails if any numeric coercion appears on an amount path (`INV-2`) |
| G1-3 | Confirmed and pending balances never appear merged, on any screen (`INV-3`) |
| G1-4 | Steady-state request rate with one tab open on the busiest page is under 30 req/min, measured against the proxy (`DAT-010`) |
| G1-5 | The proxy retries no `POST` under any condition. Verified by a test that fails the build if a mutation is re-sent on timeout or 5xx (`BFF-020`) |
| G1-6 | Session expiry redirects to login; an expired session cannot reach any payd route through the proxy |

## WP2 — The alarms and the safe mutations

**Build:** the four worklists (funded-terminal orders, unattributed payments,
orphaned payments, `needs_operator` withdrawals — read-only for the last),
order cancel/extend/resolve, payment attribute, address disable, webhooks page
with retry/replay/test.

Why before withdrawals: these are the actions where a mistake costs a decision
record or a duplicate notification, not a payout. They exercise the whole
mutation path — confirm dialogs, cache invalidation, error mapping — at a
survivable cost.

| Gate | |
|---|---|
| G2-1 | Each of the four alarm counters reaches zero on a seeded database and returns to non-zero when its condition is reproduced |
| G2-2 | Force-cancel of a funded order requires a second explicit confirmation and cannot be reached in one click (`WORD-051`) |
| G2-3 | Attributing a payment to an order of a different asset requires an extra confirmation naming both assets (`WPAY-034`) |
| G2-4 | Bulk IPN replay defaults to `dry_run: true` and never loops automatically past 200 events (`WIPN-041`, `WIPN-043`) |
| G2-5 | Every mutation invalidates its entity, every list containing it, and the alarm counters (`DAT-041`) |
| G2-6 | `409 external_ref_conflict` renders requested-versus-stored fields side by side and never presents the stored order as a successful creation (`WORD-037`) |

## WP3 — Withdrawals

**Build:** the create wizard (compose → estimate → confirm), `needs_operator`
resolve, the daily limit meter, address delegate and clear-drift, resources
page.

Everything on this page is gated on `WWD-001`–`WWD-007`.

| Gate | |
|---|---|
| G3-1 | **A repository-wide search finds no retry, resume, re-broadcast, or re-send control on any withdrawal, grant, or delegation path** — including as a disabled control, a commented-out block, or a menu item (`WWD-001`) |
| G3-2 | A submission timeout renders the ambiguous-outcome panel, sends nothing further, and instructs the operator to check the list before acting (`WWD-086`) |
| G3-3 | The estimate step cannot be skipped, and `can_proceed: false` disables submission (`WWD-060`, `WWD-066`) |
| G3-4 | `confirmed_balance_sufficient` and `trx_for_resources_sufficient` render as two separate verdicts with distinct remedies (`WWD-062`) |
| G3-5 | Every `blocked_by` value has specific copy and a specific next step; no raw enum reaches the screen (`WWD-063`) |
| G3-6 | A consumed TOTP code produces the `totp_consumed` message, clears the field, and does not resubmit (`AUTH-043`, `WWD-083`) |
| G3-7 | The `Idempotency-Key` is generated once per wizard completion and never regenerated or reused (`WWD-005`) |
| G3-8 | The resolve dialog states that it records a decision and moves no funds, and requires confirmation that the txid was checked on chain (`WWD-042`, `WWD-043`) |
| G3-9 | The wizard refuses to render below 1024px (`UI-074`) |
| G3-10 | Withdrawal-scoped requests stay under 10 req/min with a detail page open and polling (`DAT-006`) |

## WP4 — Reporting and operations

**Build:** reports (volume, fees), CSV exports, System page (workers, quota,
config, assets, audit, session), staleness markers, dark mode polish,
responsive card layouts.

| Gate | |
|---|---|
| G4-1 | `unpriced_paid_count` is prominent on the volume report, not a footnote (`WRPT-003`) |
| G4-2 | Every UTC-scoped figure is labelled UTC in visible text: daily limit, volume day buckets, quota days (`INV-6`) |
| G4-3 | CSV exports stream through the proxy without buffering and preserve `Content-Disposition` (`BFF-011`) |
| G4-4 | Missing scopes render disabled controls naming the scope, never hidden ones (`AUTH-032`) |
| G4-5 | Every backend route appears in the coverage matrix as consumed or explicitly unconsumed with a reason ([`17`](17-api-coverage-matrix.md)) |
| G4-6 | The Session tab identifies the network, so a testnet deployment is distinguishable from mainnet at a glance (`WSYS-054`) |

## Cross-phase rules

| ID | Requirement |
|---|---|
| WP-001 | No phase MAY ship a control whose backend route does not exist. If a screen needs an endpoint the backend lacks, the endpoint is a backend change, not a client-side workaround (`INV-5`) |
| WP-002 | Every phase MUST leave the coverage matrix accurate. A route consumed in WP2 but recorded as unconsumed makes the matrix worthless |
| WP-003 | The `INV-1`–`INV-6` invariants MUST be checked in every phase's review, not only in the phase that introduced them |
| WP-004 | An API contract change MUST update `backend/internal/api/openapi.yaml`, the derived types, this spec set, and the coverage matrix together (repo root `AGENTS.md`) |
