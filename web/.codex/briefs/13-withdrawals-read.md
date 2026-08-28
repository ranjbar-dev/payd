ROLE: PAGE
TASK-ID: 13-withdrawals-read
GOAL: Build the read-only withdrawals list, the withdrawal detail page, and the daily limit meter. NO create, NO resolve, NO action that moves money or records a decision.

You are working in the repository at C:\Users\root\Desktop\tron-payment-proccesor.
The web app is `web/`. Run every command from `web/`.

THIS IS THE HIGHEST-STAKES PAGE IN THE DASHBOARD. Read §11.0 below in full
before you write a line. A well-intentioned resilience improvement on this page
is a defect, and the failure mode is paying a customer twice with a clean audit
trail on both payments.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/02-tech-stack.md
  web/docs/specs/05-data-fetching.md
  web/docs/specs/06-conventions.md
  web/docs/specs/11-withdrawals.md — §11.0, §11.1, §11.2, §11.3 are yours. §11.4
    (resolve), §11.5 (create wizard) and §11.6 (needs_operator worklist) are NOT:
    they are tasks 18-wd-wizard and 19-wd-resolve. READ §11.0 REGARDLESS — it
    binds every task that touches this page.
  backend/docs/specs/13-withdrawal-engine.md §13.0 — required reading, stated in
    the spec header.
  backend/internal/api/openapi.yaml — sections for: GET /api/v1/withdrawals,
    GET /api/v1/withdrawals/{id}, GET /api/v1/withdrawals/limits. Read the
    `Withdrawal` schema in full; it is the contract.

