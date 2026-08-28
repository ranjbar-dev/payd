ROLE: PAGE
TASK-ID: 20-addr-totp
GOAL: Build the two TOTP-gated address actions — delegate (§10.6) and clear-drift (§10.3) — and wire delegate into the needs-resources worklist.

You are working in the repository at C:\Users\root\Desktop\tron-payment-proccesor.
The web app is `web/`. Run every command from `web/`.

═══════════════════════════════════════════════════════════════════════
ONE OF THESE TWO ACTIONS MOVES FUNDS. THE OTHER MOVES NOTHING AND IS
ROUTINELY MISTAKEN FOR FIXING SOMETHING.
═══════════════════════════════════════════════════════════════════════

DELEGATE broadcasts a transaction. It is attempted EXACTLY ONCE and is NEVER
retried, under the same no-retry rule as a withdrawal (backend RES-013, WDR-000).
Everything §11.0 says about withdrawals applies to it.

CLEAR-DRIFT records an acknowledgement. It corrects NO balance. It re-enables
withdrawals from an address whose ledger and chain disagree — which is exactly
why it is dangerous to treat as a fix. An operator who clears drift believing it
repaired the balance has just unblocked payouts from an address whose true
holdings are unknown.

Read `web/docs/specs/11-withdrawals.md` §11.0 before writing anything; it binds
the delegate path too.

§11.0 IN SHORT, AND IT OVERRIDES ANYTHING BELOW YOU DISAGREE WITH: no control
anywhere retries, resumes, re-broadcasts or re-signs a fund-moving action; no
mutation is automatically re-sent by the client for any reason — timeout,
network error, 5xx or 429; mutations are `retry: false` at the query-client
level; and on an ambiguous outcome the UI shows what is known and offers no
affordance that could send a second transaction.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/10-addresses.md §10.3 (WADR-020..025) and §10.6
    (WADR-050..056). §10.2, §10.4, §10.5, §10.7, §10.8 are DONE — do not rebuild
    them.
  web/docs/specs/12-resources-and-energy.md — where WADR-054 and WADR-055 point
  web/docs/specs/04-auth-and-session.md — AUTH-040..AUTH-045
  backend/internal/api/openapi.yaml — POST /api/v1/wallets/{address}/delegate,
    POST /api/v1/wallets/{address}/clear-drift, GET /api/v1/resources/wallet,
    and every `x-error-codes` entry on all three.

