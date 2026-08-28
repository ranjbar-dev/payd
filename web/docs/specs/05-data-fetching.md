# 5. Data fetching, polling, and the rate-limit budget

**Part of:** payd admin dashboard specification v1.0
**ID prefixes in this file:** `DAT-*`
**Related:** [`03-architecture-and-bff.md`](03-architecture-and-bff.md) (the proxy), backend `API-023` (rate limits), `API-025` (cursor pagination), `API-024` (error envelope)

---

## 5.1 The constraint

payd rate-limits **per API key**, in fixed one-minute windows: 100 req/min
generally, **10 req/min on `/api/v1/withdrawals` and its subpaths**.

The dashboard holds exactly one key. So every open tab, every background poll,
and every operator click share one 100/min budget — and the withdrawal screens
share a 10/min budget that is easy to blow with a single 5-second poll.

Exceeding it returns 429 `rate_limited`, and the operator sees an outage on the
screen they need most.

## 5.2 Polling tiers

| Tier | Interval | Applies to | Cost |
|---|---|---|---|
| **A — live** | 5s | An entity actively changing state under the operator's eyes: an order awaiting payment, a withdrawal in a non-terminal state | 12 req/min each |
| **B — operational** | 30s | Lists and dashboards on the visible page | 2 req/min each |
| **C — alarms** | 60s | The four navigation alarm counters, polled on every page | 1 req/min each |
| **D — manual** | never | Reports, exports, audit log, effective config, assets, whoami | 0 |

| ID | Requirement |
|---|---|
| DAT-001 | Every query MUST declare its tier explicitly. A query with no declared tier defaults to **D**, so forgetting to think about it costs nothing rather than costing the budget |
| DAT-002 | Tier A MUST be applied only to a single entity's detail view, and only while that entity is in a non-terminal state. On reaching a terminal state the query MUST drop to manual — a `confirmed` withdrawal polled every 5 seconds forever is pure waste |
| DAT-003 | Polling MUST stop when the tab is hidden (`document.visibilityState`), and MUST refetch once on becoming visible. An operator with six tabs open overnight otherwise consumes the entire budget while looking at none of them |
| DAT-004 | Polling MUST stop when the browser reports offline, and resume on reconnect |
| DAT-005 | Only the visible page's tier-B queries MUST poll. Background pages MUST NOT keep intervals alive |
| DAT-006 | **Withdrawal routes MUST be budgeted against 10 req/min, not 100.** Tier A on `/withdrawals/{id}` MUST therefore use a 10-second interval, not 5. With the list at tier B this totals 8 req/min, leaving headroom for the operator's own actions. A 5-second detail poll (12/min) exceeds the cap by itself |
| DAT-007 | After a 429, every query MUST back off to 60 seconds for two minutes, and the UI MUST show a non-blocking notice explaining that refresh has slowed. Retrying a 429 at the same cadence turns a brief limit into a sustained one |
| DAT-008 | Identical in-flight requests MUST be de-duplicated. Two components needing the alarm counts MUST share one request |
| DAT-009 | **The nav alarm counters MUST come from `GET /stats`, which already carries them.** The list endpoints do NOT expose a total: `OrderList`, `FundedOrderList`, `PaymentList`, `WithdrawalList`, and `DeadIPNPage` return rows plus `next_cursor` and nothing else, so a `limit=1` probe can establish zero versus non-zero but never an exact count. `/stats` gives `needs_operator`, `payments["unattributed"]`, `orphaned_unresolved`, and `ipn_dead` (per consumer, summed) exactly — four counters in one request, on a query the Overview page already makes. See `WOVW-004` for the funded-terminal counter, which `/stats` cannot supply |
| DAT-010 | Total steady-state consumption with one tab open on the busiest page MUST stay under 30 req/min, leaving room for a second tab and for operator actions. Any change that breaks this MUST update the worked example below |

### Worked example (busiest realistic case)

| Source | Endpoints | Interval | req/min |
|---|---|---|---|
| Nav alarm counters | `/stats` — all five counts | 60s | 1 |
| Overview page | `/chain/status`, `/workers`, `/chain/quota`, `/readyz`, `/prices`, `/chain/params`, `/reports/volume` | 30s | 14 |
| Overview `/stats` | shared with the nav counters, not fetched twice (`DAT-008`) | 30s | 2 |
| One withdrawal detail open, in flight | `/withdrawals/{id}` | 10s | 6 |
| **Total** | | | **23** |

