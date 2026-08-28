ROLE: PAGE
TASK-ID: 14-orders-mut
GOAL: Add the order mutations — create, cancel (including the force path), extend — and the funded-terminal worklist with its resolve action.

You are working in the repository at C:\Users\root\Desktop\tron-payment-proccesor.
The web app is `web/`. Run every command from `web/`.

These are the first mutations in the dashboard. None of them requires TOTP and
none of them moves money — but one of them (`resolve`) is routinely MISTAKEN for
moving money, and that misunderstanding is the thing this task must design
against.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/05-data-fetching.md — mutation rules, DAT-030..DAT-034
  web/docs/specs/06-conventions.md — UI-060 in particular: confirmation text
    comes from the API response, not from the form inputs
  web/docs/specs/08-orders.md §8.4, §8.5, §8.6 — yours in full. §8.1–8.3 are
    already built; do not rebuild them.
  backend/internal/api/openapi.yaml — sections for: POST /api/v1/orders,
    POST /api/v1/orders/{id}/cancel, POST /api/v1/orders/{id}/extend,
    POST /api/v1/orders/{id}/resolve, GET /api/v1/orders/funded-terminal,
    GET /api/v1/assets, GET /api/v1/ipn/consumers. Read every `x-error-codes`
    block on those routes — half this task is error rendering.

