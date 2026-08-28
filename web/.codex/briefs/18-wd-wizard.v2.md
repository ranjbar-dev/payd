ROLE: PAGE
TASK-ID: 18-wd-wizard
GOAL: Build the withdrawal create wizard at `/withdrawals/new` — compose, then a mandatory estimate, then confirm with a payd TOTP code — and handle every submission outcome.

You are working in the repository at C:\Users\root\Desktop\tron-payment-proccesor.
The web app is `web/`. Run every command from `web/`.

═══════════════════════════════════════════════════════════════════════
THIS IS THE HIGHEST-STAKES TASK IN THE PROJECT.
═══════════════════════════════════════════════════════════════════════

This is the only screen in the dashboard that moves money out of the system. The
failure mode is not a broken page — it is a customer paid twice, with a clean
audit trail on both payments and no way to claw either back. A well-intentioned
resilience improvement here IS the defect.

Read `web/docs/specs/11-withdrawals.md` §11.0 in full before you write anything.
It overrides every other rule in every document, including anything below that
you think reads more sensibly.

§11.0, QUOTED VERBATIM — THESE OVERRIDE EVERYTHING:

  WWD-001  **There MUST be no control anywhere in this dashboard that retries,
           resumes, re-broadcasts, or re-signs an existing withdrawal.** No
           button, no menu item, no keyboard shortcut, no link. The backend
           exposes no such endpoint (backend `API-015`/`WDR-000c`) and the UI
           MUST NOT invent one.
  WWD-002  **No withdrawal mutation MAY be automatically re-sent by the client
           for any reason** — timeout, network error, 5xx, or 429. Mutations
           MUST be configured `retry: false` at the query-client level, not per
           call site (`DAT-034`, `BFF-020`).
  WWD-003  A failed, rejected, or `needs_operator` withdrawal MUST render its
           terminal reason and **no action that moves money**. The only
           permitted action is recording a decision (`WWD-040`).
  WWD-004  Where a new payout is genuinely required, the UI MAY offer "Create a
           new withdrawal", which opens the wizard **empty of an idempotency
           key** and requires the operator to pass through estimate and
           confirmation again. It MUST be visually distinct from a retry and
           MUST be labelled as a new, separate movement of funds.
  WWD-005  The `Idempotency-Key` MUST be generated once per wizard completion
           and MUST NOT be regenerated on a failed submission or reused across
           submissions. Reuse with different parameters returns 409
           `idempotency_key_reuse` (backend `WDR-003a`), which is a caller bug,
           not a retry.
  WWD-006  On an ambiguous submission outcome (timeout, 502, 504, connection
           reset), the UI MUST show the ambiguous-outcome panel (`WWD-034`) and
           MUST NOT show a retry affordance, a "resend" link, or an
           auto-refreshing error toast.
  WWD-007  Any code review touching this page MUST verify `WWD-001`–`WWD-006`.
           This is the one page where a well-intentioned resilience improvement
           is a defect.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/11-withdrawals.md — §11.0 and §11.5 are yours in full. §11.4 and
    §11.6 (resolve, needs_operator worklist) are task 19. §11.1–11.3 are already
    built.
  web/docs/specs/04-auth-and-session.md — AUTH-040..AUTH-045, the payd TOTP rules
  web/docs/specs/06-conventions.md — UI-060, UI-061, UI-062, UI-063, UI-074
  backend/docs/specs/13-withdrawal-engine.md §13.0 — required by the spec header
  backend/internal/api/openapi.yaml — POST /api/v1/withdrawals,
    POST /api/v1/withdrawals/estimate, GET /api/v1/withdrawals/limits,
    GET /api/v1/wallets/with-balance, GET /api/v1/assets. Read EVERY
    `x-error-codes` block on the create route; half this task is outcome handling.