Two tabs: 46/min against 100. Withdrawal-scoped: 6/min against 10.

Away from the Overview page the nav costs **one** request per minute for all five
alarm counters, because `/stats` carries them together. The four separate probes
this replaced cost four, and returned no exact figure.

## 5.3 Cursor pagination

Backend `API-025`: `limit` (default 50, max 200) and an opaque `cursor`;
responses carry `next_cursor`.

| ID | Requirement |
|---|---|
| DAT-020 | Pagination MUST be forward-only: "Load more" or "Next", plus a "Back to start". There MUST be no page-number control — the backend has no offset, and synthesising one means fetching every prior page |
| DAT-021 | `cursor` MUST be treated as opaque. It MUST NOT be parsed, decoded, incremented, or persisted across sessions |
| DAT-022 | An empty `next_cursor` MUST end the list. The UI MUST distinguish "no more results" from "no results at all" |
| DAT-023 | Default `limit` MUST be 50. The operator MAY raise it to 200 on list pages; anything larger is an export ([`14`](14-reports-and-exports.md)) |
| DAT-024 | Changing any filter MUST reset the cursor. Carrying a cursor across a filter change returns rows from the previous query's ordering |
| DAT-025 | `GET /wallets/needs-resources` MUST NOT be paginated in the UI. It accepts `limit`/`cursor` for consistency but ignores them and returns the complete set with no `next_cursor` — rendering a "Load more" button there produces an infinite loop |
| DAT-026 | Filter state MUST live in the URL query string, so an operator can send a colleague a link to the exact list they are looking at |

## 5.4 Errors

Backend `API-024`: `{"error": {"code", "message", "details"}}`.

| ID | Requirement |
|---|---|
| DAT-030 | The UI MUST branch on `error.code`, never on `error.message`. Messages are human text and will change |
| DAT-031 | Every error surface MUST show the `code` verbatim somewhere copyable, so an operator can quote it in a bug report |
| DAT-032 | `details` MUST be rendered when present. It carries the actionable part: `totp_consumed`, the conflicting fields of `external_ref_conflict`, `blocked_by` entries |
| DAT-033 | These codes MUST have specific, written copy rather than a generic failure toast: `unauthorized`, `rate_limited`, `insufficient_balance`, `external_ref_conflict`, `idempotency_key_reuse`, `totp_in_body`, `price_stale`, `address_pool_exhausted`, `upstream_unreachable`, `upstream_timeout`. The exact wording lives with each page's spec |
| DAT-034 | A failed mutation MUST NOT be auto-retried by the query client. Mutations MUST be configured with `retry: false` globally, not per call (`BFF-022`) |
| DAT-035 | A failed read MUST leave the last good data visible with a staleness marker, rather than replacing the page with an error. An operator watching a withdrawal does not want the screen to empty because one poll failed |
| DAT-036 | A 401 from the proxy MUST redirect to `/login` preserving the intended path. A 401 from payd (bad key or missing scope) MUST NOT — it is a configuration fault and MUST render the scope banner instead |

## 5.5 Cache invalidation

| ID | Requirement |
|---|---|
| DAT-040 | Query keys MUST be built by a single factory in `lib/query-keys.ts`, keyed by resource and filters. Ad-hoc key strings make invalidation unreliable |
| DAT-041 | A successful mutation MUST invalidate the affected entity and every list that could contain it. Cancelling an order invalidates that order, the order list, the funded-terminal list, and the alarm counters |
| DAT-042 | The UI MUST NOT optimistically update any money-bearing field. An optimistic balance is a wrong balance, and the operator cannot tell which they are looking at |
| DAT-043 | Mutation results MUST be written into the cache from the *response body* where the backend returns the updated entity, avoiding an immediate refetch that costs budget |
| DAT-044 | After creating a withdrawal, its detail query MUST enter tier A immediately, because the interesting states (`awaiting_energy`, `broadcast`) occur within the first 90 seconds |