READ ALSO, AS THE PATTERN TO FOLLOW — orders, payments and addresses are DONE.
Match their structure, naming, and use of the shared components:
  web/app/(dash)/orders-dashboard.tsx
  web/app/(dash)/payments-dashboard.tsx
  web/app/(dash)/addresses-dashboard.tsx, web/app/(dash)/address-detail.tsx
  web/lib/query.ts, web/lib/query-keys.ts, web/lib/payd/browser-client.ts
  web/lib/payd/schemas.ts — the withdrawal schemas are already written.
  web/components/data/*, web/components/forms/*

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/(dash)/withdrawals/page.tsx
  web/app/(dash)/withdrawals/[id]/page.tsx
  web/app/(dash)/withdrawals-dashboard.tsx
  web/app/(dash)/withdrawal-detail.tsx
  web/lib/query-keys.ts              — ADD withdrawal keys if the factory lacks
                                       them. Do not change or remove an existing
                                       key; other pages use them.
  web/components/data/*.tsx          — only if an existing shared component needs
                                       a genuinely additive change. Prefer using
                                       them as they are. Never change an existing
                                       component's default behaviour.
  PLUS, whenever your change requires it, the build configuration:
    web/package.json, web/tsconfig.json, web/next.config.mjs,
    web/postcss.config.mjs, web/tailwind.config.ts, web/components.json
  If you change the module format, the compiler target, or the toolchain in ANY
  of those, you MUST bring the others into agreement in the same task and prove
  it with a passing `npm run build`. The project is `"type": "module"`; every
  config file is already ESM. Do not convert one back.
Everything else belongs to another agent. If you need a change outside this
list, STOP and report it instead of making it.

NOTE ON RUNNING THE BUILD: `web/.env` exists with three required variables
present but EMPTY, and Next lets those empty values beat process-supplied ones.
Move `.env` aside, build with inline values, and MOVE IT BACK. Do not fill it in.

═══════════════════════════════════════════════════════════════════════
§11.0 — QUOTED VERBATIM. THESE OVERRIDE EVERY OTHER RULE IN THIS BRIEF.
═══════════════════════════════════════════════════════════════════════

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
           permitted action is recording a decision (`WWD-040`) — and that
           action is NOT yours to build.
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

  WHAT THIS MEANS FOR YOU CONCRETELY, since you are building the READ side:
    - No "Retry", "Resume", "Re-broadcast", "Try again", "Resend" control on any
      withdrawal row or detail — not enabled, not disabled, not commented out,
      not in a menu.
    - An error on a GET may offer a refetch; that is reloading a view, not
      re-sending a mutation. Label it "Reload", never "Retry", so nothing on
      this page reads as re-attempting a payout.
    - WWD-004's "Create a new withdrawal" link is PERMITTED but the wizard does
      not exist yet (task 18). If you render it, it must point at
      `/withdrawals/new`, be labelled as a new and separate movement of funds,
      and be visually distinct from anything that could read as a retry. If in
      doubt, leave it out — omitting it costs nothing here.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  WWD-010, WWD-011, WWD-012, WWD-013            (status vocabulary)
  WWD-020..WWD-026                              (list and limit meter)
  WWD-030..WWD-039                              (detail)
  UI-004  — confirmed and pending figures are never merged. Deferred debt that
            lands here as well as on the addresses page.
  DAT-026 — filter state persisted in the URL query string.

NOTES ON REQUIREMENTS THAT WILL OTHERWISE COST AN ATTEMPT:

  WWD-021 — the filters are `status` AND NOTHING ELSE. `GET /withdrawals` takes
  `status`, `limit`, `cursor`; `GET /export/withdrawals.csv` takes `status` and
  `limit`. They already agree, which is all this requirement asks. Do NOT invent
  date or address filters, and do NOT filter the returned cursor page in the
  browser — that reports the page rather than the ledger and is the defect that
  cost the addresses task an attempt (`DAT-020`).

  WWD-022 — `needs_operator` rows pinned above all others. Pin by reordering the
  rows you were given; never by fetching a different page or by hiding rows.

  WWD-023 — total cost comes from the backend's `total_cost_trx`. Do NOT add
  `network_fee_trx`, `energy_cost_trx` and `bandwidth_cost_trx` together in the
  client. That is both INV-2 and UI-001, and the backend already computes it.

  WWD-025 / WWD-026 — the meter is `GET /withdrawals/limits`: used, remaining,
  cap, labelled UTC with its reset time (INV-6). It MUST say that in-flight
  withdrawals consume the allowance — `requested`, `awaiting_resources`,
  `awaiting_energy`, `signing`, `broadcast` and `confirmed` all count. An
  operator who assumes only confirmed payouts count will be surprised by a 409.

  WWD-032 — `resolved_by` values are `chain_absence`, `resource_acquisition`,
  and `operator`. An unrecognised value renders RAW. Never map it to the nearest
  known one.

  WWD-033 — the txid is shown whenever present, INCLUDING for `failed` and
  `needs_operator`. It is persisted before broadcast precisely so an ambiguous
  outcome is checkable.

  WWD-034 — the ambiguous-outcome panel is a fixed five-part statement and it
  contains no control that submits anything: the funds may or may not have
  moved; the txid with its Tronscan link is how to find out; the last lookup
  error; the service will attempt nothing further; recording an outcome is a
  decision record, not an action. The recording action itself is task 19 — state
  that a decision can be recorded, do not build the control.

  WWD-037 / WWD-038 — link to the source address (`/addresses/[address]`, which
  exists) and to the outbound payment ledger row. Payments are searchable by
  txid at `/payments?txid=...` — that page is built and that filter works. The
  resources page is task 21 and does not exist yet; a link to it is acceptable
  and expected to dangle until then, exactly as the addresses page links to the
  wizard.

  WWD-039 — tier A at 10s while non-terminal, and polling MUST STOP ENTIRELY on
  any terminal status. Not slow down. Stop.

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
         including sorting, filtering, and zero-checks. Ever. The limit meter's
         used/remaining/cap are money and come from the API as strings; render
         them, do not compute a percentage from them unless the API gives you
         one.
  INV-3  `confirmed` AND `pending` BALANCES ARE NEVER MERGED into one figure.
  INV-4  NO PAYD API KEY, TOTP CODE, OR SECRET REACHES THE BROWSER — not in a
         response body, a JS-readable cookie, a URL, localStorage, an error
         message, or the built bundle. No `NEXT_PUBLIC_` variable exists in this
         project and you must not add one (WST-020).
  INV-5  NO BUSINESS LOGIC IN THE CLIENT. Never compute whether a withdrawal is
         permitted, whether a balance suffices, or what a status means beyond
         rendering it. Never decide client-side that a withdrawal "probably
         went through".
  INV-6  ANYTHING SCOPED TO A UTC DAY IS LABELLED UTC IN VISIBLE TEXT. The daily
         cap resets on a UTC day boundary and the meter must say so.

DESIGN BRIEF — this is a financial operations console, not a marketing site.
Target: Linear's density and keyboard discipline, Stripe's clarity about money,
a terminal's honesty about state.
  - Dark mode is the DEFAULT (UI-075). This gets opened at 3am during an
    incident, and this page is why.
  - Density over whitespace. Cards are the <1024px fallback only (UI-073) and
    are NOT your task.
  - Tabular figures and monospace for every amount, address, txid, and id.
  - Colour carries severity, never identity. The six-level vocabulary in UI-020
    is the WHOLE palette: neutral, progress, success, muted, warning, critical.
    Warning and critical also carry an icon (UI-021).
  - `needs_operator` IS THE SINGLE LOUDEST THING IN THE ENTIRE INTERFACE. It
    means money is in an unknown state. It must be visually distinct from every
    other warning, everywhere it appears, including in the list (UI-071,
    WWD-011).
  - A `burned` energy source is marked as the expensive path (WWD-036).
  - No decorative motion, no progress percentage, no ETA (WWD-013). The engine
    provides neither and a fabricated one invites an operator to intervene.
  - Empty states differ: an empty list is NEUTRAL, a failed load is an ERROR
    that keeps the last good data visible (UI-050, UI-051).
  - Keyboard reachable, visible focus rings, labelled badges (UI-076).

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean (NOT `npx tsc` — it resolves to an
    older global TypeScript here and reports three false tsconfig errors)
  - `npm run lint` clean
  - `npm run build` succeeds and `.env` is back where it was
  - every requirement ID above is satisfied and you can point to where
  - `/withdrawals` and `/withdrawals/[id]` render, and the addresses page's
    existing withdrawal links resolve
  - `grep -rniE "retry|resume|re-?broadcast|try ?again|resend" ` over your files
    returns nothing but the word "Reload" and, at most, a WWD-004 "create a new
    withdrawal" link

YOU MUST NOT:
  - build the create wizard, the resolve dialog, or the needs_operator worklist
    — tasks 18, 19. No buttons, no dialogs, no placeholders.
  - add a runtime dependency (the budget is fixed in WST-001; the installed set
    is @tanstack/react-query, lucide-react, next, react, react-dom,
    react-hook-form, zod, and nothing else)
  - modify anything under `backend/`
  - implement a business rule the backend owns
  - write a retry, backoff, or automatic re-send on any path
  - commit, push, or change git branches
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line where it is satisfied
  - explicit confirmation that WWD-001..WWD-006 hold in what you wrote, with the
    grep you ran
  - anything you could not do, and why
  - any spec ambiguity or contradiction you hit
