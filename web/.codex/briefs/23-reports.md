ROLE: PAGE
TASK-ID: 23-reports
GOAL: Build the /reports page (volume report, fee report, CSV export dialogs) and wire export entry points into /orders and /withdrawals.

You are working in the repository at C:\Users\root\Desktop\Projects\github\tron-payment-proccesor, on branch web-autopilot.

READ FIRST, FULLY:
  web/docs/specs/14-reports-and-exports.md — the whole file, it is short
  web/docs/specs/06-conventions.md — UI-001 (amounts as returned), UI-010/UI-011 (UTC
    labeling), UI-064 (no "try again" after an unknown-outcome mutation)
  web/docs/specs/04-auth-and-session.md — AUTH-032 (missing scope disables, never hides)
  web/docs/specs/03-architecture-and-bff.md — BFF-011 (proxy streams CSV, does not
    buffer), BFF-021 (a GET is never retried on an HTTP status, only optionally once on
    a connection-level failure — this dashboard's proxy already does zero retries,
    which is a compliant subset)
  backend/internal/api/openapi.yaml — sections for: `/reports/volume`, `/reports/fees`,
    `/export/orders.csv`, `/export/withdrawals.csv`. The volume-report bucket schema
    was tightened today (2026-08-21) by the orchestrator to match the real handler
    exactly — trust it, it is no longer `additionalProperties: true`.
  backend/internal/api/reports.go and backend/internal/store/reports.go — read these
    directly rather than only the schema. In particular: `volume` in each bucket is a
    map of asset symbol to an ALREADY-FORMATTED decimal string (paid/confirmed orders
    only); `usd_total` is an exact string sum of immutable per-order price snapshots
    and excludes any order counted in `unpriced_paid_count`; `energy_by_source_trx`
    and `bandwidth_by_source_trx` are maps with fixed keys already present at zero
    (`rented`/`burned`/`self_delegated` for energy, `existing`/`delegated`/`topup` for
    bandwidth) — do not treat a present-but-zero key as "no data".

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/(dash)/reports/                    (new route: page.tsx and friends)
  web/app/(dash)/reports-*.tsx                (new component files, if you prefer
                                               colocating outside the route folder,
                                               matching this repo's existing convention
                                               of `<feature>-dashboard.tsx` etc.)
  web/app/(dash)/export-dialog.tsx            (new, shared by all three export entry
                                               points: orders, withdrawals, and the
                                               report page itself)
  web/app/(dash)/orders-dashboard.tsx         (add an "Export CSV" entry point only —
                                               do not touch its existing filters,
                                               table, or mutation logic)
  web/app/(dash)/withdrawals-dashboard.tsx    (same: add an "Export CSV" entry point
                                               only)
  web/app/(dash)/resource-purchases.tsx or web/app/(dash)/resources-dashboard.tsx
                                               (add a link to the fee report per
                                               WRES-052/WRPT-025 — one line, do not
                                               touch anything else in this file)
  web/lib/payd/schemas.ts, web/lib/payd/types.ts
                                               (add volume-report, fee-report response
                                               types/schemas only)
  web/lib/query-keys.ts                       (add report query keys only, following
                                               the existing factory shape)
  web/app/providers.tsx, web/app/(dash)/layout.tsx
                                               (add a scopes context — see below)
Everything else belongs to another agent. If you need a change outside this list,
STOP and report it instead of making it.

WHY A SCOPES CONTEXT: AUTH-032 requires the export controls to render DISABLED and
NAME the missing scope when the operator's payd key lacks it (orders export needs
`orders:read`, withdrawals export needs `withdrawals:read`), never hidden. The only
scope data that exists today is server-side (`lib/session.ts`'s `getSessionWhoami`,
already read once in `app/(dash)/layout.tsx` to build the page-level `ScopeBanner`).
No client-readable scopes exist yet. Add a `ScopesContext` in `app/providers.tsx`
following the EXACT SAME PATTERN as `TronscanContext` in the same file (a plain React
context fed from a server component, never a `NEXT_PUBLIC_` variable, never fetched by
the client) — a `useScopes(): string[]` hook, provided a value from
`app/(dash)/layout.tsx` using the same `getSessionWhoami(session.id)` call already
made there for `ScopeBanner`. This is additive: do not remove or restructure
`ScopeBanner` or its existing coarse page-level warning; the export controls need
their OWN per-control disabled state, which the banner does not provide.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  WRPT-001 through WRPT-009 (volume report)
  WRPT-020 through WRPT-025 (fee report)
  WRPT-030 through WRPT-037 (CSV exports)
  WRPT-040 through WRPT-042 (deliberately absent — verify you have NOT built these)
  UI-001, UI-010, UI-011 (money as returned, UTC labeling)
  AUTH-032 (scope-gated disabled controls)

