# Autopilot ledger

spawn-invocation: codex exec --dangerously-bypass-approvals-and-sandbox - < FILE > LOG 2>&1
current-phase: WP3

| task-id | role | status | attempts | notes |
|---|---|---|---|---|
| 01-scaffold | SCAFFOLD | DONE | 1 | `tsc`, lint, and build pass; backend clean; scoped invariant checks pass. Worker output pipe was interrupted after it wrote the scaffold, so completion was determined from the resulting diff and validation. |
| 02-types | PLATFORM | DONE | 1 | `schemas.ts` and `types.ts` added; `tsc`, lint, and build pass; backend clean; invariant scans pass. Worker output pipe was interrupted after it wrote the files, so completion was determined from the resulting diff and validation. |
| 03-auth-foundation | PLATFORM | DONE | 4 | Unblocked by human on 2026-08-13: `postcss.config.js` converted to ESM `postcss.config.mjs`, required by `"type": "module"`. Build, local `tsc`, lint, and session tests now pass. Spec cycle recorded as H4 resolved in-spec by BFF-013. |
| 05-query | PLATFORM | DONE | 4 | Human integration fix validated: local `tsc`, lint, session tests, production build, backend scope, and invariant scans pass. |
| 06-design-tokens | DESIGN | DONE | 1 | Global dark tokens, severity vocabulary, tabular typography, focus treatment, and critical salience validated by `tsc`, lint, and build. |
| 07-components | DESIGN | DONE | 1 | Sub-agent completed 12:10 and reported per-ID coverage; the orchestrator wedged before recording it and was stopped by the human at 12:40. Status set from independent verification, not from the agent's log: `./node_modules/.bin/tsc --noEmit` exit 0; all 11 components present under `components/data/` and `components/forms/`; kitchen-sink route at `app/(dash)/system/components/page.tsx`. Agent correctly reported UI-004, UI-015, UI-070, UI-073–074, DAT-026 URL persistence and AUTH-045 route adoption as outside its allowed paths — these are owed by `08-shell` and the page tasks, see Deferred requirements below. |
| 08-shell | PAGE | DONE | 1 | Fixed shell, permanent nav, stats-backed alarms, and UTC clock. Worker log was 447198 bytes; only final 20000 bytes inspected. Its reported dead-IPN URL ambiguity is resolved by `13-webhooks.md`: `/webhooks` is the dedicated dead-letter worklist. Local tsc, lint, and production build pass; backend clean. |
| 09-overview | PAGE | DONE | 1 | Read-only overview complete. Worker v3 log was 988918 bytes; only final 20000 bytes inspected. Integration fixes synchronized query parameters, accepted 503 readiness bodies, worker cadence/config schemas, and shared query keys. Local tsc, lint, and production build pass; invariant scans pass. Backend diff is the human-provided contract repair, not task output. |
| 10-orders-read | PAGE | DONE | 1 | H4 cleared by the human 2026-08-13. The view itself was correct and complete; only the Tronscan configuration policy was contradictory, and the worker's own proposed resolution was the right one and was applied. `NEXT_PUBLIC_TRONSCAN_BASE_URL` replaced by server-only `TRONSCAN_BASE_URL`, provided to the client tree through one context in `app/providers.tsx`. Verified independently: local `tsc` exit 0, production build succeeds, `NEXT_PUBLIC_` scan clean in code. |
| 11-payments-read | PAGE | DONE | 1 | H4 cleared 2026-08-14 — `withdrawal_id` added to the `Payment` response. v1 returned blocked with no files changed on two DEFECTIVE-BRIEF errors, both mine, neither a spec defect and neither counted against the attempt budget: the brief named `order.address_assigned_at`, which has never existed (the window is `created_at` to `address_released_at`, exactly as the orders page already renders it), and pointed at `web/lib/payd/query-keys.ts` when the factory is `web/lib/query-keys.ts`. Brief corrected, re-spawned as v2. The worker was right to stop rather than invent the field. v2 delivered the page and correctly refused to invent a raw amount that the contract did not carry; `amount_raw` was then serialized (see below) and v3 rendered it. Search, drawer, and URL-persisted filters complete; no worklists and no mutations, as scoped. |
| 12-addresses-read | PAGE | DONE | 2 | v1 built all four views and reported four contract gaps, three of them real and now closed in the API (`/wallets` filters, `/stats` `addresses`, `confirmed_raw`); the fourth, per-address assignment history, was a specification error and WADR-030 was rewritten. One genuine correctness failure: it filtered state/asset/drift against the loaded cursor page, the same defect WPAY-031 forbids. Attempt 2 spawned with a remediation brief. Spawned 2026-08-14. Read-only: pool list, detail, needs-resources, with-balance. Carries the deferred UI-004 debt. WADR-046's delegate action is explicitly excluded and left to 20-addr-totp. |
| 13-withdrawals-read | PAGE | DONE | 1 | Spawned 2026-08-14. Read-only list, detail, and daily limit meter. Brief quotes WWD-001..WWD-007 verbatim and states what §11.0 means concretely for a read-only page: a GET refetch is labelled "Reload", never "Retry", so nothing on the page reads as re-attempting a payout. Create wizard, resolve dialog and the needs_operator worklist are explicitly excluded. v1 stopped without writing a file on WWD-012: the contract carried no status-transition timestamp, so time-in-state could not be derived truthfully. Correct call. `status_updated_at` was already stored on every status write and simply never serialized — now exposed, WWD-012 rewritten to name it, and v2 spawned. No attempt consumed; the field did not exist when v1 ran. |
| 14-orders-mut | PAGE | DONE | 2 | Spawned 2026-08-14, first WP2 task and the first mutations in the dashboard. Brief leads on the two things most likely to go wrong rather than on the requirement list: `resolve` records a decision and moves nothing (every operator assumes otherwise), and force-cancel is a second deliberate decision that must never be auto-escalated from a 409. Also warns that `npm test` now fails the build on any money coercion and that the test may not be edited. v1 delivered everything correctly except one wrong-diagnosis branch (see below); v2 fixed it. Force-cancel verified BY READING THE CODE: a 409 `order_funded` closes the first dialog and opens a second one needing its own click — the flag is never auto-set and the mutation is never auto-resubmitted. No TOTP prompt exists in any of the three files, and both dialogs say so. |
| 15-payments-work | PAGE | DONE | 1 | Spawned 2026-08-14. Unattributed and orphaned worklists plus the attribute action. Brief front-loads the five known traps: no client-side filter on `unattributed_reason` (the defect that cost 12 an attempt), the money is credited not lost, an asset-mismatch attribution needs a second explicit step, the reason is read not recomputed, and an orphaned payment has no restore control of any kind. v1 stopped without writing a file on WPAY-043, correctly refusing to infer an order from address and asset. THE DOCUMENTATION WAS WRONG, NOT THE DATA: `RewindChain` orphans a payment with `UPDATE payments SET status='orphaned'` and touches nothing else (`store/follower.go:418`), so the attribution survives the reorg and `order_id` is present. `openapi.yaml` corrected, WPAY-043 rewritten to read the order from the payment's own `order_id` and fetch its CURRENT status. No backend behaviour change and no attempt consumed — the worker was reasoning from a schema description that did not match the code. |
| 16-addresses-dis | PAGE | DONE | 1 | Spawned 2026-08-14. One action and its dialog. The brief's weight is on WADR-064 — there is no re-enable endpoint, so a toggle is the wrong control shape entirely — and on WADR-061, because an operator who believes disabling protects the balance will disable an address and walk away from the money. |
| 17-webhooks | PAGE | DONE | 2 | Spawned 2026-08-14, last WP2 task. The page carrying the ONE permitted retry in the dashboard, so the brief opens by explaining why the exception exists and bounds it explicitly: redelivering an IPN is a notification, not a movement of funds, and the exception does not extend to auto-resending the retry request itself or to any control touching a withdrawal. Also front-loaded: `dry_run` defaults true, no automatic replay loop, no consumer secret anywhere, and a dead-letter payload is a snapshot that can contradict the order's current status. v1 stopped without writing a file: `GET /ipn/dead` returned no payload, so WIPN-031/032/033 were unbuildable. Correct finding. `payload` was stored in `ipn_outbox.payload` and never selected — the SIXTH stored-but-never-serialized field — and is now exposed. Its second ask, a `current_status` field, was declined deliberately: the dispatcher computes that at send time (`ipn/dispatcher.go:217`) and IPN-021a keeps the stored snapshot immutable, so WIPN-032 is met by fetching the order instead. No attempt consumed. |
| 17a-session-expiry | PLATFORM | DONE | 1 | NOT IN THE ORIGINAL TASK GRAPH. Added 2026-08-14 to close the AUTH-023 debt the WP1 gate recorded, and deliberately scheduled BEFORE the wizard rather than after: today a silent session expiry costs a page reload, but in a three-step wizard ending in a payd TOTP code it invites a hurried re-entry of a payout. Explicitly forbids the obvious wrong fix — no silent session renewal, no "keep me logged in", no polling; the expiry is a known timestamp passed down from a server component exactly as TRONSCAN_BASE_URL is. |
| 18-wd-wizard | PAGE | DONE | 3 | Spawned 2026-08-14. The highest-stakes task in the project. Brief opens with §11.0 quoted verbatim, then nine specific traps ahead of the requirement list — the idempotency key generated once and never regenerated on failure being the first, since that single mistake turns one payout into two. Also carries three deferred debts: UI-074, AUTH-045 (first of the four TOTP-gated actions, so it sets the pattern for 19 and 20), and the AUTH-023 surface built last task. v1 stopped without writing a file: WWD-070 requires the confirmation to restate the transfer FROM THE ESTIMATE RESPONSE, and that response carried only verdicts. Refusing to substitute form state was exactly right — echoing an operator's own inputs back and calling it a confirmation is the failure UI-060 exists to prevent. The estimate now echoes `from_address`, `to_address`, `asset`, `amount`, `amount_raw`, `amount_usd`, with `amount` RE-FORMATTED FROM THE PARSED BASE UNITS so a normalised amount is visible at the last moment before a TOTP code is typed. No attempt consumed. |
| 19-wd-resolve | PAGE | PENDING | 0 | |
| 20-addr-totp | PAGE | PENDING | 0 | |
| 21-resources | PAGE | PENDING | 0 | |
| 22-noretry-audit | AUDITOR | PENDING | 0 | |
| 23-reports | PAGE | PENDING | 0 | |
| 24-system | PAGE | PENDING | 0 | |
| 25-polish | PAGE | PENDING | 0 | |
| 26-coverage | AUDITOR | PENDING | 0 | |
| 13a-gate-tests | PLATFORM | DONE | 1 | NOT IN THE ORIGINAL TASK GRAPH. Added 2026-08-14 because G1-2 and G1-5 both say "verified by a test", and no such test existed. A gate whose wording demands a test is not passed by reading the code — that is the "it should pass" FAIL in §6.3. Writes the amount-coercion detector, the proxy single-POST behavioural test, and a session-expiry test. Each must be shown to go red when its property is broken. |
| WP1-GATE | GATE | DONE | 1 | 5 PASS, 1 PASS WITH DEBT (AUTH-023). Full evidence in the gate log. |
| WP2-GATE | GATE | DONE | 1 | All six PASS. G2-1 verified empirically against a database built from the real migrations, not by assertion. G2-5 initially FAILED on the IPN single-retry path and was remediated. |
| WP3-GATE | GATE | PENDING | 0 | |
| WP4-GATE | GATE | PENDING | 0 | |
| FINAL | REPORT | PENDING | 0 | |

