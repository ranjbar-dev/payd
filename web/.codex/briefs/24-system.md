ROLE: PAGE
TASK-ID: 24-system
GOAL: Build the /system page: Workers, Quota, Config, Assets, Audit, Session, and Health/metrics tabs.

You are working in the repository at C:\Users\root\Desktop\Projects\github\tron-payment-proccesor, on branch web-autopilot.

READ FIRST, FULLY:
  web/docs/specs/15-system-and-audit.md — the whole file
  web/docs/specs/04-auth-and-session.md — AUTH-032 (scope-gated disabled controls),
    AUTH-033, AUTH-003, AUTH-050
  web/docs/specs/06-conventions.md — UI-010/UI-011 (UTC labeling), UI-043 (no
    client-side re-sort of a backend-ordered list), WNG-006, WNG-009
  backend/internal/api/openapi.yaml — sections for: `/workers`, `/chain/quota`,
    `/config`, `/assets`, `/audit`, `/auth/whoami`, `/healthz`, `/readyz`. All of
    these are already precisely typed (no `additionalProperties: true` gaps) — the
    orchestrator tightened `/config` today (2026-08-21), adding the previously
    undocumented `resources` block and precise `withdrawal`/`tron`/`orders`
    sub-objects. Trust the schemas as written; you should not need to read Go
    handler source for field names on this task.

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/(dash)/system/page.tsx              (new — but see the naming note below,
                                               `web/app/(dash)/system/components/`
                                               already exists as a SEPARATE, unrelated
                                               route: the DESIGN task's kitchen-sink
                                               component gallery, not one of this
                                               task's seven tabs. Do not touch it, do
                                               not rename or move it, and do not treat
                                               "components" as a tab name.)
  web/app/(dash)/system-*.tsx                  (new component files, matching this
                                               repo's `<feature>-dashboard.tsx`
                                               convention)
  web/lib/payd/schemas.ts, web/lib/payd/types.ts
                                               (add quota-report and audit-log
                                               response types/schemas; tighten
                                               `configResponseSchema`'s currently-loose
                                               `assets`/`withdrawal`/`tron`/`orders`
                                               fields to match the precise openapi.yaml
                                               shapes now that they exist — `energy`,
                                               `price`, `wallet`, `resources`,
                                               `consumers` are already strict, follow
                                               that pattern for the rest)
  web/lib/query-keys.ts                       (add system-page query keys only,
                                               following the existing factory shape)
  web/lib/env.ts                              (read-only reference; if you need the
                                               PAYD_BASE_URL host for WSYS-054, see the
                                               note below — you should not need to
                                               modify this file)
Everything else belongs to another agent. If you need a change outside this list,
STOP and report it instead of making it.

