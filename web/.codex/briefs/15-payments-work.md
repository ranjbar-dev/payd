ROLE: PAGE
TASK-ID: 15-payments-work
GOAL: Build the two payment worklists — unattributed and orphaned — and the attribute action.

You are working in the repository at C:\Users\root\Desktop\tron-payment-proccesor.
The web app is `web/`. Run every command from `web/`.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/05-data-fetching.md — mutation rules, DAT-020, DAT-030..034
  web/docs/specs/06-conventions.md — UI-060 especially
  web/docs/specs/09-payments.md §9.4 and §9.5 — yours. §9.1–9.3 are already
    built (search, table, detail drawer); reuse them, do not rebuild them.
  backend/internal/api/openapi.yaml — sections for:
    GET /api/v1/payments/unattributed, GET /api/v1/payments/orphaned,
    POST /api/v1/payments/{id}/attribute, GET /api/v1/orders. Read the
    `x-error-codes` on the attribute route.

READ ALSO, AS THE PATTERN TO FOLLOW:
  web/app/(dash)/payments-dashboard.tsx and web/app/(dash)/payment-drawer.tsx
    — the payments page you are extending
  web/app/(dash)/funded-terminal-worklist.tsx — the worklist + confirm-dialog
    pattern that landed in the task before this one. Match it.
  web/components/forms/confirm-dialog.tsx — USE IT. Do not write a second dialog.
  web/lib/query.ts, web/lib/query-keys.ts, web/lib/payd/browser-client.ts

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/(dash)/payments/unattributed/page.tsx
  web/app/(dash)/payments/orphaned/page.tsx
  web/app/(dash)/payment-worklists.tsx
  web/app/(dash)/payment-attribute.tsx
  web/app/(dash)/payments-dashboard.tsx   — only to link to the worklists
  web/lib/query-keys.ts                   — ADD keys only; never change or
                                            remove an existing one.
  web/components/data/*.tsx, web/components/forms/*.tsx — only if a shared
                                            component needs a genuinely additive
                                            change. Never change an existing
                                            component's default behaviour.
  PLUS the build configuration when required: web/package.json,
  web/tsconfig.json, web/next.config.mjs, web/postcss.config.mjs,
  web/tailwind.config.ts, web/components.json. The project is `"type": "module"`
  and every config file is already ESM — do not convert one back.
Everything else belongs to another agent. If you need a change outside this
list, STOP and report it instead of making it.

NOTE ON RUNNING THE BUILD: `web/.env` holds three required variables that are
present but EMPTY, and Next lets empty values beat process-supplied ones. Move
`.env` aside, build with inline values, MOVE IT BACK. Do not fill it in.

`npm test` RUNS FOUR GATE CHECKS AND THEY MUST STAY GREEN. In particular
`lib/no-coercion.test.ts` fails the build on any numeric coercion or arithmetic
touching a money-named identifier, with an exact-line allowlist. YOU MAY NOT EDIT
ANY TEST. If you think you need a new allowlist entry, you are doing something
INV-2 forbids — stop and report it.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  WPAY-030..WPAY-037   (unattributed worklist and the attribute action)
  WPAY-040..WPAY-045   (orphaned worklist)
  DAT-026 — worklist view state in the URL.

THE FIVE THINGS THAT WILL OTHERWISE GO WRONG:

  1. NO CLIENT-SIDE FILTER ON THE UNATTRIBUTED WORKLIST (WPAY-031). There is no
     backend filter for `unattributed_reason`, and the UI MUST NOT offer one.
     Filtering a cursor-paginated list in the browser applies to the loaded page
     only and misrepresents the worklist's true size — it reports "2 asset
     mismatches" when there are twenty. This exact defect already cost the
     addresses task an attempt; do not repeat it. Show the reason as a BADGE per
     row, with `asset_mismatch` at warning severity, and no filter control.

  2. THE MONEY IS NOT MISSING (WPAY-032). State on the worklist that these funds
     are real and already credited to the address's balance — they are
     unattributed, not lost. An operator who believes the money is gone will go
     looking for it on chain and waste the outage.

  3. ATTRIBUTING THE WRONG ASSET LOSES THE SAME MONEY THE BACKEND REFUSED TO
     LOSE (WPAY-034). The attribute dialog MUST warn when the chosen order's
     asset differs from the payment's, and MUST require an EXTRA confirmation —
     a second, explicit step, not a checkbox in the same click. Attributing 25
     TRX to a 25 USDT order is precisely the loss backend ORD-002a exists to
     prevent, and doing it by hand loses exactly as much.
     WPAY-035: warn when the target order is terminal, since attribution will
     not reopen it.
     WPAY-033: the operator picks the target by searching orders on the SAME
     ADDRESS AND ASSET, and the dialog shows the candidate order's expected and
     received amounts before submission. Both are strings; render them, never
     compare or subtract them.

  4. THE REASON IS A BACKEND DECISION (WPAY-023a, already built into the drawer —
     hold the same line here). Read `unattributed_reason`; never recompute it by
     comparing the payment against the address's current order. That state has
     changed since the decision was made. A null reason renders as "reason not
     recorded" (WPAY-023b), never as one of the three values and never as an
     error.

  5. AN ORPHANED PAYMENT HAS NO RESTORE BUTTON (WPAY-044). There MUST be no
     control to "restore", "re-confirm", "recover" or "re-detect" an orphaned
     payment. Inclusion is a chain fact; the backend re-detects it by itself if
     the transaction reappears. The operator's real next step is checking the
     txid on Tronscan (WPAY-042), so that link is the prominent action.
     WPAY-041: explain what orphaned means — seen in a block, that block was
     reorganised away, the transaction has not reappeared within the reorg
     depth, the money is very likely not there.
     WPAY-043: show which order it had been contributing to AND that order's
     CURRENT status. A reorg that reverted a `paid` order to `partial` is the
     case that matters.
     WPAY-045: a non-empty orphaned list is WARNING severity even when it holds
     one row. A single unresolved orphan usually means a customer was credited
     for money that no longer exists.

  WPAY-036 — after a successful attribution, invalidate the order, the order
  list, the funded-terminal list, the address, and the alarm counters.
  WPAY-037 — the unattributed count feeds the nav alarm counter, combined with
  orphaned per WOVW-006. That counter already exists in
  `app/(dash)/alarm-navigation.tsx` and reads from `GET /stats`; do not rebuild
  it and do not recount it from a page.

THE SIX INVARIANTS — these override anything you think is a better idea:

  INV-1  NO RETRY CONTROL ANYWHERE IN THE WITHDRAWAL PATH. No retry, resume,
         re-broadcast, or "try again" button, link, menu item, or automatic
         re-send of a failed mutation. Mutations are `retry: false` at the
         query-client level. A failed attribution is re-submitted only by the
         operator deliberately repeating the action.
  INV-2  MONEY IS A STRING, START TO FINISH. No Number(), parseFloat, +, -,
         toFixed, toLocaleString, or comparison operator on any amount field.
         Ever. Do not compare the payment's amount against the order's expected
         or received amount, in either direction — the backend decides what an
         attribution means, and a test fails the build if you try.
  INV-3  `confirmed` AND `pending` BALANCES ARE NEVER MERGED into one figure.
  INV-4  NO PAYD API KEY, TOTP CODE, OR SECRET REACHES THE BROWSER. No
         `NEXT_PUBLIC_` variable exists in this project and you must not add one.
  INV-5  NO BUSINESS LOGIC IN THE CLIENT. Never decide client-side whether an
         attribution is valid, whether an order can still be paid, or why a
         payment was not attributed.
  INV-6  ANYTHING SCOPED TO A UTC DAY IS LABELLED UTC IN VISIBLE TEXT.

DESIGN BRIEF — a financial operations console, not a marketing site. Linear's
density and keyboard discipline, Stripe's clarity about money, a terminal's
honesty about state.
  - Dark mode is the DEFAULT (UI-075).
  - Density over whitespace; cards are the <1024px fallback only (UI-073).
  - Tabular figures and monospace for every amount, address, txid, and id.
  - Colour carries severity, never identity: neutral, progress, success, muted,
    warning, critical (UI-020). Warning and critical carry an icon too (UI-021).
  - AN EMPTY WORKLIST IS A SUCCESS STATE, and these two are the clearest example
    in the whole dashboard: no unattributed payments means every payment found
    its order, and no orphaned payments means nothing was credited that the
    chain later took away. Make them look like success, not like an empty table
    (UI-050).
  - A failed load is an ERROR that keeps the last good data visible (UI-051).
  - No decorative motion. Keyboard reachable, visible focus rings (UI-076).

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean (NOT `npx tsc`)
  - `npm run lint` clean
  - `npm test` green — 4/4, with no test edited
  - `npm run build` succeeds and `.env` is back where it was
  - every requirement ID above is satisfied and you can point to where
  - `/payments/unattributed` and `/payments/orphaned` render and are reachable
    from the payments page
  - the orphaned view contains no control that submits anything

YOU MUST NOT:
  - offer any filter on `unattributed_reason`
  - build a restore, re-confirm or re-detect control for an orphaned payment
  - add a runtime dependency (@tanstack/react-query, lucide-react, next, react,
    react-dom, react-hook-form, zod — nothing else)
  - edit any test to get green
  - modify anything under `backend/`
  - implement a business rule the backend owns
  - commit, push, or change git branches
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line where it is satisfied
  - the exact text of the asset-mismatch warning and of its extra confirmation
    step, quoted, so the wording can be reviewed without opening the files
  - anything you could not do, and why
  - any spec ambiguity or contradiction you hit