READ ALSO, AS THE PATTERN TO FOLLOW:
  web/app/(dash)/withdrawals-dashboard.tsx, web/app/(dash)/withdrawal-detail.tsx
  web/app/(dash)/payment-attribute.tsx — the two-step confirmation pattern
  web/components/forms/confirm-dialog.tsx and web/components/forms/totp-field.tsx
    — USE BOTH. AUTH-045 requires all four TOTP-gated actions to route through
    the one shared confirm component; this is the first of them, so whatever you
    need from it must be added THERE, additively, not forked into a second dialog.
  web/app/providers.tsx — `useSessionExpiry()` returns `isExpiringSoon`,
    `isExpired`, `remainingMs`, `expiresAt`. Built last task specifically for you.

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/(dash)/withdrawals/new/page.tsx
  web/app/(dash)/withdrawal-wizard.tsx
  web/app/(dash)/withdrawal-wizard-steps.tsx
  web/lib/query-keys.ts               — ADD keys only.
  web/components/forms/confirm-dialog.tsx, web/components/forms/totp-field.tsx
                                      — ADDITIVE changes only. Never change
                                        existing default behaviour; task 14, 15
                                        and 16 all depend on these.
  PLUS the build configuration when required.
Everything else belongs to another agent. If you need a change outside this
list, STOP and report it instead of making it.

NOTE ON RUNNING THE BUILD: `web/.env` holds three required variables that are
present but EMPTY, and Next lets empty values beat process-supplied ones. Move
`.env` aside, build with inline values, MOVE IT BACK.

`npm test` RUNS FOUR GATE CHECKS AND THEY MUST STAY GREEN. YOU MAY NOT EDIT ANY
TEST. `lib/no-coercion.test.ts` fails the build on any numeric coercion or
arithmetic touching a money-named identifier — on this page that matters more
than anywhere else in the codebase.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  WWD-050..WWD-057   (compose)
  WWD-060..WWD-068   (estimate)
  WWD-070..WWD-076   (confirm and submit)
  WWD-080..WWD-087   (every submission outcome)
  UI-074 — the wizard refuses to render below 1024px. Deferred debt landing here.
  AUTH-045 — routed through the one shared confirm component. Deferred debt
             landing here, and this is the first of the four TOTP-gated actions,
             so you set the pattern the other three will follow.
  AUTH-023 — use `useSessionExpiry()`. If the session is about to expire, the
             operator must learn it BEFORE typing a TOTP code, not after the
             submission fails. Do not discard their inputs.