REUSE, DO NOT REINVENT:
  - AUTH-032 scope gating: `useScopes()` already exists in `app/providers.tsx`
    (added by `23-reports`, committed `8914e55`). The Config tab (WSYS-024) and Audit
    tab (WSYS-045) both require `admin:read` — call `useScopes()` and render the tab
    disabled with the scope named, exactly like `export-dialog.tsx` does for its own
    scope check. Do not build a second scopes mechanism.
  - WSYS-061 (named human text for every failing readiness reason): `readinessDetail`
    in `app/(dash)/overview-dashboard.tsx` already maps `/readyz` reason codes to
    human text and links, for the same requirement class (`WOVW-012`). Read it and
    reuse the same mapping — either import it if practical, or replicate the same
    code-to-text pairs locally if the function isn't exported; do not invent a
    different set of reason strings for the same codes.
  - `DataTable`, `CursorPager`, `Timestamp` (with `variant="utc-day"` for anything
    UTC-scoped per UI-010, and its ordinary local-zone rendering for everything else
    per UI-011), `Amount`, `TxidLink`/`AddressLink`, `EmptyState`, `ErrorState` all
    already exist under `components/data/`. Use them; do not build page-local
    replacements.
  - Tab navigation: no existing precedent forces one pattern. `23-reports` used
    sub-routes (`/reports`, `/reports/fees`) for its two tabs. This page has seven.
    Either sub-routes per tab (matching `/reports`'s precedent) or a single
    `/system` route with `?tab=` in the URL (matching this app's general convention
    of persisting filter/view state in the URL, `DAT-026`) is acceptable — pick one
    and apply it consistently across all seven tabs. Do not mix the two approaches
    on the same page.

WSYS-054 (show the configured PAYD_BASE_URL host and the Tronscan network): the
Tronscan half is already solved — `useTronscanBaseUrl()` gives you the origin, derive
the network name from its host (e.g. `nile.tronscan.org` vs `tronscan.org`) the same
way any other page displaying network identity would. For PAYD_BASE_URL: this value
is server-only (`lib/env.ts`'s `getEnv()`, marked `import "server-only"`) and per
`env.ts:47-49` is ALWAYS validated to be a loopback host (`127.0.0.1`, `::1`, or
`localhost`) — this dashboard's BFF always talks to a local payd instance, never a
remote one. Read it in a SERVER COMPONENT (the Session tab's page/section, following
the exact pattern `app/(dash)/layout.tsx` already uses for `TRONSCAN_BASE_URL` and
`ScopesProvider`: read server-side, pass down as a plain prop or context value, NEVER
a `NEXT_PUBLIC_` variable, NEVER the full `PAYD_BASE_URL` if it ever carried
credentials — extract only `new URL(baseURL).hostname`, never the raw configured
value, even though today that value is always loopback and contains no
credential). Do not add a new context if a plain server-to-client prop through the
Session tab's own component tree is enough; only reach for a context if multiple
unrelated components need the value the way Tronscan's does.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  WSYS-001 through WSYS-006 (workers)
  WSYS-010 through WSYS-014 (quota)
  WSYS-020 through WSYS-024 (config)
  WSYS-030 through WSYS-034 (assets)
  WSYS-040 through WSYS-046 (audit)
  WSYS-050 through WSYS-054 (session)
  WSYS-060 through WSYS-063 (health/metrics)
  UI-010, UI-011, UI-043
  AUTH-032

THE SIX INVARIANTS — these override anything you think is a better idea:

  INV-1  NO RETRY CONTROL ANYWHERE ON A FUND-MOVING PATH. This page has no
         fund-moving action at all — nothing here creates, resolves, or broadcasts
         anything. A manual "Reload" control on a read query (matching the pattern
         used on every other list page in this app) is fine; it is a deliberate
         human click on a GET, not an automatic resend.
  INV-2  MONEY IS A STRING, START TO FINISH. `daily_limit_usd`, `max_burn_trx`,
         `balance_warn_trx`, `bandwidth_topup_trx` are all decimal strings — render
         them with `<Amount>`, never `Number()`/`parseFloat()`/arithmetic. Integer
         fields (`percent_used` is a `number` in the OpenAPI schema, not a string —
         check its actual type before assuming string) may render directly but are
         never derived from other fields on this page (no client-side recomputation
         of a percentage from two other figures).
  INV-3  `confirmed` AND `pending` BALANCES ARE NEVER MERGED. Not directly relevant
         to this page's data, but if anything you touch renders a balance, the rule
         still applies.
  INV-4  NO PAYD API KEY, TOTP CODE, OR SECRET REACHES THE BROWSER. `GET /config`'s
         response is already a redacted, credential-incapable projection (WSYS-022) —
         render exactly what it returns and add nothing. `/metrics` requires an API
         key server-side; WSYS-062 forbids fetching or parsing it from the client at
         all, so there is no key-handling concern there — just a link and a note.
  INV-5  NO BUSINESS LOGIC IN THE CLIENT. Do not decide whether a worker is
         "healthy" beyond rendering the distinction the backend already computed
         (WSYS-004: fresh tick + flat error_count vs stale tick). Do not recompute
         `percent_used` or decide independently whether quota is "fine" — render the
         figure and mark the 90% threshold as a threshold, not a verdict.
  INV-6  ANYTHING SCOPED TO A UTC DAY IS LABELLED UTC IN VISIBLE TEXT. The quota
         history's `day_start` values and any audit-log date-range filter.

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean
  - `npm run build` clean, `/system` (and, if you chose sub-routes, each tab's route)
    in the route table, and `/system/components` UNCHANGED and still present
  - `npm test` still 4/4
  - every requirement ID above is satisfied and you can point to where
  - the mechanical scans below return nothing unexpected:
      grep -rniE "retry|resume|re-?broadcast|try ?again|resend|re-?send" on the files
        you touched — every hit must be a manual read-refetch control (matching the
        established `onRetry`/"Reload" pattern elsewhere) or explanatory prose, never
        an automatic resend of anything
      grep -rnE "Number\(|parseFloat|parseInt|toFixed|toLocaleString" on the files you
        touched — no hit on a money field (daily_limit_usd, max_burn_trx,
        balance_warn_trx, bandwidth_topup_trx); a hit on a genuinely non-money integer
        must be justified in your report
      grep -rn "NEXT_PUBLIC_" on the files you touched — none

YOU MUST NOT:
  - add a runtime dependency (WST-001's budget is fixed; this task needs none)
  - modify anything under `backend/`
  - fetch, parse, or render `/metrics` content (WSYS-062) — link only
  - build any control that edits, deletes, or exports an audit record (WSYS-046)
  - re-sort the audit log client-side (WSYS-041/UI-043)
  - touch `web/app/(dash)/system/components/` (a different task's route)
  - expose the raw `PAYD_BASE_URL` value or anything beyond its hostname
  - commit, push, or change git branches
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line where it is satisfied
  - which tab-navigation pattern you chose (sub-routes vs `?tab=`) and why
  - anything you could not do, and why
  - any spec ambiguity or contradiction you hit