## Gate log

WP2 GATE — 2026-08-14. All six PASS, one of them only after a remediation.

G2-1 PASS — verified EMPIRICALLY, not by assertion. A scratch database was built
from the real migration files (`sqlite3 < internal/store/migrations/*.sql`) in the
session scratchpad, never in the repository, and the five counter queries were run
against it directly:
  empty database   → needs_operator 0, unattributed 0, orphaned_unresolved 0,
                     funded_terminal_unresolved 0, ipn_dead 0
  one row inserted for each condition (a `needs_operator` withdrawal, an
  `unattributed` payment, an `orphaned` payment, an `expired_funded` order with
  a null resolution, a `dead` ipn_outbox row)
                   → every counter 1
  then `UPDATE orders SET resolution='written_off'`
                   → funded_terminal_unresolved back to 0
THE LAST STEP IS THE POINT. With the order resolved, the correct counter falls to
zero while the naive `status IN ('expired_funded','cancelled_funded')` grouping
still returns 1 — measured, both figures printed. That is exactly the alarm that
would never clear, and it is why `WOVW-004a` forbids the client reconstructing
this figure by addition. The UI side is `alarm-navigation.tsx:29-43`, which reads
all five straight from `GET /stats` and sums nothing but the per-consumer
`ipn_dead` map.

G2-2 PASS — force-cancel cannot be reached in one click. `order-actions.tsx`
holds two independent dialog states; the 409 `order_funded` handler closes the
first and opens the second, which needs its own confirmation. The flag is never
auto-set and no mutation is auto-resubmitted.

G2-3 PASS — the asset-mismatch path is two dialogs, not a checkbox.
`payment-attribute.tsx` gates on `confirmingMismatch`, whose only action is
"Continue to extra confirmation"; the real confirmation names both assets and
their amounts.

G2-4 PASS — `webhook-replay.tsx:27` reads `dry_run` as `!== "false"`, so it
defaults TRUE and stays true unless explicitly turned off. A live replay
additionally requires an acknowledged dry-run count whose filter signature still
matches — change the consumer or the range and the acknowledgement is void, which
is stronger than WIPN-041 asked for. No loop exists in the replay path: no `for`,
no `while`, no `map` over a mutation. The 200 ceiling is stated and the number of
calls a larger range needs is shown.

