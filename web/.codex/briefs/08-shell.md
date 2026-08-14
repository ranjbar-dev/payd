---
ROLE: PAGE
TASK-ID: 08-shell
GOAL: Build the protected fixed dashboard shell with permanent navigation, four live alarm counters, and a visible UTC clock.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/03-architecture-and-bff.md
  web/docs/specs/04-auth-and-session.md
  web/docs/specs/05-data-fetching.md
  web/docs/specs/06-conventions.md
  web/docs/specs/07-overview-page.md
  web/docs/specs/16-implementation-phases.md
  backend/internal/api/openapi.yaml — sections for: GET /api/v1/stats

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/(dash)/layout.tsx
  web/app/(dash)/nav-shell.tsx
  web/app/(dash)/alarm-navigation.tsx
  web/app/(dash)/scope-banner.tsx
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
  UI-015, UI-070, UI-071, UI-072, UI-075, UI-076
  WOVW-001, WOVW-002, WOVW-003, WOVW-004, WOVW-004a, WOVW-005, WOVW-006, WOVW-061
  DAT-001, DAT-008, DAT-009, DAT-030, DAT-035, DAT-040
  AUTH-026, BFF-030, BFF-031
  WG-020, WG-022

TASK-SPECIFIC CONDITIONS:
  - Keep the existing server-side session guard and ScopeBanner. The dashboard
    shell itself must be server-rendered; only the live navigation/alarm
    component is client-side.
  - Navigation must permanently link to every dashboard page: Overview, Orders,
    Payments, Addresses, Withdrawals, Resources, Webhooks, Reports, System.
    Links to later tasks may target their documented routes, but no page may be
    reachable only by manually typing a URL.
  - Use one `GET /stats` TanStack query with `queryKeys.stats()` and declared
    tier C polling. Do not call list endpoints for counters. The later Overview
    task will use the same key at tier B, so it must share this cached result.
  - `OperationalStats` is intentionally open-ended. Safely narrow only the
    documented count fields. Read `needs_operator`, `payments.unattributed`,
    `orphaned_unresolved`, `funded_terminal_unresolved`, and sum numeric values
    in `ipn_dead`. Unknown keys must be ignored. These are backend-supplied
    counts, not money; never derive funded-terminal from order-status buckets.
  - Show exactly four permanent counters: needs_operator (critical and loudest),
    unattributed payments (combined unattributed + orphaned, with an accessible
    hover/focus breakdown), funded-terminal, and dead IPNs. Every counter is a
    keyboard-accessible link to its documented worklist/filter and zero remains
    visibly rendered as quiet `0`.
  - Display a continuously updating UTC clock in the nav footer, visibly
    labelled `UTC`. Use native Date/Intl only. No new runtime dependencies,
    bespoke state manager, business logic, or animations.
  - Reuse existing `AlarmCounter`, query utilities, query keys, design tokens,
    and Next `Link`; do not modify them outside this scope.

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

  - Dark mode is the DEFAULT (UI-075). This gets opened at 3am during an
    incident.
  - Density over whitespace. An operator scanning 200 payments needs rows, not
    cards. Cards are the <1024px fallback only (UI-073).
  - Tabular figures and monospace for every amount, address, txid, and id.
    Columns of numbers align on the decimal point.
  - Colour carries severity, never identity. The six-level vocabulary in UI-020
    is the WHOLE palette: neutral, progress, success, muted, warning, critical.
    Warning and critical also carry an icon — colour is never the only signal
    (UI-021).
  - `needs_operator` is the single loudest thing in the entire interface. It
    means money is in an unknown state. Visually distinct from every other
    warning, everywhere it appears (UI-071, WWD-011).
  - No decorative motion. Transitions show causality — a row entering, a state
    changing — and nothing else. No shimmer outlasting the request, no spinner
    that collapses layout (UI-044).
  - Every destructive or fund-moving confirmation reads its text from the API
    response, not from the form inputs (UI-060).
  - Empty states are three different things and must look different: an empty
    worklist is SUCCESS, an empty search is NEUTRAL, a failed load is an ERROR
    that keeps the last good data visible (UI-050, UI-051).
  - Keyboard reachable, visible focus rings, labelled badges (UI-076).

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean
  - `npm run lint` clean
  - `npm run build` clean
  - every requirement ID above is satisfied and you can point to where
  - the shell, all navigation links, four stats-backed counters, and the UTC
    clock work without touching page/task-owned files

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