READ ALSO, AS THE PATTERN TO FOLLOW:
  web/app/(dash)/orders-dashboard.tsx — the page you are extending
  web/app/(dash)/withdrawal-detail.tsx — the most recent detail view
  web/components/forms/confirm-dialog.tsx — the shared confirm component. USE
    IT. Do not write a second dialog.
  web/lib/query.ts — mutations are `retry: false` at the client level. Do not
    set a per-call retry policy, and do not override it.
  web/lib/query-keys.ts, web/lib/payd/browser-client.ts, web/lib/payd/schemas.ts

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/(dash)/orders-dashboard.tsx      — extend it with the actions
  web/app/(dash)/orders/new/page.tsx
  web/app/(dash)/orders/funded-terminal/page.tsx
  web/app/(dash)/order-create-form.tsx
  web/app/(dash)/order-actions.tsx
  web/app/(dash)/funded-terminal-worklist.tsx
  web/lib/query-keys.ts                    — ADD keys only; never change or
                                             remove an existing one.
  web/components/data/*.tsx, web/components/forms/*.tsx — only if a shared
                                             component needs a genuinely
                                             additive change. Never change an
                                             existing component's default
                                             behaviour; other pages depend on it.
  PLUS the build configuration when your change requires it: web/package.json,
  web/tsconfig.json, web/next.config.mjs, web/postcss.config.mjs,
  web/tailwind.config.ts, web/components.json. The project is `"type": "module"`
  and every config file is already ESM — do not convert one back.
Everything else belongs to another agent. If you need a change outside this
list, STOP and report it instead of making it.

NOTE ON RUNNING THE BUILD: `web/.env` holds three required variables that are
present but EMPTY, and Next lets empty values beat process-supplied ones. Move
`.env` aside, build with inline values, MOVE IT BACK. Do not fill it in.

THERE ARE NOW TESTS. `npm test` runs three gate checks and they must stay green:
  - `lib/no-coercion.test.ts` FAILS THE BUILD on any numeric coercion or
    arithmetic touching a money-named identifier, with an exact-line allowlist.
    If you write `Number(amount)` anywhere, this task fails. If you need a new
    allowlist entry, you are almost certainly doing something INV-2 forbids —
    stop and report it rather than editing the allowlist. YOU MAY NOT EDIT THAT
    TEST.
  - `lib/proxy-no-retry.test.ts` proves one upstream call per POST.
  - `lib/session-expiry.test.ts` proves an expired session cannot reach payd.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  WORD-030..WORD-041   (create form and its five distinct error paths)
  WORD-050..WORD-056   (cancel, force-cancel, extend)
  WORD-060..WORD-068   (funded-terminal worklist and resolve)
  DAT-026 — the worklist's filter state in the URL.

THE FOUR THINGS THAT WILL OTHERWISE GO WRONG:

  1. `resolve` DOES NOT MOVE MONEY, AND EVERY OPERATOR WILL ASSUME IT DOES.
     WORD-064: the dialog must say plainly that this RECORDS A DECISION and moves
     nothing. Choosing `refunded` does not issue a refund. A refund is a separate
     withdrawal the operator makes afterwards from the deposit address. WORD-065
     permits a link that pre-fills the withdrawal wizard as a convenience for
     that separate deliberate action — it MUST NOT be part of the same
     submission, and it MUST NOT read as "resolve and refund". The wizard is task
     18 and does not exist yet; the link may dangle.
     WORD-063: the note is required and must be non-empty. It is the audit
     trail's only explanation of why money stopped being chased.
     WORD-068: say in the dialog that the action is written to `audit_log`.

  2. FORCE-CANCEL IS A SECOND DECISION, NOT A RETRY OF THE FIRST.
     WORD-051: on a 409 for an order in `partial`, `paid` or `confirmed`, DO NOT
     auto-set `force: true` and DO NOT re-submit. Surface the received amount
     from the error and require a SECOND, explicit confirmation whose text states
     that the order becomes `cancelled_funded` and the funds stay in the deposit
     address awaiting a resolution record. WORD-052: warn that the address
     returns to the pool after cooldown with the funds still in it, and that the
     order will appear in the funded-terminal worklist.
     An auto-escalating force flag turns "cancel this empty order" into "abandon
     a funded one" with one click, which is why the requirement is written this
     way.

  3. THE CREATE FORM'S ERRORS ARE FIVE DIFFERENT THINGS.
     WORD-037 — 409 `external_ref_conflict`: render the conflicting fields from
     `details` SIDE BY SIDE, requested versus stored, and link to the existing
     order. NEVER show the stored order as if creation succeeded. That exact
     failure — a 500 USDT request rendering a 25 USDT order — is why backend
     API-002 exists.
     WORD-038 — 200 exact idempotent match: say an EXISTING order was returned,
     not that one was created.
     WORD-039 — 503 `address_pool_exhausted`: the pool hit `wallet.pool_max_size`
     with no free address. Link to the addresses page. Do NOT present it as
     transient or offer to try again.
     WORD-040 — 503 from stale prices: name price staleness, link to the prices
     card.
     WORD-041 — 400 unknown or disabled consumer: name the consumer, link to the
     webhooks page (task 17; the link may dangle).
     The code is on `error.code` (DAT-030). The pool-exhausted code is
     `address_pool_exhausted` — not `pool_exhausted`.

  4. AMOUNT INPUT IS A STRING FIELD (WORD-035, INV-2). A numeric input coerces
     and rounds. Use a text input with a string-preserving mask, validate
     precision against the decimals from `GET /assets` (WORD-031) by string
     inspection, and send the string through untouched. `amount` OR `amount_usd`,
     never both (WORD-030).
     WORD-034: stamp `created_by: "dashboard"` into `metadata`.
     WORD-033: warn that a dashboard-created order has no consumer service
     expecting its IPNs.
     WORD-054: cap the extend input at 24 hours after `created_at` rather than
     submitting a value that will 400.
     WORD-056: NEITHER CANCEL NOR EXTEND PROMPTS FOR TOTP. Not "optional", not
     "if configured" — no prompt. An unnecessary code prompt trains the operator
     to generate codes reflexively, and the withdrawal flow depends on them not
     having that habit.

  UI-060 — every destructive or fund-adjacent confirmation reads its text from
  the API response where one is available, not from the form inputs. What the
  server understood is what the operator must confirm.

THE SIX INVARIANTS — these override anything you think is a better idea:

  INV-1  NO RETRY CONTROL ANYWHERE IN THE WITHDRAWAL PATH. No retry, resume,
         re-broadcast, or "try again" button, link, menu item, or automatic
         re-send of a failed mutation. Mutations are `retry: false` at the
         query-client level. A failed cancel, extend, create or resolve is
         re-submitted only by the operator deliberately re-entering the action —
         never by the client, and never by a control labelled "retry".
  INV-2  MONEY IS A STRING, START TO FINISH. No Number(), parseFloat, +, -,
         toFixed, toLocaleString, or comparison operator on any amount field,
         including in validation. Ever. There is now a test that fails the build
         on this.
  INV-3  `confirmed` AND `pending` BALANCES ARE NEVER MERGED into one figure.
  INV-4  NO PAYD API KEY, TOTP CODE, OR SECRET REACHES THE BROWSER. No
         `NEXT_PUBLIC_` variable exists in this project and you must not add one.
  INV-5  NO BUSINESS LOGIC IN THE CLIENT. Do not decide client-side whether an
         order may be cancelled, whether force is required, or what a 409 means
         beyond rendering what the API said. The one permitted client-side rule
         is WORD-054's 24-hour extend cap, which the spec explicitly requires
         the UI to mirror.
  INV-6  ANYTHING SCOPED TO A UTC DAY IS LABELLED UTC IN VISIBLE TEXT.

DESIGN BRIEF — a financial operations console, not a marketing site. Linear's
density and keyboard discipline, Stripe's clarity about money, a terminal's
honesty about state.
  - Dark mode is the DEFAULT (UI-075).
  - Density over whitespace; cards are the <1024px fallback only (UI-073).
  - Tabular figures and monospace for every amount, address, txid, and id.
  - Colour carries severity, never identity: neutral, progress, success, muted,
    warning, critical, and nothing else (UI-020). Warning and critical carry an
    icon too — colour is never the only signal (UI-021).
  - An empty funded-terminal worklist is a SUCCESS state, not a neutral one:
    it means no customer's money is sitting unresolved. An empty search is
    NEUTRAL. A failed load is an ERROR that keeps the last good data visible
    (UI-050, UI-051).
  - The funded-terminal payer address is the most important thing on that
    screen (WORD-061): prominent, monospace, copyable. It is what makes a refund
    actionable without a chain lookup.
  - Sorted oldest first (WORD-062). Age is the risk.
  - No decorative motion. Keyboard reachable, visible focus rings (UI-076).

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean (NOT `npx tsc`)
  - `npm run lint` clean
  - `npm test` green — all four existing tests still pass
  - `npm run build` succeeds and `.env` is back where it was
  - every requirement ID above is satisfied and you can point to where
  - `/orders/new` and `/orders/funded-terminal` render; cancel, force-cancel,
    extend and resolve all work from the order views
  - after a successful mutation the right queries are invalidated: the order,
    the order list, the funded-terminal list, and the alarm counters (WORD-066,
    WORD-067)

YOU MUST NOT:
  - prompt for TOTP anywhere in this task
  - auto-set `force: true`, or auto-resubmit any mutation for any reason
  - add a runtime dependency (@tanstack/react-query, lucide-react, next, react,
    react-dom, react-hook-form, zod — nothing else)
  - edit `lib/no-coercion.test.ts` or any other test to get green
  - modify anything under `backend/`
  - implement a business rule the backend owns
  - commit, push, or change git branches
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line where it is satisfied
  - the exact text of the force-cancel second confirmation and of the resolve
    dialog, quoted, so the wording can be reviewed without opening the files
  - anything you could not do, and why
  - any spec ambiguity or contradiction you hit
