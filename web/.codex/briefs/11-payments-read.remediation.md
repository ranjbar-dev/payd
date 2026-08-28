ROLE: PAGE
TASK-ID: 11-payments-read
GOAL: Build the read-only payments page — search with filters, results table, and a payment detail drawer — with no worklists and no mutations.

You are working in the repository at C:\Users\root\Desktop\tron-payment-proccesor.
The web app is `web/`. Run every command from `web/`.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/02-tech-stack.md
  web/docs/specs/05-data-fetching.md
  web/docs/specs/06-conventions.md
  web/docs/specs/09-payments.md — sections 9.1, 9.2, 9.3 are yours. 9.4 and 9.5
    (the unattributed and orphaned worklists, and the attribute action) belong to
    task 15-payments-work. DO NOT BUILD THEM.
  backend/internal/api/openapi.yaml — sections for: GET /api/v1/payments,
    GET /api/v1/assets, GET /api/v1/orders/{id}. Read the `Payment` schema in
    full; it is `additionalProperties: false` and it is the contract.

READ ALSO, AS THE PATTERN TO FOLLOW — the orders page is DONE and is the
reference implementation for a filtered list plus detail. Match its structure,
its naming, and its use of the shared components. Do not invent a second way of
doing what it already does:
  web/app/(dash)/orders-dashboard.tsx
  web/app/(dash)/orders/page.tsx
  web/lib/query.ts, web/lib/query-keys.ts, web/lib/payd/browser-client.ts
  web/lib/payd/schemas.ts — `paymentSchema` is already written and complete.
  web/components/data/*, web/components/forms/*

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/(dash)/payments/page.tsx
  web/app/(dash)/payments-dashboard.tsx
  web/app/(dash)/payment-drawer.tsx
  web/lib/query-keys.ts             — the factory already carries generic
                                      `payments` keys. Extend it only if the
                                      filtered search needs a key it does not
                                      have. Do not change or remove an existing
                                      key; other pages use them.
  web/components/data/*.tsx         — only if an existing shared component needs a
                                      genuinely additive change. Prefer using them
                                      as they are. Never change an existing
                                      component's default behaviour.
  PLUS, whenever your change requires it, the build configuration:
    web/package.json, web/tsconfig.json, web/next.config.mjs,
    web/postcss.config.mjs, web/tailwind.config.ts, web/components.json
  If you change the module format, the compiler target, or the toolchain in ANY
  of those, you MUST bring the others into agreement in the same task and prove
  it with a passing `npm run build`. The project is `"type": "module"`; every
  config file is already ESM. Do not convert one back.
Everything else belongs to another agent. If you need a change outside this
list, STOP and report it instead of making it.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  WPAY-001, WPAY-002, WPAY-003, WPAY-004, WPAY-005, WPAY-006, WPAY-007,
  WPAY-008, WPAY-009, WPAY-010
  WPAY-020, WPAY-021, WPAY-022, WPAY-023, WPAY-023a, WPAY-023b
  DAT-026 — filter state persisted in the URL query string. This one is owed by
    every page that owns a filtered list, and it is yours here.

NOTES ON THREE REQUIREMENTS THAT HAVE ALREADY COST A HALT:

  WPAY-005 — `Payment.withdrawal_id` now exists. It is a nullable string. Render
  the link from that field and nothing else. NULL IS NOT AN ERROR AND NOT AN
  EMPTY CELL: it means either an inbound payment or an outbound transfer this
  service did not broadcast, and the second one is exactly what an operator is
  looking for. Say "not a service withdrawal" on an outbound row. Never infer the
  link from the txid, the address, or anything else.

  WPAY-023 / 023a / 023b — `unattributed_reason` is a backend decision recorded
  at match time. Read it. Never recompute it by comparing the payment against the
  address's current order — that state has since changed and the answer would
  differ from the one actually made (INV-5). Null on an unattributed payment
  renders as "reason not recorded", not as one of the three values and not as an
  error.

  WPAY-022 — the assignment window is the order's `created_at` as the lower
  boundary and `address_released_at` as the upper one. There is no
  `address_assigned_at` field and you must not invent one. The orders page
  already renders exactly this window — see the "Assignment window" field in
  `web/app/(dash)/orders-dashboard.tsx` — and yours MUST agree with it. A null
  `address_released_at` has two meanings, split in WORD-023a: still held, or no
  longer recorded. Do not collapse them, and do not treat null as "now".

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
         message, or the built bundle. No `NEXT_PUBLIC_` variable exists in this
         project and you must not add one (WST-020). A value the browser
         legitimately needs is read server-side and passed down from a server
         component — see `TRONSCAN_BASE_URL` in `app/providers.tsx`.
  INV-5  NO BUSINESS LOGIC IN THE CLIENT. Never compute whether an order is
         paid, a withdrawal is permitted, or a balance suffices. Render what
         the API said.
  INV-6  ANYTHING SCOPED TO A UTC DAY IS LABELLED UTC IN VISIBLE TEXT. WPAY-009
         requires the resolved UTC range to be shown for a local-time date
         filter.

DESIGN BRIEF — this is a financial operations console, not a marketing site.
Target: Linear's density and keyboard discipline, Stripe's clarity about money,
a terminal's honesty about state.
  - Dark mode is the DEFAULT (UI-075). This gets opened at 3am during an
    incident.
  - Density over whitespace. An operator scanning 200 payments needs rows, not
    cards. Cards are the <1024px fallback only (UI-073) and are NOT your task.
  - Tabular figures and monospace for every amount, address, txid, and id.
    Columns of numbers align on the decimal point.
  - Colour carries severity, never identity. The six-level vocabulary in UI-020
    is the WHOLE palette: neutral, progress, success, muted, warning, critical.
    Warning and critical also carry an icon — colour is never the only signal
    (UI-021).
  - `needs_operator` is the single loudest thing in the entire interface.
    Visually distinct from every other warning, everywhere it appears.
  - No decorative motion. Transitions show causality and nothing else. No
    shimmer outlasting the request, no spinner that collapses layout (UI-044).
  - Empty states are three different things and must look different: an empty
    worklist is SUCCESS, an empty search is NEUTRAL, a failed load is an ERROR
    that keeps the last good data visible (UI-050, UI-051).
  - Keyboard reachable, visible focus rings, labelled badges (UI-076).

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean (NOT `npx tsc` — it resolves to an
    older global TypeScript here and reports three false tsconfig errors)
  - `npm run lint` clean
  - `npm run build` succeeds
  - every requirement ID above is satisfied and you can point to where
  - the page is reachable at `/payments` and the drawer opens from a result row
  - a pasted txid, a TRON address, and a ULID each route to the right filter
    without the operator choosing a field first (WPAY-002)
  - filters survive a page reload, because they live in the URL (DAT-026)

YOU MUST NOT:
  - add a runtime dependency (the budget is fixed in WST-001; the installed set
    is @tanstack/react-query, lucide-react, next, react, react-dom,
    react-hook-form, zod, and nothing else)
  - build the unattributed worklist, the orphaned worklist, or the attribute
    action — those are task 15
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

═══════════════════════════════════════════════════════════════════════
REMEDIATION — read this section last; it overrides nothing above, it adds one
thing that was genuinely impossible when you first ran.
═══════════════════════════════════════════════════════════════════════

YOUR PREVIOUS RUN IS ALREADY ON DISK AND IS ACCEPTED. Do not rewrite it, do not
restructure it, do not "improve" it. `app/(dash)/payments/page.tsx`,
`app/(dash)/payments-dashboard.tsx` and `app/(dash)/payment-drawer.tsx` exist and
pass tsc, lint, and build. Every other requirement is satisfied. Touch as little
as you can.

FAILURES:

  WPAY-021 — the drawer renders "Raw amount: Not supplied by payd's Payment
  contract." at `app/(dash)/payment-drawer.tsx:77`. You were right that the field
  did not exist. It exists now: `Payment.amount_raw`, a required non-null string,
  the transfer in the asset's integer base units exactly as the chain recorded
  it. It is in `backend/internal/api/openapi.yaml` and in the `paymentSchema` in
  `web/lib/payd/schemas.ts` — both already updated, do not change either.

  Render it in the drawer alongside the formatted amount. Both figures, labelled
  so an operator can tell which is which. `amount` is `amount_raw` divided by the
  asset's configured decimals, so the two disagree if that configuration is wrong
  or changes — which is the whole reason WPAY-021 asks for both, and why you must
  NOT compute either one from the other. INV-2 applies with full force: it is a
  string, print it as a string, no Number(), no arithmetic, no comparison.

THAT IS THE ONLY CHANGE. Re-run `./node_modules/.bin/tsc --noEmit`,
`npm run lint` and `npm run build`, then report the one file:line where WPAY-021
is now satisfied.
