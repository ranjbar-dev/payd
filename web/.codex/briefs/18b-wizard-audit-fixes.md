ROLE: PAGE
TASK-ID: 18b-wizard-audit-fixes
GOAL: Fix the four defects an independent audit found in the withdrawal path, and extend the two gate tests that failed to catch them.

You are working in the repository at C:\Users\root\Desktop\tron-payment-proccesor.
The web app is `web/`. Run every command from `web/`.

CONTEXT: the wizard was built, reviewed by the orchestrator, then audited
independently. The audit found four real defects and confirmed the rest correct.
Fix exactly these four. Do not restructure the wizard, do not re-style anything,
do not "improve" what the audit verified as correct.

READ FIRST:
  web/docs/specs/11-withdrawals.md §11.0 and §11.5, especially WWD-005,
    WWD-075, WWD-085, WWD-086, WWD-086a, WWD-086b
  web/docs/specs/03-architecture-and-bff.md — BFF-020
  web/app/(dash)/withdrawal-wizard.tsx
  web/lib/payd/client.ts, web/app/api/payd/[...path]/route.ts
  web/lib/payd/browser-client.ts — note `isPaydError` is `instanceof PaydError`
  web/lib/no-coercion.test.ts, web/lib/proxy-no-retry.test.ts

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/(dash)/withdrawal-wizard.tsx
  web/lib/payd/client.ts
  web/lib/no-coercion.test.ts        — EXPLICITLY AUTHORISED for this task only,
                                       and only to STRENGTHEN the check
  web/lib/proxy-no-retry.test.ts     — EXPLICITLY AUTHORISED for this task only,
                                       and only to ADD a case
  PLUS build configuration if genuinely required.
You may not weaken any test, widen any allowlist, or delete any assertion.

─────────────────────────────────────────────────────────────────────
FIX 1 — CRITICAL. The idempotency key does not survive navigation.
─────────────────────────────────────────────────────────────────────

`withdrawal-wizard.tsx:89` holds the key in component state only. After an
ambiguous outcome the panel links to `/withdrawals` (correctly — WWD-086 says
check the list first). But navigating there and coming back remounts the wizard,
state resets to null, and reaching step 3 mints a FRESH key at `:126`.

Sequence that pays twice: payd commits withdrawal A, the client gets a 500 or a
timeout, the operator follows the instruction to check the list, does not
recognise the row, returns and composes the same transfer. New key, new row,
second payout.

FIX: persist the key so that RECOMPOSING THE SAME TRANSFER REUSES IT.

  - Store the key in `sessionStorage` together with a signature of the transfer
    it belongs to: source, destination, asset, amount. Nothing else.
  - On mount, restore it. When the operator reaches step 3:
      · signature matches the stored one  → REUSE the stored key
      · signature differs                 → this is a genuinely different
                                            transfer, so mint a new key and
                                            replace the stored one
      · explicit "create a new withdrawal" → clear and mint a new one
  - Reusing the key is the SAFE direction and it is the whole point of
    idempotency: if row A exists, the backend returns it with 200 and the wizard
    already renders "existing withdrawal returned" rather than creating a second.
    If no row exists, the same key creates exactly one.

  SESSION STORAGE RULES — narrow and absolute. It holds the idempotency key and
  the transfer signature and NOTHING ELSE. Never the TOTP code, never a session
  id, never an API key, never a whole draft object. Put a comment on the write
  saying why this one value is allowed to persist. INV-4 is unchanged: no secret
  reaches browser storage, and an idempotency key is not a secret — it is the
  token that PREVENTS a second payout.

─────────────────────────────────────────────────────────────────────
FIX 2 — HIGH. The proxy follows redirects on POST.
─────────────────────────────────────────────────────────────────────

`lib/payd/client.ts` calls `fetch` with no `redirect` option, so it defaults to
`follow`. A 307 or 308 makes fetch re-issue the POST — body, `Idempotency-Key`
and `X-TOTP` included — to another URL. That is two upstream requests from one
operator click, which BFF-020 forbids outright.

FIX: set `redirect: "manual"` on the proxy fetch. A redirect from payd is not a
success and must not be followed; surface it as an upstream failure. Add a brief
comment naming BFF-020.

─────────────────────────────────────────────────────────────────────
FIX 3 — LOW, but it makes a requirement dead code.
─────────────────────────────────────────────────────────────────────

`createWithdrawal` at `withdrawal-wizard.tsx:47` throws a PLAIN OBJECT, while
`isPaydError` tests `instanceof PaydError`. So the 503 branch at `:145` never
runs and 503 falls through to the generic definite panel. The outcome stays SAFE
— 503 is genuinely "not created" — but WWD-085's stale-price distinction never
renders.

FIX: detect the thrown shape the function actually throws. Do not change what it
throws unless that is genuinely simpler, and if you do, check every other branch
that reads `payd.status` still works.

─────────────────────────────────────────────────────────────────────
FIX 4 — the detector that should have caught a future mistake.
─────────────────────────────────────────────────────────────────────

`lib/no-coercion.test.ts` money-name list omits `projected_trx_cost`, which the
wizard renders. A comparison like `estimate.projected_trx_cost > "0"` would pass
the check today.

FIX: add every money-named field these pages actually use that is missing —
including at least `projected_trx_cost`, `estimated_burn_trx`,
`estimated_rent_trx`, `energy_fee_sun`, `trx_for_bandwidth_burn`,
`bandwidth_cost_trx`, `network_fee_trx`, `resource_fee_trx`, `stake_trx`,
`provider_balance_trx`, `max_burn_trx`, `balance_warn_trx`. Keep the existing
names. The allowlist must NOT grow: if adding a name flags real code, that is a
finding — STOP and report it rather than allowlisting it.

ALSO: add a case to `lib/proxy-no-retry.test.ts` proving the proxy issues exactly
ONE upstream request when the upstream answers a POST with a 307 and again with a
308. Count calls as the existing cases do. That test passed a build containing
FIX 2's defect, which is what a missing case looks like.

─────────────────────────────────────────────────────────────────────

THE SIX INVARIANTS still bind, and on this page INV-1 binds hardest: no retry,
resume, re-broadcast, resend, or automatic re-send of any withdrawal mutation.
Money is a string. No secret in the browser. No business logic in the client.
Confirmed and pending are never merged. UTC is labelled.

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean (NOT `npx tsc`)
  - `npm run lint` clean
  - `npm test` green, now including your two new cases
  - `npm run build` succeeds and `.env` is back where it was (it holds three
    required-but-empty variables that beat process env; move it aside, MOVE IT
    BACK)
  - `grep -rniE "retry|resume|re-?broadcast|try ?again|resend|re-?send"` over the
    wizard files still returns nothing

YOU MUST NOT:
  - weaken any test or widen the coercion allowlist
  - store anything in sessionStorage beyond the key and the transfer signature
  - change what the ambiguous panel offers — it must still have no route to a
    fresh key
  - modify anything under `backend/`
  - commit, push, or change git branches

REPORT AT THE END:
  - each fix → file:line
  - the exact sessionStorage write, quoted, and what it stores
  - the new test cases and proof they fail without the fix (say what you
    temporarily broke to see them go red, and confirm you reverted it)
  - anything you could not do, and why
