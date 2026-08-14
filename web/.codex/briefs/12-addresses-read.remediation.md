ROLE: PAGE
TASK-ID: 12-addresses-read
GOAL: Build the read-only address pool — list, address detail, needs-resources worklist, and the with-balance view — with no mutations of any kind.

You are working in the repository at C:\Users\root\Desktop\tron-payment-proccesor.
The web app is `web/`. Run every command from `web/`.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/02-tech-stack.md
  web/docs/specs/05-data-fetching.md
  web/docs/specs/06-conventions.md
  web/docs/specs/10-addresses.md — sections 10.1, 10.2, 10.3, 10.4, 10.5 and
    10.8 are yours. §10.6 (delegate) and §10.7 (disable) are NOT: they are tasks
    20-addr-totp and 16-addresses-dis. Read §10.3 in full anyway — the drift
    DISPLAY (WADR-020, WADR-021) is yours; only the clear-drift ACTION
    (WADR-022..025) is not.
  backend/internal/api/openapi.yaml — sections for: GET /api/v1/wallets,
    GET /api/v1/wallets/{address}, GET /api/v1/wallets/with-balance,
    GET /api/v1/wallets/needs-resources, GET /api/v1/config, GET /api/v1/assets.
    Read the `Wallet` schemas in full; they are the contract.