THE NINE THINGS THAT WILL OTHERWISE GO WRONG:

  1. THE IDEMPOTENCY KEY IS GENERATED ONCE, WHEN STEP 3 IS FIRST REACHED
     (WWD-075, WWD-005). Not per submit. Not regenerated after a failure. Not
     reused across wizard runs. A new key after a failed submission turns one
     payout into two; the same key with edited parameters is a 409
     `idempotency_key_reuse`, which is a caller bug and MUST be reported as one
     (WWD-082) — the operator starts a NEW wizard, they do not edit and resubmit.

  2. THE ESTIMATE IS MANDATORY AND UNSKIPPABLE (WWD-060). There must be no path,
     including a URL with pre-filled query parameters, that reaches step 3
     without a fresh estimate. WWD-067: if the operator goes back to step 1 and
     changes ANYTHING, the estimate is re-run and the previous result discarded.
     A stale estimate against changed parameters is worse than no estimate.
     WWD-066: `can_proceed: false` disables step 3 entirely — the operator must
     not be able to submit what the backend has already refused.

  3. TWO SUFFICIENCY VERDICTS, NEVER ONE (WWD-062).
     `confirmed_balance_sufficient` and `trx_for_resources_sufficient` are
     rendered as two separate, separately labelled verdicts. A TRC-20 transfer
     spends two balances on the source address and the remedies are different:
     deposit more of the asset, versus top the address up with TRX. Backend
     API-032 split them because collapsing them told operators the balance was
     short while the asset balance sat well above the request — sending them to
     top up the wrong one.

  4. `blocked_by` IS SEVEN SPECIFIC EXPLANATIONS, NOT AN ENUM DUMP (WWD-063).
     Each of `withdrawals_disabled`, `confirmed_balance`, `trx_for_resources`,
     `daily_usd_cap`, `energy_unavailable`, `energy_burn_limit`,
     `chain_parameters_unavailable` gets specific text and a specific next step.
     A raw enum value on a blocked payout screen is not an answer.
     WWD-064: `chain_parameters_unavailable` means the service has not read
     `getEnergyFee` yet and is HOLDING withdrawals rather than assuming a price.
     WWD-065: `energy_burn_limit` shows configured `energy.max_burn_trx` against
     the live computed burn cost — a misconfigured ceiling silently disables the
     fallback of last resort.

  5. NO "MAX" BUTTON (WWD-055). Computing it is arithmetic on money, and the
     figure would be wrong anyway because it excludes the TRX needed for
     resources. No percentage buttons either.

  6. THE SOURCE IS A SELECTOR, THE DESTINATION IS FREE TEXT (WWD-050, WWD-052).
     Source comes from `GET /wallets/with-balance` — confirmed funds only — and
     free text MUST NOT be accepted for it. Destination IS free text: the backend
     validates it (WDR-004) and the UI MUST NOT pre-validate with its own address
     library (WST-005). Show the full destination untruncated in monospace before
     submission (WWD-071) and require an explicit paste confirmation.
     WWD-053: warn if the destination matches a known pooled deposit address —
     withdrawing to your own pool is almost always a mistake.
     WWD-051: the selector shows confirmed and pending SEPARATELY per asset
     (INV-3) plus `can_withdraw` and `blocked_by`. A blocked address stays
     SELECTABLE but marked, so the operator learns why instead of wondering where
     it went.

  7. THE CONFIRMATION READS FROM THE ESTIMATE, NOT THE FORM (WWD-070, UI-060).
     Source, destination, asset, amount, projected energy source and projected
     cost all come from the estimate response. What the server understood is what
     the operator confirms.
     WWD-073: the submit button names the action and amount — "Withdraw 100.00
     USDT" — and disables on click until the response arrives.
     WWD-074: NO Enter-key submission and no keyboard shortcut anywhere in this
     flow.
     WWD-076: state that only CONFIRMED funds are spendable and pending deposits
     are not.

  8. THE FIVE OUTCOMES ARE FIVE DIFFERENT SCREENS (WWD-080..WWD-086).
     200 — an EXISTING withdrawal was returned for a repeated key and NO TOTP WAS
       CHECKED. Say so explicitly, link to the record, and do not report it as a
       new withdrawal.
     201 — navigate to the detail page; tier-A polling starts there immediately.
     409 `idempotency_key_reuse` — caller bug; a new wizard, not an edit.
     409 with `details.totp_consumed: true` — render the AUTH-043 copy: the code
       has been consumed, the request was NOT created, wait for a fresh code
       before correcting the request.
     4xx validation — map to the field that caused it (WWD-084).
     503 — distinguish stale prices from other causes and state plainly that the
       withdrawal was NOT created (WWD-085).
     TIMEOUT / 502 / 504 — the ambiguous-outcome panel (WWD-034) PLUS the extra
       instruction from WWD-086: check the withdrawal list for a row created in
       the last minute before doing anything else. The request may have reached
       payd, consumed the TOTP code, and created the row. No retry affordance, no
       resend link, no auto-refreshing toast.

  9. AFTER ANY ERROR: THE TOTP CODE IS DISCARDED AND NOTHING RESUBMITS ITSELF
     (WWD-087). The code is single-use at the backend; keeping it on screen
     invites re-submission with a code that is already burned. Clear it, keep the
     rest of the form, and require the operator to act deliberately.

THE SIX INVARIANTS — these override anything you think is a better idea:

  INV-1  NO RETRY CONTROL ANYWHERE IN THE WITHDRAWAL PATH. This page is the
         reason that rule exists. No retry, resume, re-broadcast, resend, or
         "try again" — not as a button, not as a link, not as an automatic
         refetch, not commented out for later.
  INV-2  MONEY IS A STRING, START TO FINISH. No Number(), parseFloat, +, -,
         toFixed, toLocaleString, or comparison operator on any amount field,
         including in validation, including for the daily allowance, including
         to decide whether a balance covers a request. THE BACKEND DECIDES
         SUFFICIENCY — that is what the estimate is for.
  INV-3  `confirmed` AND `pending` BALANCES ARE NEVER MERGED. The source
         selector shows both, separately labelled, per asset.
  INV-4  NO PAYD API KEY, TOTP CODE, OR SECRET REACHES THE BROWSER — and the
         TOTP code the operator types MUST go out as a header through the proxy,
         never in a URL, never in a query string, never in a body field (a body
         field returns `totp_in_body`), never in localStorage, never logged.
  INV-5  NO BUSINESS LOGIC IN THE CLIENT. Never compute whether a withdrawal is
         permitted, whether a balance suffices, or what the fee will be. Render
         what the estimate said.
  INV-6  ANYTHING SCOPED TO A UTC DAY IS LABELLED UTC IN VISIBLE TEXT — the
         daily allowance in the composer above all (WWD-056).

