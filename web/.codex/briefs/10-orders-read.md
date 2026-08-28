---
ROLE: PAGE
TASK-ID: 10-orders-read
GOAL: Build the read-only orders list, order detail, and events tab with no mutation controls.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/03-architecture-and-bff.md
  web/docs/specs/05-data-fetching.md
  web/docs/specs/06-conventions.md
  web/docs/specs/08-orders.md
  web/docs/specs/16-implementation-phases.md
  backend/internal/api/openapi.yaml — sections for: GET /api/v1/orders, GET /api/v1/orders/{id}, GET /api/v1/orders/{id}/events

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/(dash)/orders/
  web/app/(dash)/orders-dashboard.tsx
  PLUS, whenever your change requires it, the build configuration:
    web/package.json, web/tsconfig.json, web/next.config.mjs,
    web/postcss.config.*, web/tailwind.config.*, web/components.json
Everything else belongs to another agent. If you need a change outside this
list, STOP and report it instead of making it.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  WORD-001..WORD-028
  UI-001..UI-008, UI-011..UI-014, UI-020..UI-024, UI-030..UI-035,
  UI-040..UI-044, UI-050..UI-053, UI-076
  DAT-001..DAT-005, DAT-020..DAT-026, DAT-030..DAT-036, DAT-040
  BFF-001, BFF-030, BFF-032

TASK-SPECIFIC CONDITIONS:
  - This is strictly read-only. Do not create, cancel, extend, resolve, show any
    mutation affordance, or consume `GET /orders/funded-terminal`; those belong
    to later tasks.
  - Build `/orders`, `/orders/[id]`, and the events view/tab using existing
    client query, query-key, table, pager, status, amount, timestamp, address,
    and txid components. Add no runtime dependency.
  - All supported list filters must persist in URL search params: status, asset,
    date range, external_ref, consumer, address. Changing a filter resets cursor.
    Use backend default newest-first, do not client-sort. Cursor stays opaque.
  - List columns: id, external_ref, status, asset, expected and received adjacent,
    consumer, address, created, expires. Render `overpaid` only when its string is
    nonzero; do not numeric-coerce or calculate shortfall. A `partial` shortfall
    indication must be explicit backend-state copy, not arithmetic.
  - Detail renders every response field including pretty JSON metadata and full,
    selectable id. Render state machine/current state and possible transitions as
    a static explanation; never derive status from amounts. Render each payment's
    txid, sender, amount, status, block height, chain time, observed time (secondary),
    and backend `is_dust`; flag dust.
  - Assignment window uses `created_at` and `address_released_at`. If null and
    nonterminal, say `still assigned`; if null and terminal, say `no longer
    recorded — attribution is settled`. Never infer an upper bound from current
    address state.
  - Detail tier A (5s) only while status is pending or partial; manual otherwise.
    Events share the detail lifecycle. List is tier B. Keep last good data on errors,
    surface copyable codes, and use the common 1024px read-only card fallback.
  - Dead event may link to `/webhooks`; it must describe IPN redelivery only, not
    an order or payment mutation.

THE SIX INVARIANTS — these override anything you think is a better idea:
  INV-1 NO retry control on withdrawal path. INV-2 money is strings only.
  INV-3 confirmed/pending never merged. INV-4 secrets never reach browser.
  INV-5 no client business logic. INV-6 UTC-day content visibly says UTC.

DESIGN BRIEF:
Financial operations console: dark by default, dense tables, tabular/monospace
financial identifiers, semantic severity palette only, warning/critical with
icons, no decorative motion, visible focus and keyboard reachability. Use cards
only below 1024px for read-only fallback.

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean
  - `npm run lint` clean
  - `npm run build` clean
  - each requirement above has a file:line report
  - all list/detail/events calls are proxy-backed reads; no mutation code exists

YOU MUST NOT:
  - add a runtime dependency; modify `backend/`; implement backend rules;
    write retries/re-sends; commit/push/change branches; resolve ambiguity yourself.

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line
  - anything blocked and why
  - any ambiguity or contradiction
---