READ ALSO, AS THE PATTERN TO FOLLOW:
  web/app/(dash)/withdrawal-resolve.tsx and web/app/(dash)/withdrawal-wizard.tsx
    — the two TOTP-gated actions already built. AUTH-045 requires ALL FOUR to
    route through the ONE shared confirm component. These two are the third and
    fourth; follow the established pattern exactly rather than inventing a third
    shape.
  web/components/forms/confirm-dialog.tsx, web/components/forms/totp-field.tsx
  web/app/(dash)/address-detail.tsx, web/app/(dash)/addresses-dashboard.tsx
  web/app/(dash)/address-disable.tsx — the non-TOTP address action

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/(dash)/address-delegate.tsx
  web/app/(dash)/address-clear-drift.tsx
  web/app/(dash)/address-detail.tsx      — to mount both actions
  web/app/(dash)/addresses-dashboard.tsx — ONLY to add the delegate action to the
                                           needs-resources rows (WADR-046)
  web/lib/query-keys.ts                  — ADD keys only.
  web/components/forms/*.tsx             — ADDITIVE only; four tasks depend on
                                           these now.
  PLUS build configuration if genuinely required.
Everything else belongs to another agent. If you need a change outside this
list, STOP and report it instead of making it.

NOTE ON RUNNING THE BUILD: `web/.env` holds three required variables that are
present but EMPTY, and Next lets empty values beat process-supplied ones. Move
`.env` aside, build with inline values, MOVE IT BACK.

`npm test` RUNS FOUR GATE CHECKS AND THEY MUST STAY GREEN. YOU MAY NOT EDIT ANY
TEST. The money-coercion detector now covers twelve more field names including
`estimated_burn_trx` and `trx_for_bandwidth_burn`.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  WADR-020, WADR-021 are already built (drift DISPLAY). Yours:
  WADR-022, WADR-023, WADR-024, WADR-025   (clear-drift)
  WADR-046                                  (delegate action on needs-resources)
  WADR-050..WADR-056                        (delegate)
  AUTH-045 — the one shared confirm component, as tasks 18 and 19 use it.

THE EIGHT THINGS THAT WILL OTHERWISE GO WRONG:

  1. DELEGATE MUST NOT DEFAULT TO ENERGY (WADR-051). The dialog requires an
     EXPLICIT choice of `ENERGY` or `BANDWIDTH`, with neither preselected.
     Hardcoding `ENERGY` was the v1.1 bug that made bandwidth unsourceable —
     backend RES-010 names it. A default is the same bug with extra steps.

  2. DELEGATE IS BROADCAST ONCE AND NEVER RETRIED (WADR-053). The dialog says
     so, in those terms, before the operator confirms. This is a fund-moving
     transaction under the withdrawal no-retry rule.

  3. ON AN AMBIGUOUS DELEGATE OUTCOME, NO TRY-AGAIN (WADR-054). Direct the
     operator to the resource grants list to see the recorded grant and its
     on-chain resolution. Same classification as the wizard: any 5xx other than
     503, and any failure with no HTTP status at all, is ambiguous. Do not offer
     a second attempt on any of them.

  4. SUCCESS IS THE GRANT RECORD, NOT THE HTTP RESPONSE (WADR-055). After
     submission, link to the grant rather than declaring success from a 2xx. The
     broadcast outcome is settled on chain, not in the response body. The
     resources page is task 21 and does not exist yet; the link may dangle.

  5. SHOW THE RESOURCE WALLET BEFORE DELEGATING (WADR-052). Current available
     energy, bandwidth and TRX from `GET /resources/wallet`, in the dialog,
     before submission — so nobody delegates from an empty wallet.
     WADR-056: state that stake is NOT managed here — the service never stakes or
     unstakes automatically, and unstaking has a 14-day period.

  6. CLEAR-DRIFT CORRECTS NOTHING (WADR-023). The dialog says plainly that it
     records an acknowledgement and does NOT correct any balance; it re-enables
     withdrawals from the address without making the ledger right. Say it
     prominently, not as a footnote.

  7. CLEAR-DRIFT IS PER ASSET AND REQUIRES ACKNOWLEDGING THE CHAIN VALUE
     (WADR-022). The operator must acknowledge the current `chain_raw` shown in
     the dialog — the exact value, echoed from the API, not typed from memory.
     The backend takes that value and rejects a mismatch, which is the point:
     it proves the operator looked. Send it as the API defines it.
     WADR-024: recommend investigating first, and link to the address's payment
     history AND its Tronscan page. Drift usually means a payment the detector
     missed or a transfer nothing recorded.
     WADR-025: clearing invalidates the address, the wallet lists, AND the
     withdrawal estimate cache for that address.

  8. WADR-046 — the needs-resources worklist gets the delegate action per row.
     That worklist was deliberately built without it, waiting for this task. Add
     the action; change nothing else about that view.

THE SIX INVARIANTS:
  INV-1  NO RETRY, RESUME, RE-BROADCAST OR AUTOMATIC RE-SEND on either action.
         Delegate is fund-moving; treat it exactly as the wizard treats a
         withdrawal.
  INV-2  MONEY IS A STRING. `chain_raw` and `confirmed_raw` are base-unit
         strings — echo them, never coerce, compare or subtract them. A test
         fails the build on this.
  INV-3  `confirmed` and `pending` balances are never merged.
  INV-4  NO SECRET IN THE BROWSER. The TOTP code goes to the BFF in the request
         body over TLS per BFF-041 and is moved into `X-TOTP` by the proxy —
         follow the existing pattern exactly. Never store it, never log it,
         never put it in a URL.
  INV-5  NO BUSINESS LOGIC IN THE CLIENT. Never decide whether drift is real,
         whether a delegation is needed, or how much to delegate beyond what the
         operator enters and the API validates.
  INV-6  UTC labelled in visible text wherever a UTC day is involved.

DESIGN BRIEF — a financial operations console. Dark mode default, density over
whitespace, tabular monospace for every amount, address and raw value, the
six-level severity palette only, icon alongside warning and critical, no
decorative motion, keyboard reachable with visible focus rings.
  - `drift_detected` is CRITICAL severity: the ledger and the chain disagree
    about how much money is at that address.
  - The delegate dialog must read as a fund-moving action — same visual weight
    as the withdrawal confirmation, not the weight of a settings toggle.
  - The clear-drift dialog must NOT read as a fix. It is an acknowledgement, and
    the wording and styling should make an operator hesitate.

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean (NOT `npx tsc`)
  - `npm run lint` clean
  - `npm test` green — 4/4, no test edited
  - `npm run build` succeeds and `.env` is back where it was
  - every requirement ID above is satisfied and you can point to where
  - `grep -rniE "retry|resume|re-?broadcast|try ?again|resend|re-?send"` over
    your files returns NOTHING
  - neither dialog preselects a resource type, and neither offers a second
    attempt on any outcome

YOU MUST NOT:
  - preselect or default the delegate resource type
  - offer to try again after any delegate outcome
  - describe clear-drift as correcting, fixing or repairing a balance
  - compute or compare any raw balance value
  - put the TOTP code anywhere the existing pattern does not
  - add a runtime dependency
  - edit any test
  - modify anything under `backend/`
  - commit, push, or change git branches
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line
  - the exact clear-drift wording about not correcting the balance, and the
    exact delegate wording about being broadcast once, both quoted
  - the grep output for the retry-language scan
  - anything you could not do, and why