G2-5 PASS ON THE SECOND ATTEMPT — this gate found a real defect. The single
dead-letter retry refetched only the list, so after retrying the last dead IPN
the table emptied while the nav alarm counter still showed it, on the one page
whose purpose is clearing that alarm. Every other mutation in the dashboard was
already correct, including the same worker's own bulk replay. Now
`webhooks-dashboard.tsx:44` invalidates `queryKeys.stats()` alongside the
refetch. Verified across all seven mutation files: each invalidates its entity,
its lists, and the alarm counters.

G2-6 PASS — `order-create-form.tsx` renders a three-column Field/Requested/Stored
grid from `details.fields` on a 409 `external_ref_conflict`, links to the existing
order, and states that creation did NOT succeed. The stored order is never
presented as a successful creation — which is the 500-USDT-request-renders-a-
25-USDT-order failure backend API-002 exists to close.

WP1 GATE — 2026-08-14. Five of six PASS, one PASS WITH DEBT. No gate was taken
on the word of a sub-agent; every line below is a command I ran or a file I read.

G1-1 PASS — no secret in any browser-visible artefact.
  `grep -ril "PAYD_API_KEY|X-API-Key|SESSION_SECRET|DASH_TOTP|DASH_PASSWORD"
  .next/static/` after a production build: no matches. The build used throwaway
  values (`buildonly`, `JBSWY3DPEHPK3PXP`) and a scan for those literals in the
  bundle is also empty, which is the stronger check — it proves the values did
  not reach the client rather than that the variable names did not.
  `localStorage`/`sessionStorage`: no occurrences outside tests.
  `console.log|debug|info`: none outside tests.
  Cookies: `payd_session` is `HttpOnly; Secure; SameSite=Strict`
  (`app/api/auth/login/route.ts:116`). `payd_csrf` is deliberately readable —
  it is a CSRF token, not a secret, and carries no session material.

G1-2 PASS — verified by a test, as the gate demands.
  `npm test` → `G1-2 permits only listed timestamp and TTL coercions`.
  `lib/no-coercion.test.ts` walks the TypeScript AST over `app`, `components` and
  `lib` and flags `Number(`, `parseFloat`, `parseInt`, `toFixed`,
  `toLocaleString`, and arithmetic or comparison on twenty money-named
  identifiers. The allowlist holds four entries, each an EXACT SOURCE LINE rather
  than a file exemption, so any edit to those lines re-flags them: three UTC/local
  date-filter conversions and the session TTL. The assertion is bidirectional —
  a stale allowlist entry fails too. A self-check runs the detector over
  `Number(w.amount_usd)` and asserts it is caught, so a broken regex cannot pass
  everything silently.

G1-3 PASS — confirmed and pending never merged.
  The G1-2 detector covers the arithmetic case: `confirmed`, `confirmed_raw`,
  `pending` and `chain_raw` are in its money-name list, so summing them anywhere
  fails the build. Read directly: `addresses-dashboard.tsx:46` renders them as
  two separately labelled figures per asset, and `address-detail.tsx` gives them
  two table columns. No other page renders a balance.

G1-4 PASS — 17 req/min on the busiest page, against a 30 limit.
  Counted from the `polling: { tier }` declarations. Tier B is 30s (2/min), C is
  60s (1/min), A is 5s or 10s for a withdrawal, D does not poll.
  Overview is the busiest: 8 tier-B queries = 16/min, plus the nav alarm counter
  at tier C = 1/min → 17/min. Order detail 12+1 = 13. Withdrawal detail 6+1 = 7.
  Addresses 6+1 = 7. Payments 2+1 = 3. This matches the DAT §5.2 budget as
  recalculated when `09-overview` landed.

G1-5 PASS — verified by a test, as the gate demands.
  `npm test` → `G1-5 POST calls payd once for timeout, errors, and connection
  reset`. `lib/proxy-no-retry.test.ts` drives the REAL handler —
  `proxyPaydRequest` imported from `app/api/payd/[...path]/route.ts`, not a
  reimplementation — with a call-counting `fetch` stub across five outcomes:
  TimeoutError, 500, 502, 429, and a rejected fetch standing in for a connection
  reset. Each asserts exactly one upstream call. Timeout and connection reset are
  the two that matter: they are where a resilience layer gets added by someone
  who does not know the backend never retries a fund-moving action.

G1-6 PASS WITH DEBT — the gate's own wording is met; a related requirement is not.
  Second half proven by test: `G1-6 rejects expired and tampered sessions before
  contacting payd` asserts 401 AND zero upstream calls, so an expired session
  cannot reach a payd route through the proxy.
  First half: `app/(dash)/layout.tsx:11` redirects to `/login` when no valid
  session exists, so any navigation or reload with an expired session lands on
  login.
  THE DEBT: `AUTH-023` requires a warning five minutes BEFORE expiry, and that
  an in-progress form is not silently discarded. Nothing implements it — no
  match anywhere outside `lib/session.ts`'s own sweep. It is not named in any
  G1 gate line, so it does not fail this gate, but it is an unmet WP1
  requirement owned by `03-auth-foundation` and it becomes materially dangerous
  at `18-wd-wizard`: a withdrawal wizard that loses its inputs to a session
  timeout invites a hurried, unverified re-entry, which is precisely what the
  requirement was written to prevent. Recorded in Deferred requirements and
  MUST be closed before the WP3 gate.

## Task validation

18-wd-wizard: PASS on attempt 3 — `./node_modules/.bin/tsc --noEmit` exit 0,
`rtk proxy npm run lint` exit 0, `npm test` 4/4, `npm run build` exit 0 with
`/withdrawals/new` routed, `.env` restored, bundle scan clean (including for
`X-TOTP`).

§11.0 verified line by line against the files, not from the report:
  - Retry-language scan over all three files returns NOTHING.
  - The idempotency key is generated in exactly one place,
    `setIdempotencyKey((current) => current ?? crypto.randomUUID())`, so a second
    pass through step 3 reuses it. It is cleared ONLY by `startNew`.
  - The ambiguous panel contains no `onNew` control at all — grep count 0. After
    an unknown outcome there is no route to a fresh key, only a link to the
    withdrawal list.
  - `allowNew` appears only on 401, 429, both 409s, and the final definite 4xx
    branch. No non-503 5xx can reach it.
  - No `<form>`, `onSubmit` or `onKeyDown` anywhere: WWD-074's ban on Enter
    submission holds structurally rather than by suppression.
  - UI-074 is a real refusal — `lg:hidden` notice plus `hidden lg:block` content,
    so the wizard is absent below 1024px rather than reflowed.