READ ALSO, AS THE PATTERN TO FOLLOW — orders and payments are DONE and are the
reference implementations for a filtered list, a detail view, and a drawer.
Match their structure, naming, and use of the shared components. Do not invent a
second way of doing what they already do:
  web/app/(dash)/orders-dashboard.tsx
  web/app/(dash)/payments-dashboard.tsx
  web/app/(dash)/payment-drawer.tsx
  web/lib/query.ts, web/lib/query-keys.ts, web/lib/payd/browser-client.ts
  web/lib/payd/schemas.ts — the wallet schemas are already written.
  web/components/data/*, web/components/forms/*

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/(dash)/addresses/page.tsx
  web/app/(dash)/addresses/[address]/page.tsx
  web/app/(dash)/addresses/needs-resources/page.tsx
  web/app/(dash)/addresses-dashboard.tsx
  web/app/(dash)/address-detail.tsx
  web/lib/query-keys.ts              — ADD wallet keys if the factory lacks
                                       them. Do not change or remove an existing
                                       key; other pages use them.
  web/components/data/*.tsx          — only if an existing shared component needs
                                       a genuinely additive change. Prefer using
                                       them as they are. Never change an existing
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

NOTE ON RUNNING THE BUILD: `web/.env` exists with three required variables
present but EMPTY, and Next lets those empty values beat process-supplied ones.
Move `.env` aside, build with inline values, and MOVE IT BACK. Do not fill it in
and do not commit anything to it — those are the operator's real credential
slots.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  WADR-001..WADR-009   (pool list)
  WADR-020, WADR-021   (drift display only — not the clear-drift action)
  WADR-030..WADR-037   (address detail)
  WADR-040..WADR-045   (needs-resources view)
  WADR-070, WADR-071, WADR-072   (with-balance view)
  UI-004   — confirmed and pending are two labelled figures, NEVER merged into
             one. This is deferred debt the components task could not reach from
             its own file scope, and it lands on you here.
  DAT-026  — filter state persisted in the URL query string.

  WADR-046 is NOT yours — the delegate action on the needs-resources rows belongs
  to 20-addr-totp. Build the worklist so a row action can be dropped in later;
  do not build the action, and do not leave a disabled or placeholder button
  where one will go.

NOTES ON REQUIREMENTS THAT HAVE ALREADY COST A HALT OR A BUG:

  UI-004 / WADR-002 — INV-3 is not a style rule. Confirmed and pending are
  separate columns per asset, separately labelled, never summed, never shown as
  one "balance". An operator who reads a merged figure as spendable will start a
  withdrawal the backend then refuses.

  WADR-033 — `can_withdraw` is PER ASSET. One verdict for the whole address is
  the exact v1.1 bug this requirement exists to prevent: it reported
  `can_withdraw: true` for an address that had no bandwidth. Render what the API
  returns per asset and never reduce it to a single indicator.

  WADR-041 / WADR-043 / WADR-044 — `estimated_rent_trx` is a KNOWN BACKEND GAP:
  it is not computed on this endpoint today, so it is always absent and
  "provider unavailable" is what renders. That is correct and expected — do NOT
  report it as a contract defect, do NOT substitute the burn figure, and do NOT
  compute an estimate client-side. Build the display so it appears the moment the
  field does. An omitted `estimated_burn_trx` is a different thing and renders as
  "chain parameters not yet read".

  WADR-035 — a missing `checked_at` renders as "never polled", and a stale one on
  a low-balance address is EXPECTED, not a fault: the backend polls on a tiered
  cadence, minutes for high-balance addresses and six hours for the rest. Say so
  rather than flagging it as an alarm.

  WADR-008 — pool health thresholds (`wallet.pool_min_free`,
  `wallet.pool_max_size`) come from `GET /config` and from nowhere else.
  WSYS-020a forbids hardcoding a fallback for any operator threshold.

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
         including sorting, filtering, and zero-checks. Ever. "Has a balance" is
         a filter the API answers, not a client-side `> 0`.
  INV-3  `confirmed` AND `pending` BALANCES ARE NEVER MERGED into one figure.
  INV-4  NO PAYD API KEY, TOTP CODE, OR SECRET REACHES THE BROWSER — not in a
         response body, a JS-readable cookie, a URL, localStorage, an error
         message, or the built bundle. No `NEXT_PUBLIC_` variable exists in this
         project and you must not add one (WST-020). A value the browser
         legitimately needs is read server-side and passed down from a server
         component — see `TRONSCAN_BASE_URL` in `app/providers.tsx`.
  INV-5  NO BUSINESS LOGIC IN THE CLIENT. Never compute whether an order is
         paid, a withdrawal is permitted, or a balance suffices. `can_withdraw`,
         `sufficient`, `blocked_by`, `needs_resources` and `drift_detected` are
         all backend verdicts. Render them; never derive them.
  INV-6  ANYTHING SCOPED TO A UTC DAY IS LABELLED UTC IN VISIBLE TEXT.

DESIGN BRIEF — this is a financial operations console, not a marketing site.
Target: Linear's density and keyboard discipline, Stripe's clarity about money,
a terminal's honesty about state.
  - Dark mode is the DEFAULT (UI-075). This gets opened at 3am during an
    incident.
  - Density over whitespace. An operator scanning the pool needs rows, not
    cards. Cards are the <1024px fallback only (UI-073) and are NOT your task.
  - Tabular figures and monospace for every amount, address, txid, and id.
    Columns of numbers align on the decimal point.
  - Colour carries severity, never identity. The six-level vocabulary in UI-020
    is the WHOLE palette: neutral, progress, success, muted, warning, critical.
    Warning and critical also carry an icon — colour is never the only signal
    (UI-021). `drift_detected` is CRITICAL (WADR-020): the ledger and the chain
    disagree about how much money is at that address.
  - No decorative motion. Transitions show causality and nothing else. No
    shimmer outlasting the request, no spinner that collapses layout (UI-044).
  - Empty states are three different things and must look different: an empty
    worklist is SUCCESS — an empty needs-resources view means every address can
    move its funds — an empty search is NEUTRAL, and a failed load is an ERROR
    that keeps the last good data visible (UI-050, UI-051).
  - Keyboard reachable, visible focus rings, labelled badges (UI-076).

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean (NOT `npx tsc` — it resolves to an
    older global TypeScript here and reports three false tsconfig errors)
  - `npm run lint` clean
  - `npm run build` succeeds and `.env` is back where it was
  - every requirement ID above is satisfied and you can point to where
  - `/addresses`, `/addresses/[address]` and `/addresses/needs-resources` all
    render, and the payments and orders pages' existing address links resolve to
    your detail route
  - filters survive a page reload, because they live in the URL (DAT-026)

YOU MUST NOT:
  - add a runtime dependency (the budget is fixed in WST-001; the installed set
    is @tanstack/react-query, lucide-react, next, react, react-dom,
    react-hook-form, zod, and nothing else)
  - build the disable, delegate, or clear-drift actions — those are tasks 16 and
    20. No buttons, no dialogs, no placeholders for them.
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
REMEDIATION — read this section last. Your previous run is on disk and is
largely accepted. Change as little as possible; do not restructure it.
═══════════════════════════════════════════════════════════════════════

`app/(dash)/addresses/page.tsx`, `app/(dash)/addresses/[address]/page.tsx`,
`app/(dash)/addresses/needs-resources/page.tsx`, `app/(dash)/addresses-dashboard.tsx`,
`app/(dash)/address-detail.tsx` and your `lib/query-keys.ts` additions all exist
and pass tsc, lint and build. Keep them. Four of the five things you reported as
blocked were real contract gaps, and three of them have now been closed in the
API. Wire them up; do not rebuild anything else.

FAILURES:

  1. CLIENT-SIDE FILTERING — this is the one actual defect, and it is the same
     class of mistake that WPAY-031 exists to forbid. You filtered state,
     asset, drift and resource status against the loaded cursor page. A cursor
     page is not the pool: that UI reports "3 disabled addresses" when the pool
     holds thirty, and an operator cannot tell the difference.

     `GET /wallets` now takes server-side filters:
       state=free|assigned|cooling|disabled
       asset=<symbol>     addresses holding a balance in that asset
       drift=true         addresses where an asset disagrees with the chain
     has-balance stays `GET /wallets/with-balance`, and needs-resources stays
     `GET /wallets/needs-resources`. Move EVERY filter to the query. Keep them
     in the URL (DAT-026) and send them to the API. Filter nothing in the
     browser. An unrecognised `state` returns 400 `invalid_state` — surface the
     error, do not pre-validate it into a silent no-op.
     WADR-006 has been rewritten to say exactly this; re-read it.

  2. WADR-008 POOL HEALTH — you rendered the loaded page's count. Pool totals
     now come from `GET /stats`, which reports `addresses` grouped by state:
     `{"free": n, "assigned": n, "cooling": n, "disabled": n}`. Every state is
     always present, so zero renders as 0 rather than as a missing key. Total is
     their sum — that is a count of addresses, not money, so summing is fine and
     INV-2 does not apply. Compare free against `wallet.pool_min_free` and the
     total against `wallet.pool_max_size` from `GET /config`, and hardcode
     neither threshold (WSYS-020a). See WADR-008a, newly added.
     `GET /stats` is loosely typed (`additionalProperties: true`); read
     `addresses` defensively and render "unavailable" rather than throwing if it
     is absent.

  3. WADR-021 DRIFT — you were right that `confirmed_raw` did not exist. It does
     now: every balance entry carries `confirmed_raw` alongside the optional
     `chain_raw`, both in base units, and `paymentSchema`'s wallet counterpart in
     `lib/payd/schemas.ts` is already updated — do not change the schema. Show
     the two side by side per asset. DO NOT SUBTRACT THEM. INV-2 is absolute:
     showing both figures is the requirement; computing the difference is not,
     and a client-side big-number subtraction on a money field is exactly what
     that invariant forbids. `chain_raw` is still optional and still renders as
     "not yet reconciled" when absent.

  4. WADR-030 ASSIGNMENT HISTORY — correctly reported, and the specification was
     wrong, not the API. The backend retains no per-address assignment history;
     it has the CURRENT assignment only. WADR-030 has been rewritten: render
     `assigned_order_id` and `cooling_until`, and do not imply a history exists.
     The address's orders and payments lists are the history. Adjust the wording
     in your detail view if it promises more than that.

  NOT A FAILURE, DO NOT ACT ON IT: WADR-070's withdrawal-wizard source selector
  is task 18-wd-wizard. `GET /wallets/with-balance` backing the pool list's
  confirmed-balance view is all that is owed here, and you built it.

After the changes: re-run `./node_modules/.bin/tsc --noEmit`, `npm run lint` and
`npm run build` (move `.env` aside, then MOVE IT BACK), and report the file:line
for WADR-006, WADR-008/008a, WADR-021 and WADR-030 only.
