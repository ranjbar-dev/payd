ROLE: PAGE
TASK-ID: 19-wd-resolve
GOAL: Build the `needs_operator` worklist at `/withdrawals/needs-operator` and the resolve dialog. Nothing else.

You are working in the repository at C:\Users\root\Desktop\tron-payment-proccesor.
The web app is `web/`. Run every command from `web/`.

═══════════════════════════════════════════════════════════════════════
WHAT THIS SCREEN IS
═══════════════════════════════════════════════════════════════════════

Every row on this worklist is MONEY IN AN UNKNOWN STATE. The service tried to
send it, could not determine whether it landed, and stopped. It will attempt
nothing further, ever.

The action you are building RECORDS A HUMAN DECISION. It signs nothing,
broadcasts nothing, retries nothing, resumes nothing. And the decision it
records is dangerous in one specific direction: recording `failed` for a
transaction that ACTUALLY CONFIRMED produces a double payout the moment the
operator creates a replacement withdrawal. That is why WWD-043 requires the
operator to confirm they checked the txid on chain, with the link right there in
the dialog.

Read `web/docs/specs/11-withdrawals.md` §11.0 in full before writing anything.

§11.0, QUOTED VERBATIM — THESE OVERRIDE EVERYTHING:

  WWD-001  **There MUST be no control anywhere in this dashboard that retries,
           resumes, re-broadcasts, or re-signs an existing withdrawal.** No
           button, no menu item, no keyboard shortcut, no link. The backend
           exposes no such endpoint (backend `API-015`/`WDR-000c`) and the UI
           MUST NOT invent one.
  WWD-002  **No withdrawal mutation MAY be automatically re-sent by the client
           for any reason** — timeout, network error, 5xx, or 429. Mutations
           MUST be configured `retry: false` at the query-client level.
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
           submissions.
  WWD-006  On an ambiguous submission outcome the UI MUST show the
           ambiguous-outcome panel and MUST NOT show a retry affordance.
  WWD-007  Any code review touching this page MUST verify `WWD-001`–`WWD-006`.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/11-withdrawals.md §11.0, §11.4, §11.6 — yours. §11.5 (the
    wizard) is DONE; do not touch it.
  web/docs/specs/04-auth-and-session.md — AUTH-040..AUTH-045
  backend/internal/api/openapi.yaml — POST /api/v1/withdrawals/{id}/resolve and
    GET /api/v1/withdrawals, including every `x-error-codes` entry.