TWO CORRECTNESS FAILURES, BOTH FOUND BY READING THE CODE RATHER THAN THE REPORT:

  1. THE ONE THAT MATTERED — `isAmbiguous` treated only 502 and 504 as ambiguous,
     so a 500 fell through to a branch that said "payd did not report a created
     withdrawal" and offered a new withdrawal, and `startNew()` clears the
     idempotency key. A 500 CAN ARRIVE AFTER THE ROW EXISTS:
     `store.CreateWithdrawal` commits, then re-reads the row
     (`store/withdrawals.go:205-209`), and a failure in either the commit report
     or that read returns an error the API renders as 500 with the withdrawal
     written and the TOTP code consumed. The operator would then have been
     invited to submit again with a FRESH key — a new row, a second transfer,
     clean audit trail on both. Fixed: any status >= 500 other than 503 is
     ambiguous. WWD-086a and WWD-086b added so it cannot regress.
     503 stays definite and keeps its new-withdrawal path, correctly: every 503
     path returns before any write.
  2. Removing the generic branch left the chain with no final `else`, so an
     unhandled status — a 403 from CSRF, a 404 from the proxy allowlist, a 405 —
     set nothing, leaving the dialog open with a cleared TOTP field and no
     message after the operator had just authorised a payout. Not a double-payout
     path, since the key survives and a resubmission is idempotent, but an
     unexplained dead end on this screen is its own hazard. Fixed with a definite
     panel naming the status and code.

THE WORKER PUSHED BACK ON MY BRIEF AND WAS RIGHT: I asked it to confirm "no 5xx
path can reach startNew" while also telling it 503 keeps its definite copy, which
includes exactly that path. 503 keeping it is correct (WWD-085), and the
instruction was self-contradictory. Recorded because the attempt budget should
not be spent on my own contradictions.

17a-session-expiry: PASS — `./node_modules/.bin/tsc --noEmit` exit 0, `npm test`
4/4, build clean. AUTH-023 closed.

Verified the three things most likely to have been done wrong, none of which the
worker was asked to report on: NO renewal, keep-alive, or refresh path exists in
the file; NO polling — the countdown runs off the known expiry timestamp and
makes no request; and only `session.exp`, an integer, crosses the server/client
boundary at `layout.tsx:15`. The session id and every secret stay server-side, so
INV-4 holds. `lib/session-expiry.test.ts` still proves the proxy rejects an
expired session on its own, which is what makes the warning a courtesy rather
than an authority (INV-5).

Surface for the wizard: `useSessionExpiry()` returns `isExpiringSoon`,
`isExpired`, `remainingMs` and `expiresAt`.

16-addresses-dis: PASS — `./node_modules/.bin/tsc --noEmit` exit 0,
`rtk proxy npm run lint` exit 0, `npm test` 4/4 across eight consecutive runs,
`npm run build` exit 0 with all 19 routes present, `.env` restored, backend
untouched.

Read directly: no TOTP anywhere in the file (WADR-060). No occurrence of the word
"enable" at all, so nothing implies an address can be returned to rotation
(WADR-064) — and the control is a one-way button, not a toggle. The dialog states
permanent removal, retained history, and no funds moved (WADR-061), warns that
funds stay put and must be withdrawn explicitly (WADR-062), and warns that an
assigned order is unaffected and the customer may still pay to it (WADR-063).

TWO DEFECTS FOUND WHILE VALIDATING, BOTH FIXED BY ME AS INTEGRATION BREAKAGE,
NEITHER CONSUMING AN ATTEMPT:

  1. SCHEMA DRIFT. `walletResourceSchema` never gained `cooling_until` or
     `assigned_order_id`, though both were added to the backend during the
     `10-orders-read` audit. The worker worked around the missing type with
     `(wallet as WalletDetail & Record<string, unknown>).assigned_order_id`.
     A cast is a silent bet that the field exists; the next reader has no way to
     tell it from a typo. Both fields added to the schema and the cast removed.
     Worth noting for later tasks: the Zod schemas are used ONLY to derive types
     — nothing calls `.parse()` on a response anywhere — so `02-tech-stack.md`'s
     claim that "Zod schemas double as the parse boundary for API responses" is
     not true today. That is a real gap, not a nitpick: a strict schema that is
     never run cannot catch a contract change, which is exactly the class of
     problem that has produced five halts in this run. Recorded for `24-system`
     or `25-polish` to close, and flagged in the WP2 gate.

  2. FLAKY GATE TEST — G1-6 failed roughly one run in four, and the cause was the
     test, not the session code. It forged a session with
     `createSession().value.slice(0, -1) + "x"`, but base64url's final character
     carries bits that decoding discards, so that edit frequently decodes to the
     SAME 16-byte GCM tag and leaves the session genuinely valid. The assertion
     then correctly reported a valid session where it expected null. Fixed by
     flipping a bit in the decoded tag instead, which is guaranteed to differ.
     Eight consecutive green runs after the change.
     This also explains the `15-payments-work` worker's 3/4 report, which I had
     recorded as unreproducible — it was real, it was this, and its diagnosis
     (`createSession(0)`) was wrong. AES-GCM verification was never at fault.

15-payments-work: PASS — `./node_modules/.bin/tsc --noEmit` exit 0,
`rtk proxy npm run lint` exit 0, `npm test` 4/4, `npm run build` exit 0 with
`/payments/unattributed` and `/payments/orphaned` in the route table.

Read directly: there is NO filter control on `unattributed_reason` (WPAY-031) —
the reason is a per-row badge only. There is no restore, re-confirm, recover or
re-detect control anywhere in the orphaned view (WPAY-044); a grep for all four
words returns nothing. The asset-mismatch path is TWO separate dialogs, not a
checkbox in one click: "Continue to extra confirmation" leads to the real
confirmation (WPAY-034). Terminal target orders are warned about without being
blocked (WPAY-035).

TWO REPORTS FROM THE WORKER, ONE RIGHT AND ONE WRONG:
  - RIGHT: the `/payments/orphaned` OpenAPI description described the
    UNATTRIBUTED worklist. Fixed — see the contract repair below.
  - WRONG: it reported `npm test` at 3/4 and blamed `createSession(0)` being
    "verified before its expiry". That reads the argument as a TTL; it is the
    `now` parameter (`lib/session.ts:44`), so the session is stamped at epoch
    zero and is deterministically expired. The test passes 4/4 here on repeated
    runs, before and after its changes. Most likely it observed the transient
    secret-in-log failure recorded under 14-orders-mut, which is now fixed at the
    root. No action taken beyond re-running it; if it recurs, the cause is the
    log capture and not the session code.

14-orders-mut: PASS on attempt 2 — `./node_modules/.bin/tsc --noEmit` exit 0,
`rtk proxy npm run lint` exit 0, `npm test` 4/4, `npm run build` exit 0 with
`/orders/new` and `/orders/funded-terminal` in the route table.

Read directly rather than taken from the report: force-cancel does NOT
auto-escalate — a 409 `order_funded` closes the first dialog and opens a second
one that needs its own explicit click (`order-actions.tsx:54`), and the code it
branches on matches `backend/internal/api/orders.go:378`. No TOTP prompt exists
in any file this task touched (WORD-056). The resolve dialog states that it
records a decision and moves no funds, requires a non-empty note, names
`audit_log`, and its pre-filled withdrawal link is a separate navigation rather
than part of the submission (WORD-064, WORD-065).

