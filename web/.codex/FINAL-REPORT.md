# Autopilot final report

Branch `web-autopilot`. All four work packages complete and gated. Full evidence for
every claim below lives in `web/.codex/LEDGER.md`; this file is the summary §9 of
`AUTOPILOT.md` asks for, not a replacement for it.

Spawn mechanism changed mid-run: WP1–WP3 were built by `codex exec` sub-agents (until
its OAuth session was revoked, `H8`, cleared by switching to Claude Code's own Agent
tool — `AUTOPILOT.md` §2, commit `0640724`). WP4 (`23-reports` through `26-coverage`)
was built entirely by Agent-tool sub-agents, each independently re-validated by the
orchestrator (fresh `tsc`/`test`/`build`/secret-scan/backend-untouched checks, and a
direct read of the security-relevant files) rather than trusted from the sub-agent's
own report.

## Task status

| Task | Role | Status | Attempts |
|---|---|---|---|
| 01-scaffold | SCAFFOLD | DONE | 1 |
| 02-types | PLATFORM | DONE | 1 |
| 03-auth-foundation | PLATFORM | DONE | 4 |
| 05-query | PLATFORM | DONE | 4 |
| 06-design-tokens | DESIGN | DONE | 1 |
| 07-components | DESIGN | DONE | 1 |
| 08-shell | PAGE | DONE | 1 |
| 09-overview | PAGE | DONE | 1 |
| 10-orders-read | PAGE | DONE | 1 |
| 11-payments-read | PAGE | DONE | 1 |
| 12-addresses-read | PAGE | DONE | 2 |
| 13-withdrawals-read | PAGE | DONE | 1 |
| 13a-gate-tests | PLATFORM | DONE | 1 |
| 14-orders-mut | PAGE | DONE | 2 |
| 15-payments-work | PAGE | DONE | 1 |
| 16-addresses-dis | PAGE | DONE | 1 |
| 17-webhooks | PAGE | DONE | 2 |
| 17a-session-expiry | PLATFORM | DONE | 1 |
| 18-wd-wizard | PAGE | DONE | 3 |
| 19-wd-resolve | PAGE | DONE | 1 |
| 20-addr-totp | PAGE | DONE | 1 |
| 21-resources | PAGE | DONE | 1 |
| 22-noretry-audit | AUDITOR | DONE | 1 |
| 23-reports | PAGE | DONE | 1 |
| 24-system | PAGE | DONE | 1 |
| 25-polish | PAGE | DONE | 1 |
| 26-coverage | AUDITOR | DONE | 1 |
| WP1-GATE | GATE | DONE | 1 |
| WP2-GATE | GATE | DONE | 1 |
| WP3-GATE | GATE | DONE | 1 |
| WP4-GATE | GATE | DONE | 1 |

No task was marked FAILED. Two tasks (`19-wd-resolve`, `20-addr-totp`) landed via
direct commits without their ledger rows being updated at the time — reconciled
2026-08-21 against the actual diff during `22-noretry-audit`, not re-spawned.
`22-noretry-audit` itself had a brief written on 2026-08-14 but was never actually
run until 2026-08-21 (no log existed) — run then, directly, with no CRITICAL finding.

## Gates

| Gate | Result | Notes |
|---|---|---|
| WP1-GATE (G1-1..G1-6) | 5 PASS, 1 PASS WITH DEBT | G1-6's debt (AUTH-023, session-expiry warning) closed by `17a-session-expiry` before WP3 |
| WP2-GATE (G2-1..G2-6) | 6 PASS | G2-5 failed on first pass (dead-IPN alarm counter not invalidated after retry), remediated same day |
| WP3-GATE (G3-1..G3-10) | 10 PASS | G3-1 backed by `22-noretry-audit`'s full independent audit; no CRITICAL or double-payout-capable defect found |
| WP4-GATE (G4-1..G4-6) | 6 PASS | Full file:line evidence in the ledger's WP4 gate log |

## Coverage matrix result

