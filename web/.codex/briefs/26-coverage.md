ROLE: AUDITOR
TASK-ID: 26-coverage
GOAL: Verify web/docs/specs/17-api-coverage-matrix.md against backend/internal/api/routes.go and actual web/ consumption. Every route consumed, or recorded unconsumed with a reason. Update the matrix ONLY — no application code.

You are working in the repository at C:\Users\root\Desktop\Projects\github\tron-payment-proccesor, on branch web-autopilot.

YOU MAY MODIFY ONLY THIS FILE:
  web/docs/specs/17-api-coverage-matrix.md
Nothing else. Not a route handler, not a page, not a schema, not the openapi.yaml,
not any other spec file. If the matrix is wrong because the CODE is wrong (a route
genuinely unconsumed, a scope mismatch between routes.go and what a page actually
sends), report it in your REPORT AT THE END — do not fix the code yourself. If you
find a genuine backend gap (a route the dashboard needs that does not exist), that is
a BACKEND CHANGE REQUEST (WP-001) — report it, do not work around it.

READ FIRST, FULLY:
  web/docs/specs/17-api-coverage-matrix.md — the file you are verifying and updating
  backend/internal/api/routes.go — the full list of 49 authenticated routes
    (`apiRoutes`) and 3 public routes (`publicRoutes`). This, not the matrix, not
    openapi.yaml, is the ground truth for what routes EXIST and their exact
    method/path/scope.
  backend/internal/api/openapi.yaml — cross-check descriptions and scopes agree with
    routes.go; if they disagree, routes.go wins for what actually runs, but the
    disagreement itself is worth reporting (it has caused real halts earlier in this
    project — see the ledger's H4 history for the pattern).

METHOD — for every one of the 52 routes:
  1. Confirm it exists in `routes.go` with the exact method, path, and scope the
     matrix claims. A route in the matrix that isn't in `routes.go` (renamed, removed)
     or a scope that doesn't match is a matrix error — fix it in the matrix.
  2. Find who calls it in `web/`. Use the Grep tool (not shell grep — this
     environment's shell grep has silently mangled quoted patterns before and cost a
     wasted run on an earlier task; verify anything ambiguous by opening the file)
     for the route's path segment across `web/app` and `web/lib`. The dashboard
     proxy strips the `/api/v1/` prefix internally (see `web/lib/payd/allowlist.ts`
     and `web/app/api/payd/[...path]/route.ts`), so search for the route's SHORT
     form as it appears in a `paydRequest([...])` call, e.g. `["reports","volume"]`
     for `/api/v1/reports/volume`, not the full path string.
  3. If you find a consumer, confirm the matrix's "Page" column names the RIGHT page
     — three new pages shipped since this matrix was last touched (`23-reports`,
     `24-system`, and card-view/polish fixes from `25-polish`) and the matrix's
     existing entries for `/reports/*`, `/export/*`, `/config`, `/audit`, `/workers`,
     `/assets`, `/chain/quota` were written to ANTICIPATE those pages before they
     existed — verify the anticipation matches what actually got built, since a page
     name or approach can drift from an early plan during implementation.
  4. If you find NO consumer for a route, that is a real finding for the "Routes
     deliberately not consumed" section — do not assume the matrix's existing "None"
     is still true without having actually checked.
  5. If a route's true scope differs from what the matrix claims (compare `routes.go`
     directly, not the matrix's own memory of it), fix the matrix — routes.go is
     authoritative, per this file's own stated "Source of truth" line.

SPECIFIC THINGS TO CHECK, EACH A CLASS OF DRIFT THAT HAS ALREADY HAPPENED ONCE IN
THIS PROJECT (see the ledger's contract-repair history for `09-overview`,
`12-addresses-read`, `15-payments-work` — each found a doc/code mismatch of exactly
this shape):
  - `/reports/volume`'s bucket-response shape was tightened in `openapi.yaml` on
    2026-08-21 (commit `bc1cfa6`'s predecessor) to match `internal/api/reports.go`
    exactly — confirm this file's entry still correctly says "Reports → volume".
  - `/config`'s response gained a `resources` block that went undocumented in
    `openapi.yaml` for a while (fixed commit `bc1cfa6`) — confirm the matrix's
    `/config` row correctly reflects it now being consumed by `System → config tab`
    (built in `24-system`), not just "thresholds elsewhere" as it may currently say.
  - The TOTP annotations ("+ TOTP") on `wallets/{address}/delegate`,
    `wallets/{address}/clear-drift`, `POST /withdrawals`, and
    `POST /withdrawals/{id}/resolve` are application-level facts routes.go's `scope`
    field does not carry (TOTP is checked separately from OAuth-style scope) — verify
    each is still accurate by reading the four handlers directly
    (`backend/internal/api/withdrawals.go`, `wallets.go`), not by trusting the
    existing annotation.
  - `GET /metrics` (`WSYS-062`) — confirm the matrix's note "link only, never parsed"
    is still true: grep `web/` for any fetch of `/metrics` or `metrics` as a
    `paydRequest` path segment. There should be none; `System → health tab` should
    render a plain link.
  - Confirm the header count line ("**49 authenticated routes + 3 public routes. All
    52 are consumed.**") is still numerically correct after your audit — recompute
    it, do not just leave it.

DONE WHEN:
  - Every one of the 52 routes in `routes.go`/`publicRoutes` has exactly one row in
    the matrix, with the correct method, path, scope, page, and spec ID.
  - Every row's "Page" column is verified against an actual `paydRequest`/`fetch`
    call site in `web/`, not assumed from the row already being there.
  - The "Routes deliberately not consumed" and "Routes the dashboard needs and the
    backend does not have" sections are accurate, not just left as "None" by default.
  - The header count line is recomputed and correct.
  - You have NOT modified any file other than the matrix.

YOU MUST NOT:
  - modify anything under `backend/`
  - modify any file under `web/` other than the one matrix file
  - fix a code-level finding yourself — report it
  - add a runtime dependency
  - commit, push, or change git branches
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - a diff-shaped summary of every change you made to the matrix, with why
  - every route you verified as CORRECTLY documented (a short list is fine — "all
    but the following N were already correct")
  - any route found unconsumed, and where you looked before concluding that
  - any scope, page, or TOTP-annotation mismatch you found and fixed
  - any genuine backend gap (a route the dashboard needs but does not exist) — this
    is a BACKEND CHANGE REQUEST, do not invent a client-side workaround
  - anything you could not fully verify, and why