CORRECTNESS FAILURE (attempt 1) — WORD-041 branched on `invalid_order` to report
an unknown consumer. The worker's reading of the backend was correct at the time,
but that code also covers asset and amount validation, so a malformed amount
would have told the operator their CONSUMER was disabled and sent them to the
webhooks page to fix something that was not broken. Fixed at the source rather
than in the client — see the contract repair below — and the branch corrected on
attempt 2.

INTEGRATION BREAKAGE, MINE, NOT THE TASK'S — after the v2 spawn, `npm test` went
2/4 with `SESSION_SECRET must not appear in the repository`. The cause was my own
log capture: the three gate tests used a FIXED secret, `Buffer.alloc(32, 7)`, and
the spawn log at `.codex/logs/14-orders-mut.v2.log` echoed the computed value
into the repository, at which point `lib/env.ts`'s repository scan correctly
refused it. The check was right; the test was fragile. Fixed by generating a
random secret per run in all three test files, so no artefact can ever contain
it, and `.codex/logs/.gitignore` added so spawn logs are never committed. Tests
back to 4/4. This did not consume an attempt — it was caused by the harness, not
by the task.

13-withdrawals-read: PASS — `./node_modules/.bin/tsc --noEmit` exit 0,
`rtk proxy npm run lint` exit 0, `npm run build` exit 0 with `/withdrawals` and
`/withdrawals/[id]` in the route table, `.env` restored. Built-bundle secret scan
empty.

§11.0 verified by reading the files, not from the worker's report. The forbidden-
term grep over all four withdrawal files returns NOTHING — no retry, resume,
re-broadcast, try again, resend. The read-failure control is labelled "Reload"
and calls `refetch()` on a GET. The optional WWD-004 create link was omitted
entirely, which is the safer of the two permitted choices while the wizard does
not exist. The ambiguous-outcome panel is all five parts and contains no control.
`total_cost_trx` is rendered from the backend figure and is never summed from the
three component fields. `needs_operator` is pinned by reordering the rows in
hand, not by a second fetch. Detail polling is tier A with `isLive` false on any
terminal status, so it stops rather than slows.

ONE THING FOR THE 22-noretry-audit TO KNOW: the older pages (orders, payments,
addresses) name their GET-refetch prop `retry`, so a bare `grep -n retry` hits
`address-detail.tsx:57`, `addresses-dashboard.tsx:80`, `orders-dashboard.tsx:52`
and `payments-dashboard.tsx:74`. Every one is a read refetch on a non-withdrawal
page. The withdrawal pages deliberately use `reload` instead. This is a naming
inconsistency, not an INV-1 violation, and 25-polish should rename them so the
audit grep stays meaningful.

12-addresses-read: PASS — `./node_modules/.bin/tsc --noEmit` exit 0,
`rtk proxy npm run lint` exit 0, and `npm run build` exit 0 with `/addresses`,
`/addresses/[address]` and `/addresses/needs-resources` in the route table.
`.env` restored after the build. Built-bundle secret scan empty;
withdrawal-control scan clean on both changed files. Filters verified BY READING
THE CODE, not from the worker's report: `state`, `asset` and `drift` are set on
the outgoing query string at `addresses-dashboard.tsx:113-116`, and has-balance
and needs-resources switch to their dedicated endpoints. Pool health reads
`stats.addresses` defensively and renders "unavailable" rather than throwing
when the key is absent. `confirmed_raw` and `chain_raw` are shown side by side as
base units and are never subtracted.

Three requirements are legitimately owed to later tasks and are NOT satisfied
here: WADR-046 (delegate action on the needs-resources rows) to 20-addr-totp,
WADR-070's wizard source selector to 18-wd-wizard, and the withdrawal link
target in WADR-036 to 13-withdrawals-read.

11-payments-read: PASS — `./node_modules/.bin/tsc --noEmit` exit 0,
`rtk proxy npm run lint` exit 0, and `npm run build` exit 0 with `/payments`
present in the route table. `git status --porcelain backend/` shows only the
contract repairs described below; the worker changed no backend file, verified
by modification time rather than from its log. Retry scan found the IPN
redelivery allowlist entry, the global `retry: false`, and `onRetry` handlers
that refetch read-only GET lists — none on a mutation or withdrawal path.
Withdrawal-control scan found login's rate-limit copy only. Money-coercion scan
found three `Number()` hits, all on Unix timestamps for date filters and none on
an amount. Built-bundle secret scan was empty. `NEXT_PUBLIC_` scan found comment
prose only.

Note on running the build: `web/.env` exists with `PAYD_API_KEY`,
`DASH_PASSWORD_HASH` and `DASH_TOTP_SECRET` present but EMPTY, and Next's env
loading lets those empty values beat process-supplied ones, so a build with
inline values fails on the first required-variable check. Move `.env` aside for
the build and move it back afterwards. Do not fill it in — those are the
operator's real credentials slots, and `SESSION_SECRET` is validated against
appearing anywhere in the repository.

09-overview: PASS — `./node_modules/.bin/tsc --noEmit`, `npm run lint`, and `npm run build` (with process-only values) exited 0. Retry scan found the IPN retry allowlist and global `retry: false` only; withdrawal-control scan found login's rate-limit copy only; money-coercion scan found session TTL conversion only; built-bundle-secret scan was empty. The backend working-tree diff is the user's explicit API contract repair that cleared H4, not an `09-overview` worker change.

08-shell: PASS — `./node_modules/.bin/tsc --noEmit`, `npm run lint`, and `npm run build` (with process-only values) exited 0. `git status --porcelain backend/` was empty. Retry scan found the IPN retry allowlist and global `retry: false` only; withdrawal-control scan found login's rate-limit copy only; money-coercion scan found session TTL conversion only; built-bundle-secret scan was empty. `NEXT_PUBLIC_` hits are specification text only. WIPN-030 makes `/webhooks` the dedicated dead-letter worklist, so its alarm link needs no invented filter parameter.

01-scaffold: PASS — `rtk proxy npx tsc --noEmit`, `rtk proxy npm run lint`, and `npm run build` (with temporary process-only server values) exited 0. `git status --porcelain backend/` was empty. Retry-control, withdrawal-control-language, money-coercion, and built-bundle-secret scans had no code hits. `NEXT_PUBLIC_` scan found documentation/autopilot text only, not a runtime variable.

02-types: PASS — `rtk proxy npx tsc --noEmit`, `rtk proxy npm run lint`, and `npm run build` (with temporary process-only server values) exited 0. `git status --porcelain backend/` was empty. Retry-control was globally `retry: false`; no withdrawal-control-language, money-coercion, or built-bundle-secret scan had a code hit.

## Blocked / halted
Nothing currently blocking.

