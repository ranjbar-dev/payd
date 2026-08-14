ROLE: PAGE
TASK-ID: 21-resources
GOAL: Build the resources page — energy provider, chain parameters, resource wallet, purchases, grants, and the burn-versus-rent split.

You are working in the repository at C:\Users\root\Desktop\tron-payment-proccesor.
The web app is `web/`. Run every command from `web/`.

WHY THIS PAGE EXISTS: energy and bandwidth are what make a withdrawal possible.
When payouts stall, this is the page that says why. Its second job is quieter and
more valuable: a silently failing energy provider does not announce itself — it
shows up as rising BURN cost, money spent to send the same transfers. WRES-051
exists so that becomes visible before it becomes expensive.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/12-resources-and-energy.md — the whole file is yours.
  web/docs/specs/05-data-fetching.md and 06-conventions.md
  backend/internal/api/openapi.yaml — GET /api/v1/energy/status,
    GET /api/v1/energy/purchases, GET /api/v1/chain/params,
    GET /api/v1/resources/wallet, GET /api/v1/resources/grants,
    GET /api/v1/reports/fees, GET /api/v1/config. Read every `x-error-codes`.

READ ALSO, AS THE PATTERN TO FOLLOW:
  web/app/(dash)/addresses-dashboard.tsx, web/app/(dash)/address-detail.tsx
  web/app/(dash)/withdrawals-dashboard.tsx
  web/app/(dash)/address-delegate.tsx — the delegate action links here (WADR-055)
  web/lib/query.ts, web/lib/query-keys.ts, web/lib/payd/browser-client.ts

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/(dash)/resources/page.tsx
  web/app/(dash)/resources-dashboard.tsx
  web/app/(dash)/resource-purchases.tsx
  web/app/(dash)/resource-grants.tsx
  web/lib/query-keys.ts              — ADD keys only.
  web/components/data/*.tsx          — additive only, and only if genuinely
                                       needed. Five tasks depend on these now.
  PLUS build configuration if genuinely required.
Everything else belongs to another agent. If you need a change outside this
list, STOP and report it instead of making it.

ANCHORS OTHER PAGES ALREADY POINT AT — these must exist and must be reachable:
  - `/resources#grants` — the delegate action's ambiguous-outcome panel links
    here (WADR-054, WADR-055). Make the grants section addressable by that id.
  - The withdrawal detail page links here for energy purchases and grants
    (WWD-037). A withdrawal in `awaiting_resources` or `awaiting_energy` must be
    able to reach the grants table filtered to it (WRES-042).
  - The chain params card is linked from `chain_parameters_unavailable` in the
    wizard (WWD-064) and from WADR-044.

NOTE ON RUNNING THE BUILD: `web/.env` holds three required variables that are
present but EMPTY, and Next lets empty values beat process-supplied ones. Move
`.env` aside, build with inline values, MOVE IT BACK.

`npm test` RUNS FOUR GATE CHECKS AND THEY MUST STAY GREEN. YOU MAY NOT EDIT ANY
TEST. The money-coercion detector covers `estimated_burn_trx`, `energy_fee_sun`,
`stake_trx`, `provider_balance_trx`, `max_burn_trx`, `balance_warn_trx` and more
— every figure on this page is money and none of it may be coerced or computed.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  WRES-001..WRES-007   (energy provider)
  WRES-010..WRES-015   (chain parameters)
  WRES-020..WRES-024   (resource wallet)
  WRES-030..WRES-036   (purchases)
  WRES-040..WRES-044   (grants)
  WRES-050..WRES-052   (cost visibility)
  DAT-026 — filter state in the URL.

THE EIGHT THINGS THAT WILL OTHERWISE GO WRONG:

  1. NO CONTROLS ON THE PROVIDER CARD (WRES-007). No top-up, no purchase, no
     test call, no refresh-the-provider button. The API exposes none and the
     card is a read-only diagnostic. WRES-006: state that provider calls do not
     count against the TronGrid quota — an operator debugging quota exhaustion
     needs to know where to stop looking.

  2. A GRANT IS FUND-MOVING AND FOLLOWS THE NO-RETRY RULE (WRES-043). The grants
     table offers NO retry, re-broadcast or resend control of any kind, and it
     must SAY that an unresolved grant resolves on chain rather than being
     re-attempted. This page sits one click from the delegate action; the
     temptation to add "try again" here is exactly what INV-1 forbids.
     WRES-044: an unconfirmed grant older than a few minutes is flagged, with its
     txid linked to Tronscan.

  3. `getEnergyFee` IS A CONSEQUENCE, NOT A NUMBER (WRES-011). It is a
     governance-controlled parameter that has been raised by proposal more than
     once; at 210 sun the same transfer costs twice what it does at 100. Present
     what it means for cost, not just the integer.
     WRES-014: parameters that were NEVER populated render at CRITICAL severity —
     the service holds withdrawals rather than assuming a price, so this is the
     operator's explanation for why payouts are blocked.
     WRES-013: show the read age. WRES-012: compare worst-case burn against the
     configured ceiling.
     WRES-015: comparison figures come FROM THE BACKEND where available. Do not
     compute them here.

  4. `purchased` THAT NEVER REACHED `delegated` IS MONEY SPENT FOR NOTHING
     (WRES-032). Style the five statuses per UI-020 so that row is visible at a
     glance.
     WRES-031: quoted and actual are ADJACENT columns. A persistent gap between
     them is a provider problem, and adjacency is what makes it noticeable.
     DO NOT SUBTRACT THEM — showing both is the requirement; computing the
     difference is money arithmetic and the test will fail the build.

  5. THE RESOURCE WALLET IS THE WITHDRAWAL PATH'S DEPENDENCY (WRES-021), and it
     is permanently `disabled` in the pool (WRES-024) — link to that entry.
     WRES-022: staking and unstaking are MANUAL chain operations; the service
     never does either automatically, and unstaking has a 14-day period.
     WRES-023: show whether its TRX covers the configured bandwidth reserve.

  6. THE BURN-VERSUS-RENT SPLIT COMES FROM `GET /reports/fees` (WRES-050). Do
     not compute it here from purchases or grants. WRES-051: present it as the
     diagnostic it is — rising burn is what a silently failing provider looks
     like. WRES-052: link to the full fee report rather than growing date
     controls on this page. That report is task 23 and may dangle.

  7. POLLING: purchases are TIER D — manual refresh only (WRES-036). Purchase
     history is not a live surface, and this page is one operator has open
     during an incident. Do not put the whole page on a fast tier.

  8. FILTERS ARE SERVER-SIDE. Purchases filter by status with cursor pagination
     (WRES-035); grants filter by withdrawal, status and resource type
     (WRES-040), which is exactly what backend API-041 supports. Never filter a
     loaded cursor page in the browser — that reports the page, not the data, and
     it has already cost this project one remediation.

THE SIX INVARIANTS:
  INV-1  No retry, resume, re-broadcast or automatic re-send anywhere. Grants and
         purchases are fund-moving records; they are read here, never re-driven.
  INV-2  MONEY IS A STRING. `quoted_trx`, `actual_trx`, `balance_trx`,
         `estimated_burn_trx`, `stake_trx` and every fee figure are strings.
         Render them; never coerce, compare, sum or subtract. A test fails the
         build on this.
  INV-3  `confirmed` and `pending` balances are never merged.
  INV-4  No secret in the browser; no `NEXT_PUBLIC_` variable exists and you must
         not add one.
  INV-5  NO BUSINESS LOGIC IN THE CLIENT. Do not decide whether the provider is
         healthy, whether burn is too high, or whether a grant should have
         confirmed. Render the backend's figures and flags.
  INV-6  UTC labelled in visible text wherever a UTC day is involved.

DESIGN BRIEF — a financial operations console. Dark mode default, density over
whitespace, tabular monospace for every amount, address, txid and id, the
six-level severity palette only (UI-020), an icon alongside warning and critical
(UI-021), no decorative motion, keyboard reachable with visible focus rings.
  - Never-populated chain parameters are CRITICAL: withdrawals are blocked.
  - Five or more consecutive provider failures raises the card's severity
    (WRES-003).
  - An empty grants table is NEUTRAL, not success — it means nothing is
    currently being sourced, which is normal.
  - The quoted/actual pair should read as a comparison at a glance without the
    operator doing arithmetic in their head.

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean (NOT `npx tsc`)
  - `npm run lint` clean
  - `npm test` green — 4/4, no test edited
  - `npm run build` succeeds and `.env` is back where it was
  - every requirement ID above is satisfied and you can point to where
  - `/resources` renders and `/resources#grants` scrolls to the grants section
  - `grep -rniE "retry|resume|re-?broadcast|try ?again|resend|re-?send"` over
    your files returns NOTHING
  - no filter is applied to a loaded page in the browser

YOU MUST NOT:
  - add any control that spends money, tops up a balance, or re-drives a grant
  - compute the burn/rent split, or subtract quoted from actual
  - filter a cursor page client-side
  - add a runtime dependency
  - edit any test
  - modify anything under `backend/`
  - commit, push, or change git branches
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line
  - the exact WRES-043 wording about grants resolving on chain, quoted
  - the grep output for the retry-language scan
  - anything you could not do, and why

═══════════════════════════════════════════════════════════════════════
UNBLOCKED — read this last.
═══════════════════════════════════════════════════════════════════════

Your previous run stopped without writing a file. All four findings were correct
and all four are now closed in the API. You were right that computing any of them
client-side would have violated INV-2 or INV-5 — every one of these is money or a
rule the engine owns.

  1. WRES-002 — `GET /energy/status` now returns `balance_warn_trx` and
     `balance_low`. The comparison is made in the backend because both figures are
     decimal money strings. An unparsable or absent balance is reported NOT low,
     deliberately: unknown is not safe, so render it as unknown rather than as
     healthy.

  2. WRES-012 / WRES-015 — `GET /chain/params` now returns `worst_case_burn_trx`
     (computed from the fee actually read, over `resources.min_energy`),
     `max_burn_trx`, and `burn_exceeds_ceiling`. THE VERDICT FIELD IS OMITTED
     when either figure is unparsable; when it is absent the comparison is
     UNKNOWN and must render as unknown, never as within limits.

  3. WRES-014 — never-populated parameters are the 503 `chain_params_unavailable`
     response, not a null field: the endpoint has no row to return. Branch on
     that code and render it at critical severity with the consequence — the
     service holds withdrawals rather than assuming a price.

  4. WRES-023 — `GET /config` now carries a `resources` block with
     `bandwidth_topup_trx`, `min_energy` and `min_bandwidth`. Render the wallet's
     TRX and the reserve side by side. There is no backend verdict for this one,
     so state both figures and leave the judgement to the operator — do NOT
     compare them yourself.

  5. WRES-035 — `GET /energy/purchases` now takes a server-side `status`
     parameter (`quoted`, `purchased`, `delegated`, `expired`, `failed`), with an
     unrecognised value returning 400 `invalid_status`. Filter in the query, never
     on the loaded page.

`openapi.yaml` and the Zod schemas are already updated for all five. WRES-002,
WRES-012, WRES-014, WRES-023 and WRES-035 have been rewritten in
`web/docs/specs/12-resources-and-energy.md` to say exactly the above — re-read
them. Do not change any schema or spec file.

Everything else in the brief stands unchanged. Build the whole task now.

═══════════════════════════════════════════════════════════════════════
REMEDIATION — the page is accepted. ONE broken link, in the worst place.
═══════════════════════════════════════════════════════════════════════

Everything you built passes and stays: tsc, lint, tests 4/4, build. The
server-side filters are correct — `status` on purchases and `withdrawal_id` on
grants both go out on the query string. The WRES-043 wording is right. Do not
restructure anything.

FAILURE — the `#grants` anchor does not exist.

`resources-dashboard.tsx:75` scrolls with
`document.getElementById("grants")?.scrollIntoView()`, but NO ELEMENT IN THE PAGE
HAS `id="grants"`. The optional chaining swallows it, so nothing happens and
nothing errors.

Two callers depend on that anchor, and both are the "something went wrong with
money" path:

  - `address-delegate.tsx:44` — the AMBIGUOUS-OUTCOME panel. A delegation
    broadcast whose result is unknown tells the operator to check the grants list
    at `/resources#grants`. That is WADR-054's entire remedy, and today it drops
    them at the top of a long page with no indication of where to look.
  - `withdrawal-detail.tsx:90` — links to `/resources?tab=grants&withdrawal_id=…`
    for a withdrawal stuck in `awaiting_resources` or `awaiting_energy`
    (WRES-042). The filter works; the scroll does not.

FIX — precisely this:
  1. Give the grants section `id="grants"` so both the fragment `#grants` and
     your `?tab=grants` scroll resolve to it.
  2. Make sure a bare `/resources#grants` with NO query parameter also lands on
     the section. A fragment is handled by the browser if the element exists, so
     (1) is usually sufficient — verify it rather than assuming.
  3. When arriving with `withdrawal_id` set, the grants section must be visibly
     filtered to that withdrawal, not merely scrolled to. An operator following
     that link is asking one question: what is this withdrawal waiting on.

Nothing else changes. Re-run `./node_modules/.bin/tsc --noEmit`, `npm run lint`,
`npm test`, and `npm run build` (move `.env` aside, MOVE IT BACK). Report the
file:line of the id and confirm both entry points resolve to it.