THE SIX INVARIANTS — these override anything you think is a better idea:

  INV-1  NO RETRY CONTROL ANYWHERE. WRPT-034 is this invariant applied to exports
         specifically: an export is never auto-retried on failure. If a stream fails
         partway, the operator starts a new export deliberately; nothing in your code
         re-issues the request on its own.
  INV-2  MONEY IS A STRING, START TO FINISH. No Number(), parseFloat, +, -, toFixed,
         toLocaleString, or comparison operator on any amount, volume, or fee field.
         `order_count`, `paid_count`, and `unpriced_paid_count` are plain integers, not
         money — they may be rendered directly, but do not add or derive new figures
         from them (WRPT-041 forbids client-side aggregation across report calls; the
         same restraint applies within a single response — render the buckets, do not
         sum a grand total across them).
  INV-3  `confirmed` AND `pending` BALANCES ARE NEVER MERGED. Not directly relevant to
         this page's data, but if any component you touch renders a balance, the rule
         still applies.
  INV-4  NO PAYD API KEY, TOTP CODE, OR SECRET REACHES THE BROWSER.
  INV-5  NO BUSINESS LOGIC IN THE CLIENT. Do not compute whether `unpriced_paid_count`
         is "acceptable", do not classify a report as complete or incomplete — render
         what the API said, prominently, per WRPT-003.
  INV-6  ANYTHING SCOPED TO A UTC DAY IS LABELLED UTC IN VISIBLE TEXT. The volume
         report's day grouping, and any date-range input, must say UTC in the text an
         operator reads, not only in a tooltip.

EXISTING CONVENTION TO REUSE, NOT REINVENT: `orders-dashboard.tsx` already has a
`created_from`/`created_to` UTC date-range filter — read it (`TableFilters`, the
`dateValue`/`toSeconds` helpers, and the two `<input type="date">` fields labelled
"Created from (UTC)" / "Created to (UTC)"). `type="date"` inputs carry no timezone of
their own; this codebase's established pattern is to treat the entered value as a UTC
calendar day and say so in the label, rather than building a local-time-to-UTC
conversion layer. WRPT-006 reads as if it wants local-time entry with a resolved UTC
range shown — but this repo has already shipped, gated (WP1), and repeated the
simpler UTC-label convention twice with no complaint. Use that SAME convention for the
report's date range, for consistency with the rest of the app. If you believe WRPT-006
truly requires a local-time picker and the existing convention is itself wrong, DO NOT
build a second, different date-input pattern — stop and report the conflict instead.

CSV EXPORT DIALOG (shared by all three entry points):
  - Row cap input, default 10000, client-side validated to 1–100000 before the request
    is even sent (WRPT-031) — matching, not duplicating, the backend's own limits.
  - States plainly which filters are currently applied and the row cap, so a capped
    export is never mistaken for a complete one (WRPT-035). For the orders/withdrawals
    entry points this means reading the CURRENT list filters from the URL query string
    the way the underlying dashboard already does, and passing them straight through
    to the export request — do not let the export dialog carry its own separate filter
    state that could drift from what the operator is looking at (WRPT-030).
  - Pending/streaming state that does not block the rest of the page (WRPT-033) — the
    export is a plain navigation-triggering GET (e.g. an anchor to
    `/api/payd/export/orders.csv?...` with the query string built from the current
    filters) rather than a fetch-into-memory-then-save; a direct link lets the browser
    handle the download and the streaming response natively, which is both the
    simplest implementation and the one least likely to buffer the whole file in JS
    memory before saving it. If you choose instead to fetch and construct a Blob,
    justify why in your report — the direct-link approach is preferred and avoids
    re-implementing what the browser's own download handling already does correctly.
  - Disabled with the missing scope named when `useScopes()` does not include the
    required scope for that export (AUTH-032).
  - No auto-retry on any failure path (WRPT-034, INV-1).

THE PROXY TIMEOUT FOR EXPORT ROUTES was widened today (2026-08-21) by the orchestrator
in `web/app/api/payd/[...path]/route.ts` — GET requests under `/export/` now get a
120-second ceiling instead of the default 15s, because `AbortSignal.timeout` aborts an
in-progress stream, not just connection setup, and a 100,000-row export can run past
15 seconds. You do not need to touch this file; it is listed here so you know the
constraint already accounts for a large export and you do not need to build your own
client-side timeout workaround.

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean
  - `npm run build` clean, `/reports` in the route table
  - `npm test` still 4/4 (you are not expected to add new tests for this task; the
    existing G1-2/G1-5/G1-6 suite must keep passing because none of your code should
    trip the money-coercion or retry detectors)
  - every requirement ID above is satisfied and you can point to where
  - the mechanical scans below return nothing unexpected:
      grep -rniE "retry|resume|re-?broadcast|try ?again|resend|re-?send" on the files
        you touched — every hit must be explainable (e.g. WIPN-001's existing IPN
        retry control elsewhere in the codebase, not anything you wrote)
      grep -rnE "Number\(|parseFloat|parseInt|toFixed|toLocaleString" on the files you
        touched — no hit on a money, volume, or fee field
      grep -rn "NEXT_PUBLIC_" on the files you touched — none

YOU MUST NOT:
  - add a runtime dependency (WST-001's budget is fixed; this task needs none)
  - modify anything under `backend/`
  - implement WRPT-040/041/042 (no scheduler, no client-side period comparison, no
    export beyond the two CSV endpoints that exist)
  - build a second date-input convention alongside the existing UTC-labelled one
  - fetch-and-buffer a CSV export in JS when a direct streaming link will do
  - commit, push, or change git branches
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line where it is satisfied
  - anything you could not do, and why
  - any spec ambiguity or contradiction you hit (in particular, say plainly whether you
    followed the existing UTC-label date convention or built something else, and why)