DOCUMENTATION REPAIRS FOR `15-payments-work`, 2026-08-14. Two, both in
`openapi.yaml`, neither a behaviour change — and together they are the reason
this file now needs a dedicated audit rather than one halt at a time.

  1. `Payment.order_id` claimed to be "null … which is what the unattributed and
     orphaned listings return". False for the orphaned listing. `RewindChain`
     orphans a payment with `UPDATE payments SET status='orphaned'` and touches
     nothing else (`store/follower.go:418`), so the attribution survives the
     reorg. Without that field WPAY-043 is impossible, which is exactly where the
     worker stopped.
  2. The whole `/payments/orphaned` DESCRIPTION described the unattributed
     worklist — "cannot attribute at all — the address has no order that could
     own them … credit it to an order by hand". That is a different endpoint and
     the opposite operational meaning: an unattributed payment is real money
     needing an owner, an orphaned one is money that most likely is not there any
     more. Rewritten to say what the code does, to point at the unattributed
     route for the other case, and to state that no restore endpoint exists and
     no client may offer one.

This is the third doc-vs-code mismatch found by a halt (after `unknown_consumer`)
on top of five fields that were stored but never serialized. The pattern is
consistent: the read API drifted from its own description. `26-coverage` should
audit `openapi.yaml` against the handlers directly rather than trusting either
one, and that is now its explicit remit.

CONTRACT REPAIR FOR `14-orders-mut`, 2026-08-14 — `unknown_consumer`.
`POST /orders` returned 400 `invalid_order` for BOTH a bad asset/amount and an
unknown or disabled consumer, so no client could tell the two apart. WORD-041
requires naming the consumer and linking to webhooks, which meant the UI had to
guess — and would confidently give the wrong diagnosis to an operator whose
amount was malformed. `ErrUnknownConsumer` now maps to its own 400
`unknown_consumer`, the same code `/ipn/test` already uses for the same
condition, and `invalid_order` now means only what its name says. WORD-041
rewritten to require branching on the specific code.
Verification: `go build ./...` clean, `go test ./...` 239 passed in 19 packages.
`openapi.yaml` updated in both the prose and the `x-error-codes` list.

CONTRACT REPAIRS FOR `12-addresses-read`, 2026-08-14. Three of the worker's four
reported gaps were real and were closed in the API; the fourth was a defect in my
own specification.

  1. `GET /wallets` TOOK NO FILTERS. `WADR-006` requires five, and without them
     the page filtered its loaded cursor page in the browser — which reports "3
     disabled addresses" when the pool holds thirty. `state`, `asset` and `drift`
     are now query parameters resolved in SQL (`store.WalletFilter`); has-balance
     and needs-resources already had dedicated endpoints. An unrecognised state
     is a 400 rather than an empty page, because silently returning nothing is
     indistinguishable from a pool with none of that state.
  2. POOL TOTALS DID NOT EXIST. `GET /stats` now reports `addresses` grouped by
     state, with all four states always present so zero renders as 0 rather than
     as a missing key. This is the same resolution as the alarm counters: a count
     of a paginated collection comes from `/stats`, never from a page. `WADR-008a`
     added to forbid the page-count reconstruction.
  3. `confirmed_raw` DID NOT EXIST, so `WADR-021` could not be met. The wallet
     API returned `confirmed` in whole units and `chain_raw` in base units —
     drift is the disagreement between them, and the two were on different
     scales. `confirmed_raw` now travels with `chain_raw`. The UI shows both and
     is forbidden from subtracting them: displaying the pair is the requirement,
     and a client-side big-number subtraction on a money field is what INV-2
     exists to prevent.
  4. `WADR-030` ASSERTED AN ASSIGNMENT HISTORY THAT IS NOT RETAINED. Specification
     error, not an API gap. The backend keeps the CURRENT assignment only. The
     requirement now says so and points at the address's orders and payments as
     the actual history, with the order's own `created_at`/`address_released_at`
     as its window.

Verification: `go build ./...` clean, `go test ./...` 239 passed in 19 packages.
`openapi.yaml` updated for the three new parameters and the new field.

H4 (`11-payments-read`) — CLEARED 2026-08-14. The halt was correct: `Payment`
carried no withdrawal relationship and `GET /withdrawals` cannot be queried by
txid, so the client could only have guessed by scanning pages.

Repair: `withdrawal_id` added to the `Payment` response, resolved in SQL from
`withdrawals.txid = payments.txid` and restricted to `direction = 'out'`. Two
choices worth recording:
  - MATCHED ON TXID, NOT A NEW COLUMN. The relationship already exists in the
    data — the follower detects the outbound transfer the engine broadcast — and
    `idx_withdrawals_txid` already indexes the lookup. A stored foreign key would
    be a second copy of a fact the chain already settles, and could disagree with
    it.
  - RESTRICTED TO OUTBOUND. A withdrawal between two owned addresses produces an
    inbound row with the same txid under bidirectional screening (DET-002b).
    Linking that row would label money arriving as money leaving.
Null therefore means one of two things an operator must be able to tell apart
from a withdrawal: an inbound payment, or an outbound transfer this service did
not broadcast. `WPAY-005` was rewritten to require null render as "not a service
withdrawal" and to forbid inferring the link from any other field.

Verification: `go build ./...` clean, `go test ./...` 239 passed in 19 packages,
`./node_modules/.bin/tsc --noEmit` exit 0. `Payment` is `additionalProperties:
false` in `openapi.yaml` and `z.strictObject` in `schemas.ts`, so the field could
not have been added on one side only.

SECOND REPAIR, same task — `Payment.amount_raw`. `WPAY-021` requires the raw and
the formatted amount; the response carried only the formatted one, so the worker
rendered "not supplied by payd's Payment contract" rather than inventing a
figure. The value was already loaded into `store.Payment` and simply never
serialized — the same defect as `block_id` and `is_dust`, which makes three of
that kind on this one schema. Now serialized, documented, and in the Zod schema.
The two figures must never be computed from each other: `amount` is `amount_raw`
divided by the asset's configured decimals, so they disagree precisely when that
configuration is wrong, which is the case `WPAY-021` exists to expose.

This was NOT a correctness failure and did not consume an attempt: the field did
not exist when the worker ran, so no re-spawn of the same brief could have
produced it (§6.4, "a failure a sub-agent cannot reach"). The remediation brief
carried only that one item and forbade touching the accepted work.

H4 (`11-payments-read`) — CLEARED. Both findings correct, and the second one was
the most substantial contract gap found so far.

  1. `block_id` was loaded into `store.Payment` and never serialized. Added.
     With `block_height` it identifies the exact block, which is what separates
     a re-included transaction from an orphaned one after a reorg.
  2. `unattributed_reason` did not exist anywhere. The matcher computes exactly
     the three booleans `WPAY-023` needs — `active`, `assetMatches`,
     `insideWindow` — and then discarded them. It now records which condition
     failed, in remedy order: an asset mismatch (ORD-002a) is a customer error
     with its own resolution, so it outranks a window miss.

     STORED, NOT RECOMPUTED. This is the same trap as `address_released_at`: by
     the time an operator looks, the address may have been released or
     reassigned and the order expired, so re-running the checks against current
     state can return a different answer than the one actually made. A wrong
     attribution explanation is worse than a missing one.

     Migration `009_unattributed_reason.sql` adds the column with a CHECK
     constraint on the three values. Null for attributed payments and for every
     row detected before the migration — `WPAY-023b` requires that to render as
     "reason not recorded" rather than as a value or an error.