`26-coverage` (commit `c92ee7e`) audited all 52 routes (49 authenticated + 3 public)
against `backend/internal/api/routes.go` and actual `web/` call sites. Ten matrix rows
corrected: nine were missing a genuine additional consumer (e.g. `/wallets` is also
read by the withdrawal wizard's known-destination check; `/chain/params` also feeds
Overview's readiness card); one was a real misattribution — `WOVW-052`/"Overview
volume card" was recorded against `/stats`, but the spec explicitly forbids `/stats`
from backing that card and names `/reports/volume` instead. Independently confirmed
by the orchestrator reading `overview-dashboard.tsx:113,121,171`. Zero unconsumed
routes, zero backend gaps found.

## Requirement IDs not satisfied

None found unmet as written. Three items are flagged for a human decision rather than
closed by a task, because none of them is a violation of a stated requirement — each
is either out of an already-closed task's scope, or a design tension the spec doesn't
resolve:

1. **`funded-terminal-worklist.tsx`** has the same defect `25-polish` fixed elsewhere
   twice (bare error text, no retry control, no staleness marker — UI-051/UI-052) but
   the file was not in that task's allowed-paths list, so it was reported, not fixed.
   Low severity: a read-only worklist losing its staleness/retry affordance, not a
   fund-moving path.
2. **`scope-banner.tsx`** hardcodes `bg-amber-300 text-slate-950` instead of the
   design-token classes every other component uses (UI-075's dark-mode-consistency
   spirit). Found during `25-polish`, out of that task's scope, not fixed.
3. **`POST /withdrawals`'s TOTP enforcement is conditional server-side**
   (`if cfg.RequireTOTP`, `backend/internal/api/withdrawals.go:73`), unlike the other
   three TOTP-gated mutations (`resolve`, `delegate`, `clear-drift`) which enforce it
   unconditionally. Spec `WWD-072` reads as if a payd code is always required, and the
   dashboard itself always collects and sends one regardless of server config — so
   there is no dashboard-side gap today. But a `payd` deployment configured with
   `RequireTOTP=false` would accept a withdrawal-creation request with no second
   factor even from a client that skipped the wizard and called the API directly.
   Found during `26-coverage`, reported rather than resolved (it is a backend
   configuration/spec question, not a matrix or dashboard defect).

None of the three is a violation of INV-1 through INV-6, and none was reachable from
the audited WP3 UI surface with a fresh transfer key — they are all either
out-of-scope polish debt or a backend-config question outside this run's mandate.

## Spec ambiguities encountered and how they were resolved

The full list, with resolutions, is in the ledger (search `H4` and `CLEARED`) — eight
were hit and cleared during WP1–WP3, mostly a specification asserting an API field
that did not yet exist (each time, the missing field was found already loaded in the
Go store layer and simply never serialized, then exposed rather than the spec being
weakened). Two more surfaced during WP4, both resolved in-run without a halt:

- **`23-reports`**: the brief asserted the UTC-labelled `<input type="date">`
  convention (`orders-dashboard.tsx`'s `created_from`/`created_to`) had been "shipped
  twice." The worker found only one instance — `payments-dashboard.tsx`'s date filter
  actually does the opposite (local-time entry, resolved UTC range displayed), which
  is what `WRPT-006`'s literal text asks for. Per its brief, the worker built the
  report's date range using the UTC-label convention anyway (for consistency with the
  more common pattern) rather than inventing a third approach, and reported the
  discrepancy instead of resolving which convention is "correct." **Unresolved,
  flagged for a human:** the two existing date-filter conventions in this codebase
  disagree with each other, and `23-reports` now follows one of them.
- **`25-polish`**: the orchestrator's own pre-audit, written into the brief, claimed
  `withdrawals-dashboard.tsx` was missing a UI-073 card fallback. It was wrong — the
  cards existed since the file's original commit, a week before the pre-audit note.
  The worker verified this via `git blame` rather than trusting the brief, found the
  real gap instead (`payments-dashboard.tsx` had none), and fixed that. No time lost
  beyond the verification itself; corrected in the ledger.

## What a human should review first, ranked

1. **The `POST /withdrawals` conditional-TOTP asymmetry** (item 3 above). This is the
   only item on this report that touches the withdrawal path at all, and it's a
   config question, not a code defect — worth a deliberate decision about whether
   `RequireTOTP` should be removable from the operator's YAML at all, or whether the
   backend should simply not offer the option.
2. **The two disagreeing date-filter conventions** (`orders-dashboard.tsx` vs.
   `payments-dashboard.tsx`) — cosmetic today, but the kind of inconsistency that
   compounds every time a new page copies "whichever one is closest."
3. **`funded-terminal-worklist.tsx`'s missing retry/staleness UI** and
   **`scope-banner.tsx`'s hardcoded colors** — both low-severity, both already
   identified with exact fixes available (mirror what `25-polish` already did to the
   near-identical files next to each), good first tasks for a follow-up pass.
4. **Everything else** is gated, independently re-verified by the orchestrator rather
   than taken on a sub-agent's word, and has no known open defect.
