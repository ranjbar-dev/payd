ROLE: PAGE
TASK-ID: 17-webhooks
GOAL: Build the webhooks page — consumers table, test ping, dead-letter worklist with single retry, bulk replay, and the static event reference.

You are working in the repository at C:\Users\root\Desktop\tron-payment-proccesor.
The web app is `web/`. Run every command from `web/`.

READ THIS BEFORE ANYTHING ELSE:

THIS PAGE CONTAINS THE ONLY PERMITTED RETRY IN THE ENTIRE DASHBOARD. Every other
retry is forbidden by INV-1. Here it is correct, because an IPN is an idempotent
NOTIFICATION and not a movement of funds: consumers treat `event_id` as an
idempotency key (backend IPN-022), and backend IPN-008 says outright, "Retry is
correct here and only here."

That exception is load-bearing in both directions. It must WORK — a dead IPN is a
consumer that never learned a payment arrived — and it must READ as an exception,
so nobody who sees a retry button here concludes that retrying a withdrawal is
also fine somewhere else.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/13-webhooks.md — the whole file is yours.
  web/docs/specs/05-data-fetching.md and 06-conventions.md
  backend/internal/api/openapi.yaml — sections for: GET /api/v1/ipn/consumers,
    GET /api/v1/ipn/dead, POST /api/v1/ipn/{id}/retry, POST /api/v1/ipn/replay,
    POST /api/v1/ipn/test. Read every `x-error-codes` block.