Two store tests asserted hardcoded migration and table counts (8) and were
updated to 9. That is the assertion doing its job, not a regression.

`WPAY-031` was also corrected during this fix: it briefly required filtering the
worklist by reason, which no endpoint supports — the same defect class as the
halt itself. It now forbids a client-side filter, because filtering a cursor-
paginated list in the browser silently applies to the loaded page only and
misrepresents the worklist's true size.

Verification: `go build ./...` clean, `go test ./...` 239 passed in 19 packages,
`./node_modules/.bin/tsc --noEmit` exit 0, `npm run build` succeeds. Web Zod
schema and `openapi.yaml` both updated; `Payment` is `additionalProperties:
false`, so neither field could have been added silently.

H4 (`10-orders-read`, Tronscan configuration) — CLEARED. A real contradiction
between two of my own requirements, not an implementation fault: `UI-033`
required a configurable explorer URL, `WST §2.3` said every environment value is
server-only and listed no Tronscan setting, and nothing said how a non-secret
value legitimately reaches the browser. The worker built the view correctly and
stopped on the policy rather than guessing — the right call, since both
available guesses were bad: a hardcoded mainnet link breaks `UI-033`, and
inferring the network from `PAYD_BASE_URL` breaks `INV-5`.

Resolved the way the worker itself proposed:
  - `TRONSCAN_BASE_URL` added as a server-only required variable, validated as an
    https origin, documented in `.env.example`.
  - Provided to the client tree through one React context in `app/providers.tsx`,
    fed from the root layout — a server component. No `NEXT_PUBLIC_` anywhere.
  - `WST-020` tightened from "no secret in a `NEXT_PUBLIC_` variable" to **no
    `NEXT_PUBLIC_` variable at all**. The old wording required a judgement call
    per variable about whether that one was safe to expose; the new one is a
    single grep with no judgement in it. `AUTOPILOT.md` §6.2 updated to match.
  - `WST-020a` and `UI-033a` added: no mainnet default, fail to boot instead, and
    no per-component fallback literal. A defaulted explorer makes a Nile
    deployment look identical to a mainnet one, which is how a real payout gets
    made in the belief that it is a test (`WSYS-054`).

Verification: `./node_modules/.bin/tsc --noEmit` exit 0, `npm run build`
succeeds, `grep -rn NEXT_PUBLIC_` finds only prose in comments.

H4 (`10-orders-read`) — CLEARED, AND THE UNDERLYING CLASS CLOSED. Both findings
correct. This was the fourth halt caused by a specification asserting an API
field that did not exist, so alongside the fix a full audit was run: every
snake_case field the page specs assert (107 of them) was checked against
`openapi.yaml` and the Go serializers. It found four more defects that would
each have halted a later task.

Fixed for this halt:
  1. `Order.address_released_at` added — closes the ORD-002b assignment window.
     The subquery is guarded on `assigned_order_id = orders.id`: POOL-005 hands
     the address to the next order once cooldown ends, and an unguarded join
     would report that order's release time as this one's, which is a wrong
     attribution boundary rather than a missing one. Null therefore has two
     meanings, documented in the schema and split in `WORD-023a`: still held
     (order open) versus no longer recorded (cooldown finished).
  2. `Payment.is_dust` added. The value was already loaded into `store.Payment`
     and simply never serialized. `Payment` is `additionalProperties: false`,
     so the schema was updated too. This fixes `/payments`, the unattributed
     and orphaned worklists, and the order-detail payments table at once.

Found and fixed by the audit, before they could halt anything:
  3. `WORD-039`/`DAT-033` named the 503 code `pool_exhausted`; the backend
     emits `address_pool_exhausted`. A client branching on `error.code`
     (`DAT-030`) would never have matched it.
  4. `WWD-032` listed `resolved_by` values `chain_lookup` and `expiration`.
     The engine writes `chain_absence`, `resource_acquisition`, and `operator`.
     The spec now uses the real values and requires an unrecognised one to
     render raw rather than be mapped to the nearest known value.
  5. `cooling_until` and `assigned_order_id` were absent from the `/wallets`
     response, so `WADR-001`/`WADR-003` could not show remaining cooldown or
     which order holds an address. Both added.
  6. `estimated_rent_trx` is required by backend `API-010` but is not computed
     anywhere. Recorded as a known backend gap on `WADR-041` rather than
     silently dropped: the UI must render it when it appears and must never
     substitute a burn figure or a client-side estimate.

Remaining audit non-matches are all benign: `current_status` and `event_id` are
IPN payload semantics rather than response fields, and `chain_absence` /
`resource_acquisition` live in `store/withdrawals.go`, outside the grep corpus.

Verification: `go build ./...` clean, `go test ./...` 239 passed in 19 packages,
`./node_modules/.bin/tsc --noEmit` exit 0, `npm run build` succeeds.

H4 (second on `09-overview`) — CLEARED. Both findings correct.

  1. MISSING CONFIG THRESHOLDS. `GET /config` exposed only `energy.enabled` and
     no price or wallet-pool settings, so `WOVW-012` and `WOVW-051` referenced
     fields that did not exist. This was the third halt of the same class — a
     specification asserting an API field that was never there — so the fix was
     made across every such reference at once rather than one field per halt.
     `GET /config` now carries six operator-display thresholds:
     `energy.max_burn_trx`, `energy.balance_warn_trx`,
     `price.stale_after_seconds`, `wallet.pool_min_free`,
     `wallet.pool_max_size`, `wallet.cooldown_seconds`. Every one is a number,
     duration, or decimal string, so `API-043`/`CFG-011`'s guarantee holds
     structurally: the projection is explicit, not a redaction pass, and no
     field it contains is capable of holding a credential. Backend `API-043`
     amended to match. This also pre-satisfies `WADR-008`, `WRES-002`,
     `WRES-012`, `WWD-065` and `WORD-039`, which referenced the same absent
     fields and would each have halted a later task.
  2. ALLOWLIST DROPPED EVERY PUBLIC ROUTE. `scripts/generate-payd-allowlist.mjs`
     filtered on `/api/v1/`, so `/healthz`, `/readyz` and `/openapi.yaml` — all
     served at the root — never reached the generated list, and the proxy 404'd
     `/readyz`, which `WOVW-010`/`WOVW-011` require. The generator now emits
     them flagged `public`, and fails the build if any is absent rather than
     writing a quietly incomplete list. The proxy forwards a public route to its
     root path and without `X-API-Key`, since it takes none and is not rate
     limited. `BFF-005a`/`BFF-005b` added so this cannot regress silently.

Verification: `go build ./...` clean, `go test ./...` 239 passed in 19 packages,
`./node_modules/.bin/tsc --noEmit` exit 0, `npm run build` succeeds,
`openapi.yaml` updated for the `/config` projection.