DESIGN BRIEF — a financial operations console. Linear's density and keyboard
discipline, Stripe's clarity about money, a terminal's honesty about state.
  - Dark mode is the DEFAULT (UI-075).
  - UI-074: below 1024px this wizard REFUSES TO RENDER. Not a squeezed layout —
    a clear message that a withdrawal must be composed on a full screen. A payout
    mis-keyed on a phone is the scenario that requirement exists to prevent.
  - Tabular figures and monospace for every amount, address and id. The
    destination address is the single most important string on the screen: full,
    monospace, untruncated, visually verifiable character by character.
  - Colour carries severity, never identity: neutral, progress, success, muted,
    warning, critical (UI-020), with an icon on warning and critical (UI-021).
  - The three steps must make it obvious which one you are on and that you cannot
    skip forward.
  - No decorative motion. No progress percentage, no ETA.
  - Keyboard reachable with visible focus rings — but NO Enter-to-submit and no
    shortcut on the submit action (UI-063, WWD-074).

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean (NOT `npx tsc`)
  - `npm run lint` clean
  - `npm test` green — 4/4, no test edited
  - `npm run build` succeeds and `.env` is back where it was
  - every requirement ID above is satisfied and you can point to where
  - `/withdrawals/new` renders and cannot reach step 3 without an estimate
  - `grep -rniE "retry|resume|re-?broadcast|try ?again|resend|re-?send"` over
    your files returns NOTHING except, at most, a WWD-004-style "create a new
    withdrawal" label that is explicitly not a retry
  - the idempotency key is generated in exactly one place, and you can point to
    the line that proves it is not regenerated on failure

YOU MUST NOT:
  - regenerate the idempotency key after a failed submission
  - auto-resubmit anything, under any condition
  - add a "max" or percentage button
  - pre-validate the destination address with your own logic
  - compute sufficiency, fees, or limits client-side
  - put the TOTP code anywhere but a request header
  - add a runtime dependency
  - edit any test
  - modify anything under `backend/`
  - commit, push, or change git branches
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line where it is satisfied
  - THE EXACT LINE where the idempotency key is generated, and the exact lines
    proving it is not regenerated on failure and not reused across runs
  - the exact text of the ambiguous-outcome panel, quoted
  - the grep output for the retry-language scan
  - anything you could not do, and why
  - any spec ambiguity or contradiction you hit

═══════════════════════════════════════════════════════════════════════
UNBLOCKED — read this last.
═══════════════════════════════════════════════════════════════════════

Your previous run stopped without writing a file, and the finding was exactly
right: WWD-070 required the confirmation to restate the transfer from the
estimate response, and that response carried only verdicts. Refusing to
substitute form state was the correct call — echoing the operator's own inputs
back to them and calling it a confirmation is precisely the failure UI-060
exists to prevent.

`POST /withdrawals/estimate` now echoes six fields, and they are the ONLY source
the confirmation may use for the transfer itself:

    from_address, to_address, asset, amount, amount_raw, amount_usd

`amount` is RE-FORMATTED FROM THE PARSED BASE UNITS, not copied from the request.
That is the point of the echo: if the operator typed something the parser
normalised, the confirmation shows what would actually move, at the last moment
before a TOTP code is entered. `amount_raw` is what a created withdrawal would
carry, and `amount_usd` is the figure that counts against the daily cap.

`openapi.yaml`, `withdrawalEstimateResponseSchema` and WWD-070 are all updated
already — do not change any of them.

WWD-070 now also says, explicitly: the wizard MUST NOT substitute its own form
state for any of the six, EVEN WHERE IT BELIEVES THEY MATCH. If they differ, the
difference is the most valuable thing on the screen.

Everything else in the brief above is unchanged and still binding — in
particular WWD-005/WWD-075 on the idempotency key, WWD-060/WWD-066/WWD-067 on
the mandatory estimate, and WWD-087 on discarding the TOTP code after any error.

Now build the whole task as briefed above.
