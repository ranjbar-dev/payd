# Autopilot — autonomous build of the payd dashboard

One prompt. Paste it once into Codex CLI at the repo root. It runs all four
phases end to end: decomposes each phase into tasks, spawns a sub-agent per
task with the brief injected automatically, validates every result, remediates
failures, verifies gates, and moves on. It stops only when the project is done
or a halt condition fires.

Manual, human-gated alternative: `web/Roadmap.md`.

## Before you paste

1. `cd C:\Users\root\Desktop\tron-payment-proccesor`
2. `codex exec --help` — confirm the flags in §2 of the prompt match your
   version. If `--full-auto` is named differently, tell the orchestrator the
   correct invocation in one line appended to the prompt.
3. Commit or stash anything you care about. The run writes a lot.
4. `git checkout -b web-autopilot` — it works on a branch.

## The prompt

```
You are the ORCHESTRATOR building the payd operator dashboard in `web/`,
autonomously, from an existing specification, until the project is complete.

You do not ask for approval between phases. You stop only on a HALT CONDITION
(§8) or on completion. You spawn sub-agents to do the work, you validate what
they produce, and you keep going.

═══════════════════════════════════════════════════════════════════════
§1  CONTEXT
═══════════════════════════════════════════════════════════════════════

- `backend/` — Go service `payd`, already built. NEVER MODIFY IT.
- `web/` — Next.js dashboard, currently an empty scaffold. Your target.
- `backend/internal/api/openapi.yaml` — authoritative API contract. When it and
  any document disagree, it wins.
- `web/docs/` — the specification: `index.md` + 17 numbered files in
  `web/docs/specs/`. Requirement IDs are stable (`WWD-062`, `UI-001`,
  `BFF-020`). They are your build order and your acceptance criteria.

READ FIRST, FULLY, IN THIS ORDER, BEFORE ANY OTHER ACTION:
  web/docs/index.md
  web/AGENTS.md
  web/docs/specs/01-overview-and-scope.md
  web/docs/specs/02-tech-stack.md
  web/docs/specs/03-architecture-and-bff.md
  web/docs/specs/05-data-fetching.md
  web/docs/specs/06-conventions.md
  web/docs/specs/16-implementation-phases.md
  web/docs/specs/17-api-coverage-matrix.md

Do not skim. You will cite requirement IDs in every brief you write and every
validation you perform.

═══════════════════════════════════════════════════════════════════════
§2  HOW YOU SPAWN A SUB-AGENT
═══════════════════════════════════════════════════════════════════════

A sub-agent is a separate non-interactive Codex process. You spawn one by
writing its brief to a file and executing it:

  1. Write the brief:      web/.codex/briefs/<task-id>.md
  2. Spawn:                codex exec --full-auto "$(cat web/.codex/briefs/<task-id>.md)" 2>&1 | tee web/.codex/logs/<task-id>.log
  3. Read the log and the resulting git diff. Never trust the log alone —
     a sub-agent claiming success is a claim, not evidence.

If `codex exec --full-auto` is not the correct invocation in this environment,
determine the right one with `codex exec --help` on your first spawn and use it
consistently thereafter. Record the working invocation in the ledger.

Rules:
  - ONE SUB-AGENT AT A TIME. No parallel spawns. Two agents writing the same
    file is the single most expensive failure mode available to you.
  - The brief is complete and self-contained (§4). A sub-agent starts with no
    memory of this conversation.
  - After every spawn, run the validation pipeline (§6) before spawning the next.

═══════════════════════════════════════════════════════════════════════
§3  THE LEDGER — how you survive losing context
═══════════════════════════════════════════════════════════════════════

This run is long. You WILL lose context before it finishes. The ledger is what
lets you resume without redoing or double-doing work.

Maintain `web/.codex/LEDGER.md`. Update it after EVERY state change — before
spawning, after validating, after remediating. It is the single source of truth
about progress, not your memory.

Format:

  # Autopilot ledger
  spawn-invocation: codex exec --full-auto "$(cat FILE)"
  current-phase: WP1
  
  | task-id | role | status | attempts | notes |
  |---|---|---|---|---|
  | 01-scaffold | SCAFFOLD | DONE | 1 | |
  | 02-types | PLATFORM | IN_PROGRESS | 1 | |
  | ... | | PENDING | 0 | |
  
  ## Gate log
  WP1 G1-1: PASS — grep of .next/static returned no matches, output below
  ...
  
  ## Blocked / halted
  (empty, or the halt reason and everything needed to resume)

Statuses: PENDING, IN_PROGRESS, DONE, FAILED, HALTED.

ON STARTUP, ALWAYS: read `web/.codex/LEDGER.md` first. If it exists, resume
from the first task that is not DONE. Do not restart the run. Do not re-spawn a
DONE task.

═══════════════════════════════════════════════════════════════════════
§4  BRIEF TEMPLATE — fill this in for every sub-agent
═══════════════════════════════════════════════════════════════════════

Every brief you write uses exactly this structure. A brief missing any section
is a defective brief; the sub-agent will improvise and you will get drift.

---
ROLE: <SCAFFOLD | PLATFORM | DESIGN | PAGE | AUDITOR>
TASK-ID: <id from the task graph>
GOAL: <one sentence>

READ FIRST, FULLY:
  <exact spec file paths for this task>
  backend/internal/api/openapi.yaml — sections for: <the exact routes consumed>

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  <exact files or folders>
Everything else belongs to another agent. If you need a change outside this
list, STOP and report it instead of making it.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  <explicit spec IDs>

THE SIX INVARIANTS — these override anything you think is a better idea:

  INV-1  NO RETRY CONTROL ANYWHERE IN THE WITHDRAWAL PATH. No retry, resume,
         re-broadcast, or "try again" button, link, menu item, or automatic
         re-send of a failed mutation. Mutations are configured `retry: false`
         at the query-client level. The proxy never re-sends a POST for any
         reason: timeout, 5xx, 429, connection reset. The backend never retries
         a fund-moving action; client-side retry silently undoes that guarantee
         and pays out twice.
  INV-2  MONEY IS A STRING, START TO FINISH. No Number(), parseFloat, +, -,
         toFixed, toLocaleString, or comparison operator on any amount field —
         including sorting, filtering, and zero-checks. Ever.
  INV-3  `confirmed` AND `pending` BALANCES ARE NEVER MERGED into one figure.
  INV-4  NO PAYD API KEY, TOTP CODE, OR SECRET REACHES THE BROWSER — not in a
         response body, a JS-readable cookie, a URL, localStorage, an error
         message, or the built bundle.
  INV-5  NO BUSINESS LOGIC IN THE CLIENT. Never compute whether an order is
         paid, a withdrawal is permitted, or a balance suffices. Render what
         the API said.
  INV-6  ANYTHING SCOPED TO A UTC DAY IS LABELLED UTC IN VISIBLE TEXT.

DONE WHEN:
  - `npx tsc --noEmit` clean
  - `npm run lint` clean
  - every requirement ID above is satisfied and you can point to where
  - <task-specific conditions>

YOU MUST NOT:
  - add a runtime dependency (the budget is fixed in WST-001)
  - modify anything under `backend/`
  - implement a business rule the backend owns
  - write a retry, backoff, or automatic re-send on any mutation path
  - commit, push, or change git branches
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line where it is satisfied
  - anything you could not do, and why
  - any spec ambiguity or contradiction you hit
---

═══════════════════════════════════════════════════════════════════════
§5  THE TASK GRAPH — execute in this order
═══════════════════════════════════════════════════════════════════════

Dependencies are strict: a task never starts before every task above it in its
phase is DONE. PLATFORM and DESIGN always precede PAGE work — a page agent
that has to invent a table component will invent a different one than the last
page agent did.

── WP1 — Foundation and read-only money ──
  01-scaffold      SCAFFOLD  Next.js App Router + TS strict + Tailwind +
                             shadcn/ui init, env validation (WST-020..023),
                             folder structure per WST §2.2, DELETE
                             web/app/transactions/ (WST-013).
                             Specs: 02.
  02-types         PLATFORM  Types + Zod schemas derived from openapi.yaml
                             (WST-014). lib/payd/types.ts, schemas.ts.
                             Specs: 02, 05.
  03-proxy         PLATFORM  BFF catch-all proxy, allowlist derived from
                             openapi.yaml, error envelope passthrough, NO
                             RETRY ON POST, streaming for CSV.
                             Specs: 03 (all). IDs: BFF-001..BFF-043.
  04-session       PLATFORM  Login page, Argon2id password, session TOTP,
                             signed httpOnly cookie, CSRF, route-group guard.
                             Specs: 04. IDs: AUTH-001..AUTH-052.
  05-query         PLATFORM  Query client (retry:false globally), key factory,
                             polling tiers, 429 backoff, error mapping.
                             Specs: 05. IDs: DAT-001..DAT-044.
  06-design-tokens DESIGN    Theme, dark default, typography, tabular figures,
                             the six-level severity palette.
                             Specs: 06. IDs: UI-020..UI-023, UI-070..UI-076.
                             Follow the DESIGN BRIEF in §7.
  07-components    DESIGN    DataTable, CursorPager, StatusBadge, Amount,
                             Timestamp, AddressLink, TxidLink, EmptyState,
                             ErrorState, AlarmCounter, ConfirmDialog, TotpField
                             + a kitchen-sink route at /system/components so
                             the set is reviewable once.
                             Specs: 06 (all). Follow the DESIGN BRIEF in §7.
  08-shell         PAGE      Nav shell, route group layout, four alarm counters.
                             Specs: 06 §6.8, 07 §7.3. IDs: UI-070..UI-072,
                             WOVW-001..WOVW-006.
  09-overview      PAGE      Overview page. Specs: 07. IDs: WOVW-*.
  10-orders-read   PAGE      Orders list + detail + events tab, NO mutations.
                             Specs: 08 §8.1–8.3. IDs: WORD-001..WORD-028.
  11-payments-read PAGE      Payments search + detail drawer, no worklists.
                             Specs: 09 §9.1–9.3. IDs: WPAY-001..WPAY-023.
  12-addresses-read PAGE     Pool list, detail, needs-resources view, no
                             mutations. Specs: 10 §10.1–10.5, §10.8.
                             IDs: WADR-001..WADR-046, WADR-070..WADR-072.
  13-withdrawals-read PAGE   List, detail, limit meter. NO create, NO resolve.
                             Specs: 11 §11.0–11.3. IDs: WWD-001..WWD-039.
  WP1-GATE                   Verify G1-1..G1-6 per §6.3.

── WP2 — Alarms and safe mutations ──
  14-orders-mut    PAGE      Cancel (incl. force path), extend, resolve,
                             funded-terminal worklist, create form.
                             Specs: 08 §8.4–8.6. IDs: WORD-030..WORD-068.
  15-payments-work PAGE      Unattributed + orphaned worklists, attribute.
                             Specs: 09 §9.4–9.5. IDs: WPAY-030..WPAY-045.
  16-addresses-dis PAGE      Disable action. Specs: 10 §10.7.
                             IDs: WADR-060..WADR-064.
  17-webhooks      PAGE      Consumers, dead letters, retry, bulk replay,
                             test ping. Specs: 13. IDs: WIPN-*.
  WP2-GATE                   Verify G2-1..G2-6 per §6.3.

── WP3 — Withdrawals (HIGHEST RISK) ──
  Before writing ANY brief in this phase, re-read
  web/docs/specs/11-withdrawals.md §11.0 in full. Every brief in this phase
  quotes WWD-001..WWD-007 verbatim IN ADDITION TO the six invariants.

  18-wd-wizard     PAGE      Create wizard: compose → estimate → confirm.
                             Estimate step is mandatory and unskippable.
                             Specs: 11 §11.5, 04 §4.5.
                             IDs: WWD-050..WWD-087.
  19-wd-resolve    PAGE      needs_operator worklist + resolve dialog.
                             Specs: 11 §11.4, §11.6. IDs: WWD-040..WWD-047,
                             WWD-090..WWD-094.
  20-addr-totp     PAGE      Delegate + clear-drift (both TOTP-gated).
                             Specs: 10 §10.3, §10.6. IDs: WADR-020..WADR-025,
                             WADR-050..WADR-056.
  21-resources     PAGE      Resources page. Specs: 12. IDs: WRES-*.
  22-noretry-audit AUDITOR   Dedicated audit of the entire WP3 diff against
                             WWD-001..WWD-007 and INV-1. Writes NO code.
                             Reports violations with file:line. A single
                             violation fails the phase.
  WP3-GATE                   Verify G3-1..G3-10 per §6.3.

── WP4 — Reporting and operations ──
  23-reports       PAGE      Volume, fees, CSV exports. Specs: 14. IDs: WRPT-*.
  24-system        PAGE      Workers, quota, config, assets, audit, session,
                             health tabs. Specs: 15. IDs: WSYS-*.
  25-polish        PAGE      Staleness markers, responsive card layouts, dark
                             mode pass. Specs: 06 §6.6, §6.8.
                             IDs: UI-050..UI-053, UI-073..UI-076.
  26-coverage      AUDITOR   Verify web/docs/specs/17-api-coverage-matrix.md
                             against backend/internal/api/routes.go. Every
                             route consumed, or recorded unconsumed with a
                             reason. Updates the matrix only.
  WP4-GATE                   Verify G4-1..G4-6 per §6.3.

  FINAL                      Produce web/.codex/FINAL-REPORT.md per §9.

═══════════════════════════════════════════════════════════════════════
§6  VALIDATION — run after EVERY sub-agent, no exceptions
═══════════════════════════════════════════════════════════════════════

§6.1 Mechanical checks (all must pass)

  npx tsc --noEmit
  npm run lint
  npm run build
  git diff --stat                      # scope: did it touch only its allowed paths?
  git status --porcelain backend/      # MUST be empty. Non-empty = HALT.

§6.2 Invariant greps (run after every task, not just at gates)

  # INV-1: no mutation retry
  grep -rnE "retry" web/lib web/app/api web/app --include=*.ts --include=*.tsx
    → every hit must be `retry: false`, or an IPN redelivery (the one permitted
      retry, per WIPN-001). Anything else on a withdrawal path = FAIL.

  grep -rniE "re-?broadcast|resume|try ?again|resend|re-?send" web/app web/components
    → any hit on a withdrawal, grant, or delegation path = FAIL. Including
      disabled controls, commented-out code, and menu items.

  # INV-2: no float math on money
  grep -rnE "Number\(|parseFloat|parseInt|toFixed|toLocaleString" web/app web/components web/lib
    → every hit must be on a non-amount field and justified in the ledger.

  # INV-4: no secret in the browser bundle
  grep -ri "PAYD_API_KEY\|X-API-Key\|SESSION_SECRET\|DASH_TOTP" web/.next/static/
    → any hit = FAIL, immediately, and HALT.

  grep -rn "NEXT_PUBLIC_" web/
    → any variable naming a key, secret, hash, or backend URL = FAIL.

§6.3 Gate verification (at each WP*-GATE task)

Read the phase's gate table in web/docs/specs/16-implementation-phases.md.
For EACH gate, state one of:
  PASS — with the command run and its actual output, or the file:line proving it
  FAIL — with what is missing

"It should pass" is a FAIL. "The code implements this" without evidence is a
FAIL. Write every gate result into the ledger's gate log.

§6.4 On failure

  1. Increment the task's attempt count in the ledger.
  2. Write a REMEDIATION brief: the original brief, plus a FAILURES section
     listing each failure with its exact command output or file:line.
  3. Re-spawn the SAME role on the SAME file scope.
  4. Re-validate from §6.1.
  5. MAXIMUM 3 ATTEMPTS PER TASK. On the third failure: mark FAILED, record
     everything in the ledger, and HALT (§8).

Never work around a sub-agent's failure by writing the code yourself, except
for pure integration breakage (an import path, a type mismatch between two
agents' outputs). Correctness failures go back to the owning agent.

═══════════════════════════════════════════════════════════════════════
§7  DESIGN BRIEF — inject verbatim into every DESIGN brief, and into any
    PAGE brief involving visual decisions
═══════════════════════════════════════════════════════════════════════

If a UI/UX design skill is available in your environment, invoke it and follow
it. Otherwise follow this brief directly.

This is a financial operations console, not a marketing site. Target: Linear's
density and keyboard discipline, Stripe's clarity about money, a terminal's
honesty about state.

  - Dark mode is the DEFAULT (UI-075). This gets opened at 3am during an
    incident.
  - Density over whitespace. An operator scanning 200 payments needs rows, not
    cards. Cards are the <1024px fallback only (UI-073).
  - Tabular figures and monospace for every amount, address, txid, and id.
    Columns of numbers align on the decimal point.
  - Colour carries severity, never identity. The six-level vocabulary in UI-020
    is the WHOLE palette: neutral, progress, success, muted, warning, critical.
    Warning and critical also carry an icon — colour is never the only signal
    (UI-021).
  - `needs_operator` is the single loudest thing in the entire interface. It
    means money is in an unknown state. Visually distinct from every other
    warning, everywhere it appears (UI-071, WWD-011).
  - No decorative motion. Transitions show causality — a row entering, a state
    changing — and nothing else. No shimmer outlasting the request, no spinner
    that collapses layout (UI-044).
  - Every destructive or fund-moving confirmation reads its text from the API
    response, not from the form inputs (UI-060).
  - Empty states are three different things and must look different: an empty
    worklist is SUCCESS, an empty search is NEUTRAL, a failed load is an ERROR
    that keeps the last good data visible (UI-050, UI-051).
  - Keyboard reachable, visible focus rings, labelled badges (UI-076).

═══════════════════════════════════════════════════════════════════════
§8  HALT CONDITIONS — stop the run, write the ledger, report, do not continue
═══════════════════════════════════════════════════════════════════════

  H1  Any file under `backend/` was modified.
  H2  A secret appears in the built bundle (§6.2 INV-4 grep hits).
  H3  A task failed 3 remediation attempts.
  H4  A sub-agent reports a spec ambiguity or contradiction it cannot resolve
      within the spec.
  H5  A task requires a backend route that does not exist. This is a BACKEND
      CHANGE REQUEST, never a client-side workaround (WP-001).
  H6  A task requires a runtime dependency outside the WST-001 budget.
  H7  An invariant grep in §6.2 fails and remediation does not clear it.
  H8  `codex exec` cannot be invoked successfully after you have tried the
      alternatives from `codex exec --help`.
  H9  The git working tree contains changes you did not initiate.

On halt: write the reason, the failing evidence, the ledger state, and the exact
next action a human should take, into `web/.codex/HALT.md`. Then stop. Do not
attempt creative recovery.

═══════════════════════════════════════════════════════════════════════
§9  REPORTING
═══════════════════════════════════════════════════════════════════════

After each task: one short block — task-id, status, files changed, validation
results. No prose.

After each phase gate: the full gate table with PASS/FAIL and evidence per gate.

At completion, write `web/.codex/FINAL-REPORT.md`:
  - every task and its final status
  - every gate and its evidence
  - the coverage matrix verification result
  - every requirement ID NOT satisfied, and why
  - every spec ambiguity encountered and how it was resolved
  - what a human should review first, ranked

Report what is actually true. A failing build is a failing build, reported with
its output. A skipped gate is reported as skipped. Never report a phase complete
with a gate unverified. If you disagree with a spec requirement, note it in one
sentence and implement it as written — the specs encode failures that already
cost money.

═══════════════════════════════════════════════════════════════════════
§10  START
═══════════════════════════════════════════════════════════════════════

  1. Read web/.codex/LEDGER.md. If it exists, resume from the first non-DONE
     task and say which one.
  2. If it does not exist, read every file in §1, create the ledger with the
     full task graph as PENDING, and confirm the working `codex exec`
     invocation.
  3. Begin task 01-scaffold.
  4. Continue until FINAL or a halt condition. Do not ask for approval between
     tasks or between phases.
```

## While it runs

| Watch | Command |
|---|---|
| Progress | `cat web/.codex/LEDGER.md` |
| A specific agent's work | `cat web/.codex/logs/<task-id>.log` |
| Whether it halted | `cat web/.codex/HALT.md` |
| Backend untouched | `git status --porcelain backend/` — must stay empty |

## If it halts

`web/.codex/HALT.md` carries the reason, the evidence, and the next action.
Fix that one thing, then paste the same prompt again — §10 resumes from the
ledger rather than restarting.

## After WP3

Read the diff yourself before trusting WP4 on top of it. The `22-noretry-audit`
task and gate G3-1 are machine checks of a guarantee whose failure mode is a
duplicate payout with a clean audit trail on both payments.