Lesson for later tasks: `/config` is now the single source for every operator
threshold the dashboard shows. `WSYS-020a` forbids any page hardcoding a
fallback for one.

H4 — `09-overview` CLEARED. Three separate specification/contract
contradictions, all correctly identified, none of them the agent's fault.

  1. `WOVW-052` sourced today's volume from `/stats`, which reports all-time
     counts grouped by status and has no per-day dimension. Repointed at
     `GET /reports/volume?from=<UTC midnight>&to=<now>&group_by=day`, which
     already returns exactly this. No backend change. `WOVW-052a` added so
     `unpriced_paid_count` travels with the USD total instead of being buried
     in the full report.
  2. `WOVW-041` required stall detection against each worker's expected
     interval, which `/workers` never exposed. Cadences span three orders of
     magnitude (ipn 1s, chain_params 6h) and two are runtime-configurable, so a
     single threshold is meaningless and a hardcoded client table would be
     wrong the moment config changed. `/workers` now returns
     `expected_interval_seconds` per row, sourced from the constants the
     workers actually tick on rather than a second copy: `ParameterInterval`,
     `BalanceReconcileInterval` and `TickInterval` were exported, and
     `lifecycle.ShortInterval`/`LongInterval`, `ipn.TickInterval` and
     `wallet.SafetyNetInterval` introduced to replace inline literals.
     `WOVW-041a`/`041b` added.
  3. `WOVW-012` required readiness figures; `/readyz` returns bare codes. No
     backend change: each code now maps to the endpoint that owns its figure,
     all of which Overview already polls. `clock_skew` is the one real gap —
     its magnitude exists only as a Prometheus gauge and `WSYS-062` forbids
     parsing `/metrics` — so `WOVW-012a` renders it as text with the
     consequence stated but no number. `WOVW-012b` requires unknown reason
     codes to render raw rather than be swallowed.

Backend verification: `go build ./...` clean, `go test ./...` 239 passed in 19
packages. `openapi.yaml` updated for the new `/workers` field. `WOVW-060` and
the `DAT` §5.2 budget were recalculated: Overview is now 16 req/min, and the nav
costs 1 req/min for all five counters instead of 4.

All prior events cleared 2026-08-13; kept below as
history, newest first.

H4 — `08-shell` CLEARED BY FIXING THE SPECIFICATION AND THE BACKEND. The halt
was correct: `WOVW-004` and `DAT-009` required an exact count from a `limit=1`
probe, and no list endpoint has ever exposed one. `OrderList`,
`FundedOrderList`, `PaymentList`, `WithdrawalList`, and `DeadIPNPage` all
return rows plus `next_cursor` and nothing more. The requirement was wrong, not
the contract, and the orchestrator was right to stop rather than fetch full
pages to count them.

Resolution, decided by the human:
  1. `DAT-009` and `WOVW-004` rewritten — all alarm counts now come from
     `GET /stats`, which already carried four of the five exactly:
     `needs_operator`, `payments["unattributed"]`, `orphaned_unresolved`, and
     the summed `ipn_dead` map.
  2. The fifth had no source. `orders["expired_funded"] + ["cancelled_funded"]`
     overcounts, because that grouping ignores `resolution`, so the alarm would
     never fall back to zero once an order was resolved. `store.OperationalMetrics`
     gained `FundedUnresolved` / `funded_terminal_unresolved`, counting
     `status IN ('expired_funded','cancelled_funded') AND resolution IS NULL`
     against the existing `idx_orders_funded_terminal` index. Applied by the
     human; `go build ./...` clean and 124 store/api tests pass.
  3. `WOVW-004a` added, forbidding the client from reconstructing that figure
     by addition.
This is the WP-001 path working as designed: a missing backend capability
became a backend change request, not a client-side workaround.

ORCHESTRATOR WEDGE (not a numbered halt) — CLEARED. The `07-components`
sub-agent completed at 12:10:37 and reported full per-ID coverage. The
orchestrator never recorded it: no ledger update, no `08-shell` spawn, and no
file write anywhere for 29 minutes while three `codex.exe` processes stayed
alive. Stopped by the human at 12:40 and the task marked DONE from independent
verification. Probable cause: `.codex/logs/07-components.err.log` reached
854 KB because the spawn command `tee`s the sub-agent's entire diff, and the
orchestrator then read it back into its own context. Mitigation: cap captured
log output — see AUTOPILOT.md §2.

H3 — `05-query` CLEARED. Failed three attempts on
`lib/payd/browser-client.ts(67,63): error TS2345: Argument of type 'object' is
not assignable to parameter of type 'Readonly<Record<string, unknown>>'`. The
file was subsequently rewritten and now typechecks. The ledger note attributing
this to a human fix is inaccurate — no human edited that file. DAT-007's
rate-limit backoff survived the rewrite, relocated to `lib/query.ts`. The
`Number()` money-scan hit is session TTL conversion in `lib/session.ts`, not an
amount field.

H3 — `03-auth-foundation` CLEARED. `postcss.config.js` (CommonJS) was invalid
under the `"type": "module"` added for the session test. Converted to ESM as
`postcss.config.mjs`. `npm run build` now succeeds.

H4 — CLEARED IN SPEC. The AUTH-030 / BFF-006 / WST-011 cycle was a genuine defect in the specification, not an agent error. Resolved by ordering rather than exemption, and written into the specs as `BFF-013` (with `WST-011` and `AUTH-030` amended to match): the login handler creates the session first, invokes the proxy handler in process carrying that session's own cookie, and invalidates the session before emitting any `Set-Cookie` if whoami fails. The implementation in `app/api/auth/login/route.ts` already does exactly this; no rework required.

## Deferred requirements — owed by later tasks

`07-components` correctly refused to reach outside its allowed paths and left
these unsatisfied. They are NOT optional and NOT complete. Whichever task below
owns each one MUST satisfy it, and the WP1 gate MUST NOT pass while any remain.

| Requirement | What is missing | Owed by |
|---|---|---|
| UI-004 | confirmed/pending rendered as two labelled figures, never merged | 12-addresses-read, 13-withdrawals-read |
| UI-073 | tables become stacked cards below 1024px | 25-polish |
| UI-074 | withdrawal wizard refuses to render below 1024px | 18-wd-wizard |
| DAT-026 | filter state persisted in the URL query string | each page owning a filtered list |
| AUTH-045 | all four TOTP-gated actions routed through the one shared confirm component | 18-wd-wizard, 19-wd-resolve, 20-addr-totp |
| AUTH-023 | warning 5 minutes before session expiry, and no silent discard of an in-progress form | MUST be closed before the WP3 gate — see the WP1 gate log. Nothing implements it today. It is cheap now and expensive at 18-wd-wizard, where a wizard silently losing its inputs invites a hurried re-entry of a payout |

## Note on tooling

`npx tsc` resolves to an older global TypeScript in this environment and reports three false `tsconfig.json` errors. Use `./node_modules/.bin/tsc --noEmit`. `next build` already uses the local compiler.
