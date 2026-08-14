ROLE: AUDITOR
TASK-ID: 22-noretry-audit
GOAL: Audit the ENTIRE WP3 diff against WWD-001–WWD-007 and INV-1. Write NO code. A single violation fails the phase.

You are working in the repository at C:\Users\root\Desktop\tron-payment-proccesor.

YOU MUST NOT MODIFY ANY FILE. Not a typo, not a comment, not a formatting fix.
If something must change, report it. An auditor that edits the code it audits
has destroyed the only independent check in this process.

WHAT YOU ARE AUDITING AND WHY:

WP3 built everything that moves money out of this system: the withdrawal create
wizard, the `needs_operator` resolve flow, the two TOTP-gated address actions
(delegate broadcasts a transaction; clear-drift does not), and the resources page
that sits one click from all of them.

The guarantee under audit has one failure mode: A CUSTOMER IS PAID TWICE, with a
clean audit trail on both payments and no way to claw either back. The backend
never retries a fund-moving action, and that guarantee is only as strong as the
interface in front of it.

Two defects of exactly this class have already been found in this code — one by
the build review, one by a later independent audit:
  - a 500 arriving AFTER the row was committed was classified as a definite
    failure, and offered the operator a fresh idempotency key;
  - the idempotency key lived in component state only, so navigating to the
    withdrawal list and back — which the ambiguous panel instructs the operator
    to do — minted a new one.
Both are fixed. Assume a third exists and go find it.

READ FIRST, FULLY:
  web/docs/specs/11-withdrawals.md — §11.0 in full, plus WWD-080..WWD-087,
    WWD-086a, WWD-086b, and §11.4
  web/docs/specs/10-addresses.md §10.3 and §10.6
  web/docs/specs/12-resources-and-energy.md — WRES-043 in particular
  backend/docs/specs/13-withdrawal-engine.md §13.0
  backend/internal/api/withdrawals.go — create, estimate, resolve
  backend/internal/store/withdrawals.go — CreateWithdrawal in full, especially
    the commit-then-read tail
  backend/internal/api/wallets.go — delegate and clear-drift
  web/lib/query.ts — the client's retry policy
  web/lib/payd/client.ts and web/app/api/payd/[...path]/route.ts — the proxy

THE WP3 SURFACE TO AUDIT:
  web/app/(dash)/withdrawal-wizard.tsx
  web/app/(dash)/withdrawal-wizard-steps.tsx
  web/app/(dash)/withdrawal-resolve.tsx
  web/app/(dash)/withdrawal-needs-operator.tsx
  web/app/(dash)/withdrawal-detail.tsx
  web/app/(dash)/withdrawals-dashboard.tsx
  web/app/(dash)/address-delegate.tsx
  web/app/(dash)/address-clear-drift.tsx
  web/app/(dash)/resource-grants.tsx
  web/app/(dash)/resource-purchases.tsx
  web/app/(dash)/resources-dashboard.tsx
  web/components/forms/confirm-dialog.tsx
  web/components/forms/totp-field.tsx
  web/lib/payd/client.ts, web/app/api/payd/[...path]/route.ts, web/lib/query.ts

THE QUESTIONS. Answer each with file:line evidence.

  1. WWD-001 — is there any control anywhere in the audited surface that
     retries, resumes, re-broadcasts or re-signs an existing withdrawal, grant or
     delegation? Include disabled controls, commented-out code, menu items,
     keyboard shortcuts, and anything whose LABEL implies it even if its
     behaviour does not.

  2. WWD-002 — can any fund-moving mutation be re-sent WITHOUT a fresh deliberate
     operator action? Check every one of: react-query `retry`, `refetchOnMount`,
     `refetchOnWindowFocus`, `refetchOnReconnect` reaching a mutation; a
     `useEffect` that fires a mutation on a dependency change; a component that
     submits on mount or remount; Enter-key submission; a double-click that is
     not guarded; the proxy issuing more than one upstream request for one
     inbound POST, including via redirect following.

  3. THE IDEMPOTENCY KEY (WWD-005, WWD-075). Where is it generated, where is it
     stored, where is it cleared? Enumerate every route by which a SECOND key can
     exist for what an operator would consider the same transfer: a button, a
     navigation, a remount, a React key change, a browser back/forward, a new
     tab, a second window, an expired session, sessionStorage being cleared or
     unavailable. For each, say whether it can produce a second payout and why.

  4. AMBIGUOUS OUTCOMES (WWD-006, WWD-086, WWD-086a, WWD-086b). Which statuses
     and failure modes reach the ambiguous panel? Cross-check against every path
     the backend can fail on AFTER writing a row — read the tail of
     `store.CreateWithdrawal`. Is any status treated as definite that could
     arrive post-write? Does the ambiguous panel expose any route to a fresh key,
     directly or by navigation?

  5. THE OTHER FUND-MOVING ACTION. `delegate` broadcasts a transaction and is
     never retried (RES-013). Does its UI hold the same line as the wizard —
     ambiguous classification, no second attempt, no success claimed from a 2xx?
     And does `clear-drift`, which moves nothing, avoid claiming it fixed
     anything?

  6. THE TOTP CODE. Header only, never a body to payd, never a URL, never
     storage, never a log. Cleared after every error path including ambiguous.
     Check the proxy strips it from the browser-to-BFF body.

  7. WRES-043 — the grants table offers no retry, re-broadcast or resend, and
     says an unresolved grant resolves on chain.

  8. ANYTHING ELSE THAT COULD MOVE MONEY TWICE, move it to the wrong place, or
     tell an operator something false about whether money moved. This is the
     question that matters. The first seven are a checklist; this one is the job.

REPORT FORMAT:
  - One section per finding: severity (CRITICAL / HIGH / MEDIUM / LOW),
    file:line, what is wrong, and the concrete sequence of operator actions that
    produces the bad outcome. "This looks fragile" is not a finding. "An operator
    who does X after Y gets two payouts" is.
  - CRITICAL = money can move twice, move to the wrong destination, or an
    operator can be told a transfer did not happen when it did.
  - Then a VERIFIED-CORRECT section with file:line, so the next reader does not
    redo your work.
  - State plainly if you find nothing critical. Do not manufacture findings to
    look thorough, and do not soften a real one to avoid alarm.
  - List which questions you could not fully answer, and why.

MECHANICAL SCANS — run these and paste the RAW output, then interpret it:
  grep -rniE "retry|resume|re-?broadcast|try ?again|resend|re-?send" web/app web/components web/lib
  grep -rn "retry:" web/lib web/app
  grep -rniE "sessionStorage|localStorage" web/app web/components web/lib
Every hit in the first must be an IPN redelivery control (permitted by
WIPN-001), a read-only GET refetch on a non-withdrawal page, or login
rate-limit copy. Anything else on a fund-moving path is a finding.

A NOTE ON YOUR TOOLING: a shell `grep` whose pattern contains double quotes is
unreliable in this environment and has already produced a false negative that
cost a wasted run. When a pattern needs a quote character, verify the result by
opening the file and reading the line rather than trusting an empty result.