READ ALSO, AS THE PATTERN TO FOLLOW:
  web/app/(dash)/payment-worklists.tsx — the most recent worklist
  web/app/(dash)/order-actions.tsx     — the mutation + ConfirmDialog pattern
  web/components/forms/confirm-dialog.tsx — USE IT. Do not write a second dialog.
  web/lib/query.ts, web/lib/query-keys.ts, web/lib/payd/browser-client.ts

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/(dash)/webhooks/page.tsx
  web/app/(dash)/webhooks-dashboard.tsx
  web/app/(dash)/webhook-consumers.tsx
  web/app/(dash)/webhook-dead-letters.tsx
  web/app/(dash)/webhook-replay.tsx
  web/app/(dash)/webhook-event-reference.tsx
  web/lib/query-keys.ts   — ADD keys only; never change or remove an existing one.
  web/components/data/*.tsx, web/components/forms/*.tsx — only if a shared
    component needs a genuinely additive change. Never change an existing
    component's default behaviour.
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
  WIPN-001, WIPN-002                 (the retry exception, stated and styled)
  WIPN-010..WIPN-015                 (consumers)
  WIPN-020..WIPN-023                 (test ping)
  WIPN-030..WIPN-037                 (dead letters and single retry)
  WIPN-040..WIPN-045                 (bulk replay)
  WIPN-050..WIPN-054                 (event reference)
  DAT-026 — filter state in the URL.

THE SEVEN THINGS THAT WILL OTHERWISE GO WRONG:

  1. WIPN-001 — say it where the controls are, not in a footnote: redelivering
     an IPN is safe because consumers treat `event_id` as an idempotency key, and
     THIS IS THE ONLY RETRY IN THE SYSTEM. An operator who reads that once here
     is an operator who does not go looking for a retry on a withdrawal.

  2. WIPN-002 — retry controls MUST NOT be styled like withdrawal actions, and a
     withdrawal-related event MUST NOT gain any affordance that touches the
     underlying withdrawal. Retrying a `withdrawal.confirmed` notification
     redelivers a MESSAGE; it does nothing to the withdrawal, and the UI must not
     let anyone believe otherwise.

  3. WIPN-041 / WIPN-043 — `dry_run` DEFAULTS TO TRUE in the UI, matching the
     backend. The dry-run count must be shown and acknowledged before a live
     replay is possible. And the UI MUST NOT LOOP: the backend caps a call at 200
     events, and each further call is an explicit operator action. Do not write a
     "replay all" that iterates — a large replay must not be startable and then
     walked away from. State how many calls a larger range would need (WIPN-042);
     computing that from two integers is arithmetic on COUNTS, not money, so it
     is allowed.

  4. WIPN-011 / WIPN-054 — NO CONSUMER SECRET REACHES THIS PAGE. Not displayed,
     not fetched, not in a tooltip, not in a debug panel. The event reference
     documents that the signature is
     `hex(HMAC-SHA256(secret, timestamp + "." + raw_body))` without ever showing a
     secret. INV-4 applies with full force.

  5. WIPN-014 — there is NO control to add, edit, enable, disable or delete a
     consumer. Consumers are configuration; the API exposes no such endpoint and
     the UI must not imply one exists.

  6. WIPN-033 — a dead-letter payload is a SNAPSHOT taken when the event was
     queued. Where its status contradicts the order's current status, that must
     be marked. An operator reading a stale `order.paid` snapshot for an order
     that has since reverted will reach the wrong conclusion. WIPN-031: render
     the payload as pretty-printed JSON and mark it as a snapshot.
     WIPN-034: an event dead-lettered with `last_error: 'consumer removed'` needs
     its own explanation — retrying it will not help while the consumer is gone.

  7. WIPN-036 — after a retry, RE-FETCH the row. Do not optimistically update it.
     The retry resets the delivery state; what happens next is the dispatcher's
     business, and guessing at it in the client puts a fiction on the screen.

  WIPN-045 — replay dates are entered in local time with the resolved UTC range
  displayed (INV-6).
  WIPN-037 — the dead count feeds the nav alarm counter, which already exists in
  `app/(dash)/alarm-navigation.tsx` and reads `GET /stats`. Do not rebuild it and
  do not recount it from a page.
  WIPN-013 — a rising pending count is flagged; WIPN-012 — a disabled consumer's
  pending rows stay queued, and the UI must say so.

THE SIX INVARIANTS — with one explicit, bounded exception:

  INV-1  NO RETRY CONTROL ANYWHERE IN THE WITHDRAWAL PATH. THE IPN REDELIVERY
         CONTROLS ON THIS PAGE ARE THE ONE PERMITTED EXCEPTION (WIPN-001,
         backend IPN-008), and they extend ONLY to redelivering a notification.
         They do NOT extend to: re-sending a failed `POST /ipn/retry` itself
         automatically, retrying any other mutation, or any control that touches
         a withdrawal, order, payment or address. Mutations stay `retry: false`
         at the query-client level — a failed retry-request is re-submitted only
         by the operator clicking again.
  INV-2  MONEY IS A STRING, START TO FINISH. Event payloads contain amounts;
         render them as the strings they are and never coerce or compute with
         them. Counts of events are not money and may be added.
  INV-3  `confirmed` and `pending` balances are never merged.
  INV-4  NO PAYD API KEY, TOTP CODE, OR SECRET REACHES THE BROWSER — and on this
         page that specifically includes every consumer signing secret. No
         `NEXT_PUBLIC_` variable exists in this project; do not add one.
  INV-5  NO BUSINESS LOGIC IN THE CLIENT. Do not decide whether an event should
         be redelivered, whether a consumer is healthy, or what a payload means.
  INV-6  ANYTHING SCOPED TO A UTC DAY IS LABELLED UTC IN VISIBLE TEXT.

DESIGN BRIEF — a financial operations console, not a marketing site. Linear's
density and keyboard discipline, Stripe's clarity about money, a terminal's
honesty about state.
  - Dark mode is the DEFAULT (UI-075).
  - Density over whitespace; cards are the <1024px fallback only (UI-073).
  - Tabular figures and monospace for every amount, address, txid, id and
    payload.
  - Colour carries severity, never identity: neutral, progress, success, muted,
    warning, critical (UI-020), with an icon on warning and critical (UI-021).
  - The retry and replay controls must be visually distinct from the fund-moving
    confirmations elsewhere in the dashboard (WIPN-002). Different weight,
    different placement — they are routine operations, not decisions about money.
  - An empty dead-letter table is a SUCCESS state: every consumer heard
    everything. An empty search is NEUTRAL. A failed load is an ERROR that keeps
    the last good data visible (UI-050, UI-051).
  - No decorative motion. Keyboard reachable, visible focus rings (UI-076).

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean (NOT `npx tsc`)
  - `npm run lint` clean
  - `npm test` green — 4/4, no test edited
  - `npm run build` succeeds and `.env` is back where it was
  - every requirement ID above is satisfied and you can point to where
  - `/webhooks` renders, and the existing dead-IPN alarm link from the nav and
    from the order events tab resolves to it
  - no automatic loop exists anywhere in the replay path
  - no consumer secret appears in any file you wrote

YOU MUST NOT:
  - write a loop that replays more than one call automatically
  - default `dry_run` to false
  - build any consumer create/edit/enable/disable/delete control
  - display or fetch a consumer secret
  - add a runtime dependency
  - edit any test
  - modify anything under `backend/`
  - commit, push, or change git branches
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line where it is satisfied
  - the exact wording you used for WIPN-001, quoted
  - confirmation that no consumer secret is fetched or rendered, with the grep
    you ran
  - anything you could not do, and why
