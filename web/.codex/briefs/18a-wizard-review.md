ROLE: AUDITOR
TASK-ID: 18a-wizard-review
GOAL: Independently audit the withdrawal path against §11.0 and INV-1. Write NO code. Report findings with file:line.

You are working in the repository at C:\Users\root\Desktop\tron-payment-proccesor.

YOU MUST NOT MODIFY ANY FILE. Not a typo, not a comment, not a formatting fix.
If you believe something must change, report it. An auditor that edits the code
it is auditing has destroyed the only independent check in the process.

WHY THIS AUDIT EXISTS: the withdrawal wizard was built and then reviewed by the
orchestrator, which found and fixed two defects. One of them was a double-payout
path. A review by the same party that wrote the brief is not independent, and
this is the one screen where being wrong costs real money that cannot be
recovered. You are the second pair of eyes. Assume the previous review missed
something and go looking for it.

READ FIRST, FULLY:
  web/docs/specs/11-withdrawals.md — §11.0 is the standard you are auditing
    against. WWD-001 through WWD-007, plus WWD-080..WWD-087 and the newly added
    WWD-086a and WWD-086b.
  backend/docs/specs/13-withdrawal-engine.md §13.0
  backend/internal/api/withdrawals.go — the create and estimate handlers
  backend/internal/store/withdrawals.go — CreateWithdrawal in full, especially
    its final lines
  web/app/api/payd/[...path]/route.ts — the proxy: what it forwards, what it
    refuses, what it does on timeout
  web/lib/query.ts — the query client's retry policy

AUDIT THESE FILES:
  web/app/(dash)/withdrawal-wizard.tsx
  web/app/(dash)/withdrawal-wizard-steps.tsx
  web/app/(dash)/withdrawals/new/page.tsx
  web/app/(dash)/withdrawal-detail.tsx
  web/app/(dash)/withdrawals-dashboard.tsx
  web/components/forms/confirm-dialog.tsx
  web/components/forms/totp-field.tsx
  web/app/api/payd/[...path]/route.ts

THE QUESTIONS TO ANSWER, EACH WITH file:line EVIDENCE:

  1. IDEMPOTENCY KEY. Where is it generated? Can any code path produce a SECOND
     key for what the operator would perceive as the same transfer? Trace every
     caller that clears or sets it. Specifically: after a submission whose
     outcome is unknown, is there ANY route — button, link, navigation, remount,
     state reset, React key change, route change and back — that yields a fresh
     key? A component that remounts and re-initialises its state is a fresh key
     just as surely as a button is.

  2. AMBIGUOUS OUTCOMES. Which HTTP statuses and failure modes reach the
     ambiguous panel, and which do not? Check the classification against what
     the backend can actually return AFTER writing the row. Read the tail of
     `store.CreateWithdrawal`: it commits, then re-reads. Anything that can fail
     after that commit is ambiguous no matter what status it carries. Is the
     classification complete? Is there any status that is treated as definite
     but could arrive after a write?

  3. AUTOMATIC RE-SEND. Does anything re-send a POST? Look for: react-query
     retry settings, `refetchOnMount`, `refetchOnWindowFocus`, or
     `refetchOnReconnect` reaching a mutation; a `useEffect` that fires a
     mutation on a dependency change; a component that submits on remount; a
     form that submits on Enter; a debounced or repeated handler. Check the
     proxy too — does it ever issue more than one upstream request for one
     inbound POST, including on redirect following?

  4. THE TOTP CODE. Where does it travel? Confirm it is a header and never a
     body field, a query parameter, or part of a URL. Is it cleared after every
     error path, including the ambiguous one? Can it be read back out of any
     component state after submission? Does it appear in any log line?

  5. THE CONFIRMATION. Does every value shown come from the estimate response
     rather than the form? Check each of source, destination, asset, amount,
     projected energy source and projected cost individually. A single field
     read from form state defeats the purpose of the echo.

  6. MONEY ARITHMETIC. Any coercion, comparison or arithmetic on an amount
     anywhere in these files — including in validation, sorting, a zero-check, or
     a disabled-button condition. There is a test for this but it has an
     allowlist; check the allowlist has not been widened and that the detector's
     money-name list actually covers the fields these files use.

  7. ANYTHING ELSE THAT WOULD MOVE MONEY TWICE, or move it to the wrong place,
     or tell an operator something false about whether money moved. This is the
     question that matters most. The first six are a checklist; this one is the
     job.

HOW TO REPORT:
  - One section per finding: severity (CRITICAL / HIGH / MEDIUM / LOW), the
    file:line, what is wrong, and the concrete sequence of events that produces
    the bad outcome. "This looks fragile" is not a finding; "an operator who does
    X after Y gets two payouts" is.
  - CRITICAL means money can move twice, move to the wrong destination, or an
    operator can be told a transfer did not happen when it did.
  - Then a short section listing what you verified as CORRECT, with file:line, so
    the next reader knows what has already been checked and does not redo it.
  - If you find nothing critical, say so plainly. Do not manufacture findings to
    look thorough, and do not soften a real one to avoid alarm.
  - State explicitly which of the seven questions you could not fully answer and
    why.

Finally, run and report the output of:
  grep -rniE "retry|resume|re-?broadcast|try ?again|resend|re-?send" web/app web/components
Every hit must be either an IPN redelivery control (permitted by WIPN-001), a
read-only GET refetch on a non-withdrawal page, or login rate-limit copy.
Anything else on a withdrawal path is a finding.
