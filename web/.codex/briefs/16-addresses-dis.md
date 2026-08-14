ROLE: PAGE
TASK-ID: 16-addresses-dis
GOAL: Add the address disable action to the existing addresses pages. Nothing else.

You are working in the repository at C:\Users\root\Desktop\tron-payment-proccesor.
The web app is `web/`. Run every command from `web/`.

This is a SMALL task. The addresses pages are already built and correct. You are
adding one action and its confirmation. Do not restructure, re-style, or
"improve" anything you find.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/10-addresses.md §10.7 — yours. §10.2–10.5 and §10.8 are already
    built. §10.3's clear-drift action and §10.6's delegate are NOT yours; they
    are task 20-addr-totp.
  web/docs/specs/06-conventions.md — UI-060
  backend/internal/api/openapi.yaml — POST /api/v1/wallets/{address}/disable,
    including its `x-error-codes`.

READ ALSO, AS THE PATTERN TO FOLLOW:
  web/app/(dash)/addresses-dashboard.tsx, web/app/(dash)/address-detail.tsx
  web/app/(dash)/order-actions.tsx — the mutation + ConfirmDialog pattern from
    task 14. Match it.
  web/components/forms/confirm-dialog.tsx — USE IT. Do not write a second dialog.

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/(dash)/address-disable.tsx
  web/app/(dash)/address-detail.tsx      — to mount the action
  web/app/(dash)/addresses-dashboard.tsx — only if a row-level action belongs
                                           there; prefer detail-only if unsure
  web/lib/query-keys.ts                  — ADD keys only; never change or remove
                                           an existing one.
  PLUS the build configuration when required: web/package.json,
  web/tsconfig.json, web/next.config.mjs, web/postcss.config.mjs,
  web/tailwind.config.ts, web/components.json.
Everything else belongs to another agent. If you need a change outside this
list, STOP and report it instead of making it.

NOTE ON RUNNING THE BUILD: `web/.env` holds three required variables that are
present but EMPTY, and Next lets empty values beat process-supplied ones. Move
`.env` aside, build with inline values, MOVE IT BACK.

`npm test` RUNS FOUR GATE CHECKS AND THEY MUST STAY GREEN. YOU MAY NOT EDIT ANY
TEST. `lib/no-coercion.test.ts` fails the build on any numeric coercion or
arithmetic touching a money-named identifier.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  WADR-060, WADR-061, WADR-062, WADR-063, WADR-064

WHAT MATTERS HERE:

  WADR-064 — THERE IS NO RE-ENABLE. The backend exposes no such endpoint, and
  the UI must not imply one exists: no "enable" button, no disabled-state toggle,
  no "you can re-enable this later" copy, no tooltip hinting at it. A toggle
  control is the wrong shape for this action entirely — it is a one-way door, so
  it reads as a one-way action.

  WADR-061 — the dialog states plainly what disabling does: permanent removal
  from rotation, history retained, NO FUNDS MOVED. An operator who thinks
  disabling sweeps or protects the balance will disable an address and walk away
  from the money.

  WADR-062 — if the address holds a balance, warn that the funds STAY THERE and
  the address must still be withdrawn from explicitly. Whether it holds a balance
  is a question the API already answers per asset (`confirmed`, `pending`) — read
  those figures, render them, and do NOT compare them numerically or sum them.
  INV-2 and INV-3 both apply, and a test fails the build on the former. If you
  need to decide "holds a balance" for the purpose of showing a warning, prefer
  showing the balances unconditionally over computing a zero-check.

  WADR-063 — if the address has an active assigned order, warn that the ORDER IS
  UNAFFECTED and the customer may still pay to it. Disabling stops rotation, not
  the order.

  WADR-060 — NO TOTP. Not optional, not conditional. The scope is
  `wallets:write` and the backend asks for no code. An unnecessary prompt trains
  the operator to generate codes reflexively, and the withdrawal flow depends on
  them not having that habit.

  After a successful disable, invalidate the address detail, the wallet lists,
  and the pool-health stats.

THE SIX INVARIANTS — these override anything you think is a better idea:
  INV-1  No retry, resume, or automatic re-send on any mutation path. A failed
         disable is re-submitted only by the operator repeating the action.
  INV-2  Money is a string, start to finish. No Number(), parseFloat, +, -,
         toFixed, toLocaleString, or comparison on any amount field. Ever.
  INV-3  `confirmed` and `pending` balances are never merged into one figure.
  INV-4  No API key, TOTP code, or secret reaches the browser. No
         `NEXT_PUBLIC_` variable exists in this project; do not add one.
  INV-5  No business logic in the client. Do not decide client-side whether an
         address may be disabled — render what the API says and surface its
         error.
  INV-6  Anything scoped to a UTC day is labelled UTC in visible text.

DESIGN BRIEF: dark mode default, density over whitespace, tabular monospace
figures for every amount and address, the six-level severity palette only, an
icon alongside warning and critical, no decorative motion, keyboard reachable
with visible focus rings. The confirmation text reads from the API response
where one is available, not from form inputs (UI-060).

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean (NOT `npx tsc`)
  - `npm run lint` clean
  - `npm test` green — 4/4, no test edited
  - `npm run build` succeeds and `.env` is back where it was
  - all five requirement IDs satisfied, with file:line
  - `grep -niE "enable|re-enable"` over your files finds nothing that implies an
    address can be returned to rotation

YOU MUST NOT:
  - prompt for TOTP
  - build a re-enable control, or copy suggesting one exists
  - build the delegate or clear-drift actions — task 20
  - add a runtime dependency
  - edit any test
  - modify anything under `backend/`
  - commit, push, or change git branches
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line
  - the exact dialog text, quoted
  - anything you could not do, and why
