---
ROLE: PAGE
TASK-ID: 09-overview
GOAL: Replace the placeholder with the read-only operational overview page.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/03-architecture-and-bff.md
  web/docs/specs/05-data-fetching.md
  web/docs/specs/06-conventions.md
  web/docs/specs/07-overview-page.md
  web/docs/specs/16-implementation-phases.md
  backend/internal/api/openapi.yaml — sections for: GET /api/v1/stats, GET /api/v1/chain/status, GET /api/v1/chain/quota, GET /api/v1/workers, GET /api/v1/prices, GET /api/v1/chain/params, GET /api/v1/config, GET /api/v1/reports/volume, GET /readyz

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/(dash)/page.tsx
  web/app/(dash)/overview-dashboard.tsx
  web/app/(dash)/alarm-navigation.tsx
  PLUS, whenever your change requires it, the build configuration:
    web/package.json, web/tsconfig.json, web/next.config.mjs,
    web/postcss.config.*, web/tailwind.config.*, web/components.json
  If you change the module format, the compiler target, or the toolchain in
  ANY of those, you MUST bring the others into agreement in the same task and
  prove it with a passing `npm run build`. Changing `"type"` in package.json
  without converting every CommonJS config file is the specific failure this
  clause exists to prevent.
Everything else belongs to another agent. If you need a change outside this
list, STOP and report it instead of making it.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  WOVW-001..WOVW-006, WOVW-010..WOVW-014, WOVW-020..WOVW-023,
  WOVW-030..WOVW-033, WOVW-040..WOVW-044, WOVW-050..WOVW-054,
  WOVW-060..WOVW-062
  UI-010..UI-016, UI-020..UI-023, UI-040..UI-044, UI-050..UI-053, UI-073, UI-076
  DAT-001, DAT-008, DAT-009, DAT-030..DAT-036, DAT-040, BFF-001, BFF-030, BFF-032

TASK-SPECIFIC CONDITIONS:
  - `page.tsx` is a thin server wrapper. Render a client `OverviewDashboard`.
  - Use the existing query client, `paydRequest`, `paydQueryOptions`, query-key
    factory, data components, and tokens. Declare tier B for stats, chain status,
    chain quota, workers, prices, readyz, chain params, and today’s volume report.
    Fetch config once as tier D. No tier A.
  - Reuse the nav's exact stats query (`queryKeys.stats()`) and export only the
    existing alarm-count reader from `alarm-navigation.tsx` if needed; do not
    duplicate its stats shape or make list-probe requests. The page alarm strip
    reads that cached query, not a second request.
  - Preserve money values as backend strings and render them only through
    `<Amount>`. Never perform money arithmetic. UTC visible labels are mandatory
    for quota and today's volume. Render all ordinary timestamps with `<Timestamp>`
    and durations with the existing/native formatter pattern.
  - Safely narrow the open-ended stats model and ignore unknown keys. Do not
    infer business state. Read `funded_terminal_unresolved` directly; never sum
    funded terminal order statuses.
  - Render the documented alarm strip topmost; readiness, chain, quota, prices,
    workers, and a UTC volume summary below. Give each degraded readiness reason
    the documented human wording and destination. Make `needs_operator` loudest,
    a chain reorg warning link to orphaned payments, quota thresholds as specified,
    null worker ticks as `never ticked`, use API `expected_interval_seconds` for
    stall judgement (no hardcoded cadence table), and sticky worker errors explicitly
    labelled `last error (may be resolved)`.
  - Compose readiness figures only as WOVW-012 now defines: from endpoints this
    page already polls. For clock-skew reasons show the prescribed explanation
    without a numeric figure; show unknown codes raw. Use `/reports/volume` for
    current UTC-day volume with start/end timestamps computed from native Date;
    render returned bucket amounts unchanged and show nonzero unpriced count.
    `/readyz` returns its valid degraded `{status, reasons}` body with 503; call
    `paydRequest` using its accepted-status argument `[503]` for that read so the
    UI can render the backend's degradation rather than treating it as a failed load.
  - The required 1024px read-only fallback must be cards, not hidden columns.
    Keep failed query data on screen and surface a copyable error code / stale
    marker through existing primitives or minimal local code. Do not add a custom
    chart, dependency, client state manager, business rule, mutation, or motion.

THE SIX INVARIANTS — these override anything you think is a better idea:

  INV-1  NO RETRY CONTROL ANYWHERE IN THE WITHDRAWAL PATH. No retry, resume,
         re-broadcast, or "try again" button, link, menu item, or automatic
         re-send of a failed mutation. Mutations are configured `retry: false`
         at the query-client level. The proxy never re-sends a POST for any
         reason: timeout, 5xx, 429, connection reset. The backend never retries
         a fund-moving action; client-side retry silently undoes that guarantee
         and pays out twice.
  INV-2  MONEY IS A STRING, START TO FINISH. No Number(), parseFloat, +, -,
         toFixed, toLocaleString, or comparison operator on any amount field —
         including sorting, filtering, and zero-checks. Ever.
  INV-3  `confirmed` AND `pending` BALANCES ARE NEVER MERGED into one figure.
  INV-4  NO PAYD API KEY, TOTP CODE, OR SECRET REACHES THE BROWSER — not in a
         response body, a JS-readable cookie, a URL, localStorage, an error
         message, or the built bundle.
  INV-5  NO BUSINESS LOGIC IN THE CLIENT. Never compute whether an order is
         paid, a withdrawal is permitted, or a balance suffices. Render what
         the API said.
  INV-6  ANYTHING SCOPED TO A UTC DAY IS LABELLED UTC IN VISIBLE TEXT.

DESIGN BRIEF:

This is a financial operations console, not a marketing site. Target: Linear's
density and keyboard discipline, Stripe's clarity about money, a terminal's
honesty about state.

  - Dark mode is the DEFAULT (UI-075). This gets opened at 3am during an incident.
  - Density over whitespace. An operator scanning 200 payments needs rows, not cards.
  - Tabular figures and monospace for every amount, address, txid, and id.
  - Colour carries severity, never identity. Use only neutral, progress, success,
    muted, warning, critical. Warning and critical also carry an icon.
  - `needs_operator` is the single loudest thing in the interface.
  - No decorative motion. No spinner that collapses layout.
  - Empty worklists are SUCCESS, empty searches NEUTRAL, failed loads ERROR while
    retaining last good data.
  - Keyboard reachable, visible focus rings, labelled badges.

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean
  - `npm run lint` clean
  - `npm run build` clean
  - every requirement ID above is satisfied and you can point to where
  - all required reads are proxy-backed with their declared tier and no page code makes a mutation

YOU MUST NOT:
  - add a runtime dependency (the budget is fixed in WST-001)
  - modify anything under `backend/`
  - implement a business rule the backend owns
  - write a retry, backoff, or automatic re-send on any mutation path
  - commit, push, or change git branches
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line where it is satisfied
  - anything you could not do, and why
  - any spec ambiguity or contradiction you hit
---
