# 14. Reports and exports (`/reports`)

**Part of:** payd admin dashboard specification v1.0
**ID prefixes in this file:** `WRPT-*`
**Consumes:** `GET /reports/volume`, `GET /reports/fees`, `GET /export/orders.csv`, `GET /export/withdrawals.csv`
**Related:** backend `API-044`, `API-045`, `API-046`

---

## 14.1 Volume report

| ID | Requirement |
|---|---|
| WRPT-001 | The report MUST require an inclusive Unix `from`/`to` range and a `group_by` of `day`, `asset`, or `consumer` (backend `API-044`) |
| WRPT-002 | Columns: order count, paid-or-confirmed count, actual received volume per asset, exact snapshotted USD total, and `unpriced_paid_count` |
| WRPT-003 | **`unpriced_paid_count` MUST be displayed prominently, not as a footnote.** Backend `API-044` refuses to assign a guessed historical USD value to orders without an immutable price snapshot, so the USD total is exact but incomplete, and the count is what says by how much |
| WRPT-004 | The USD total MUST be labelled as the sum of per-order price snapshots taken at creation, not a revaluation at today's price |
| WRPT-005 | When grouping by day, each bucket MUST be labelled **UTC** (`UI-010`, backend `DB-002a`) |
| WRPT-006 | Date inputs MUST be entered in local time with the resolved UTC range displayed, so the operator can see that "this month" is a UTC month |
| WRPT-007 | The report MUST render as a table. No chart in v1 (`WST-*` dependency budget) |
| WRPT-008 | Volume figures MUST be rendered as returned (`UI-001`). No client-side totals, averages, or percentages |
| WRPT-009 | Tier D (manual). A report is run, not watched |

## 14.2 Fee report

| ID | Requirement |
|---|---|
| WRPT-020 | The report MUST require an inclusive Unix `from`/`to` range over withdrawal `created_at` (backend `API-045`) |
| WRPT-021 | It MUST render exact TRX totals by energy source, by bandwidth source, and provider-attempt rental spend |
| WRPT-022 | The energy-source breakdown MUST be the headline. Backend `API-045` exists to make resource-strategy comparisons possible, and `OPS-004` names the burn-versus-rent split as the signal that a provider is silently failing |
| WRPT-023 | Provider rental spend MUST be labelled as covering **attempts**, including purchases that never resulted in delegation. Money spent on failed rentals is still spent |
| WRPT-024 | The report MUST state that it uses the same energy total calculation as the operational metrics, so a figure here and a figure in Prometheus agree |
| WRPT-025 | The fee report MUST be linked from the resources page (`WRES-052`) |

## 14.3 CSV exports

| ID | Requirement |
|---|---|
| WRPT-030 | Exports MUST reuse the JSON list filters, so an export always corresponds to a list view the operator can see on screen (backend `API-046`) |
| WRPT-031 | The export dialog MUST default the row cap to 10,000 and MUST reject caps outside 1–100,000 in the UI, matching the backend rather than letting it 400 |
| WRPT-032 | The proxy MUST stream the response and preserve `Content-Disposition` (`BFF-011`). The backend streams deliberately to avoid materialising all rows |
| WRPT-033 | The UI MUST show a pending state while streaming and MUST NOT block the page. A 100,000-row export takes time |
| WRPT-034 | An export MUST NOT be auto-retried on failure. It is a large, expensive request and a silent second attempt doubles the load (`BFF-021`) |
| WRPT-035 | The dialog MUST state which filters are applied and the row cap, so a truncated export is not mistaken for a complete one |
| WRPT-036 | Export MUST be reachable from the orders list and the withdrawals list with the current filters pre-applied, as well as from this page |
| WRPT-037 | Orders export requires `orders:read`; withdrawals export requires `withdrawals:read`. A missing scope MUST disable the control with the scope named (`AUTH-032`) |

## 14.4 Deliberately absent

| ID | Requirement |
|---|---|
| WRPT-040 | There MUST be no scheduled or emailed reports. No scheduler exists in this stack (`WNG-004`) |
| WRPT-041 | There MUST be no client-side aggregation across report calls — no "compare two periods" that subtracts one response from another. That is arithmetic on money (`UI-001`) |
| WRPT-042 | There MUST be no export of payments, addresses, or audit records. The backend exposes exactly two CSV endpoints; anything else is a database query |