READ ALSO, AS THE PATTERN TO FOLLOW:
  web/app/(dash)/withdrawal-wizard.tsx — the TOTP submission pattern, the error
    classification, and how the ambiguous panel is worded. AUTH-045 requires all
    four TOTP-gated actions to route through the ONE shared confirm component;
    the wizard is the first and set the pattern. Follow it exactly.
  web/components/forms/confirm-dialog.tsx, web/components/forms/totp-field.tsx
  web/app/(dash)/withdrawals-dashboard.tsx, web/app/(dash)/withdrawal-detail.tsx
  web/app/(dash)/funded-terminal-worklist.tsx — the worklist shape

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/(dash)/withdrawals/needs-operator/page.tsx
  web/app/(dash)/withdrawal-needs-operator.tsx
  web/app/(dash)/withdrawal-resolve.tsx
  web/app/(dash)/withdrawal-detail.tsx   — to mount resolve on a needs_operator
                                           record. Change nothing else there.
  web/lib/query-keys.ts                  — ADD keys only.
  web/components/forms/*.tsx             — ADDITIVE only; tasks 14–18 depend on
                                           these.
  PLUS build configuration if genuinely required.
Everything else belongs to another agent. If you need a change outside this
list, STOP and report it instead of making it.

NOTE ON RUNNING THE BUILD: `web/.env` holds three required variables that are
present but EMPTY, and Next lets empty values beat process-supplied ones. Move
`.env` aside, build with inline values, MOVE IT BACK.

`npm test` RUNS FOUR GATE CHECKS AND THEY MUST STAY GREEN. YOU MAY NOT EDIT ANY
TEST.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  WWD-040..WWD-047   (resolve)
  WWD-090..WWD-094   (worklist)
  AUTH-045 — routed through the one shared confirm component, as the wizard does.

THE SEVEN THINGS THAT WILL OTHERWISE GO WRONG:

  1. RESOLVE IS OFFERED ONLY FOR `needs_operator` (WWD-040). Not for `failed`,
     not for `rejected`, not for anything else. The backend refuses, and an
     action that appears where it cannot work teaches an operator that the
     screen is unreliable.

  2. THE FIRST LINE OF THE DIALOG SAYS WHAT THIS IS NOT (WWD-042): this records
     what happened; it does not sign, broadcast, retry, or resume anything.
     First line. Not buried under the form.

  3. THE OPERATOR MUST CONFIRM THEY CHECKED THE CHAIN (WWD-043), and the
     Tronscan link for the persisted txid must be IN the dialog. This is the
     requirement that prevents the double payout: recording `failed` for a
     transaction that actually confirmed is how an operator creates a
     replacement for money that already left.

  4. THE BODY IS EXACTLY `{"outcome": "confirmed"|"failed", "failure_reason":
     "..."}` AND THE TOTP GOES IN THE HEADER (WWD-041). A TOTP in the body
     returns 400 `totp_in_body`. `failure_reason` is required and non-empty when
     the outcome is `failed` (WWD-044).

  5. NO REPLACEMENT WITHDRAWAL FROM THIS FLOW (WWD-047). Do not offer, suggest,
     or link to creating one — not after a successful resolve, not in the
     dialog, not on the worklist. If a replacement is needed it is a separate,
     later, deliberate decision. This is the single most tempting thing to add
     here and it is explicitly forbidden.

  6. THE WORKLIST IS ONE ACTION ONLY (WWD-094). Resolve. Nothing else.
     WWD-090: `GET /withdrawals?status=needs_operator`, OLDEST FIRST.
     WWD-091: each row shows the persisted txid, the last lookup error, and the
     amount, with a direct Tronscan link.
     WWD-093: the page opens by explaining that each row is money in an unknown
     state, that the service will attempt nothing further, and that resolution
     is a human decision recorded after checking the chain.

  7. AFTER RESOLUTION (WWD-046): `resolved_by` shows `operator` and the
     PRESERVED TXID REMAINS VISIBLE. The backend keeps it deliberately. Do not
     hide it once the record is resolved — it is the evidence for the decision.
     WWD-045: show the persisted txid and last lookup error inline in the dialog
     too.

  ERROR HANDLING — copy the wizard's classification, because the same reasoning
  applies: a 5xx other than 503 means the resolve may or may not have been
  recorded, and the UI must say so rather than claim failure. A 401 means the
  TOTP was rejected and nothing was recorded. Clear the TOTP code after every
  error, and never auto-resubmit.

THE SIX INVARIANTS — these override anything you think is a better idea:
  INV-1  NO RETRY CONTROL ANYWHERE IN THE WITHDRAWAL PATH. Recording a decision
         is not a retry, and it must not be worded as one.
  INV-2  MONEY IS A STRING. No coercion or arithmetic on any amount. A test
         fails the build on this.
  INV-3  `confirmed` and `pending` balances are never merged.
  INV-4  NO SECRET IN THE BROWSER. The TOTP code goes in a header via the proxy
         and is never stored, never logged, never placed in a URL.
  INV-5  NO BUSINESS LOGIC IN THE CLIENT. Never decide what the chain outcome
         was. The operator decides; the backend records.
  INV-6  UTC labelled in visible text wherever a UTC day is involved.

DESIGN BRIEF — a financial operations console. Dark mode default, density over
whitespace, tabular monospace for every amount, address, txid and id, the
six-level severity palette only, icon alongside warning and critical, no
decorative motion, keyboard reachable with visible focus rings.
  - `needs_operator` IS THE LOUDEST THING IN THE INTERFACE (UI-071, WWD-011,
    WWD-092). This worklist is where that peaks. It must be visually distinct
    from every other warning in the dashboard.
  - AN EMPTY WORKLIST IS THE BEST NEWS ON THIS SCREEN: no money is in an unknown
    state. Make it read as success, not as an empty table.
  - The txid is the most important string on the row: monospace, full, with the
    Tronscan link immediately adjacent.

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean (NOT `npx tsc`)
  - `npm run lint` clean
  - `npm test` green — 4/4, no test edited
  - `npm run build` succeeds and `.env` is back where it was
  - every requirement ID above is satisfied and you can point to where
  - `/withdrawals/needs-operator` renders and the nav alarm counter links to it
  - `grep -rniE "retry|resume|re-?broadcast|try ?again|resend|re-?send"` over
    your files returns NOTHING
  - no path from this flow reaches the withdrawal wizard

YOU MUST NOT:
  - offer resolve on any status other than `needs_operator`
  - link to or suggest creating a replacement withdrawal
  - put the TOTP code in a request body
  - auto-resubmit anything
  - add a runtime dependency
  - edit any test
  - modify anything under `backend/`
  - commit, push, or change git branches
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line
  - the exact first line of the resolve dialog and the exact chain-check
    confirmation text, quoted
  - the grep output for the retry-language scan
  - anything you could not do, and why
