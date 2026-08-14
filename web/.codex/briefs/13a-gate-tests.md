ROLE: PLATFORM
TASK-ID: 13a-gate-tests
GOAL: Write the two automated checks the WP1 gate requires by name — G1-2 (no numeric coercion on any amount path) and G1-5 (the proxy never re-sends a POST) — plus a session-expiry check for G1-6, and wire them into one `npm test` script.

You are working in the repository at C:\Users\root\Desktop\tron-payment-proccesor.
The web app is `web/`. Run every command from `web/`.

WHY THIS EXISTS: `web/docs/specs/16-implementation-phases.md` gate G1-2 says
"verified by a test that fails if any numeric coercion appears on an amount
path", and G1-5 says "verified by a test that fails the build if a mutation is
re-sent on timeout or 5xx". A gate that says "verified by a test" is not passed
by reading the code. These are those tests. They must FAIL if the property they
protect is broken — a test that passes unconditionally is worse than no test,
because it converts an unverified gate into a false green.

READ FIRST, FULLY:
  web/docs/specs/16-implementation-phases.md — the WP1 gate table
  web/docs/specs/03-architecture-and-bff.md — BFF-020 and the proxy contract
  web/docs/specs/05-data-fetching.md — DAT-034
  web/lib/session.test.ts — the existing test, and the pattern to follow. It runs
    under `node --test` with `--experimental-strip-types`; see the `test:session`
    script in `package.json`. Use the same mechanism. Do not introduce a test
    framework.
  web/app/api/payd/[...path]/route.ts — the proxy you are testing
  web/lib/query.ts — where `retry: false` lives
  web/lib/session.ts

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/lib/no-coercion.test.ts        (or a name you prefer, same directory)
  web/lib/proxy-no-retry.test.ts
  web/lib/session-expiry.test.ts
  web/package.json                   — ONLY to add test scripts. Add a `test`
                                       script that runs all of them, and keep the
                                       existing `test:session` working. Do not
                                       change any other script, and do not add a
                                       dependency.
Everything else belongs to another agent. If a test can only pass by changing
application code, STOP and report it — that is a real finding and it is exactly
what these tests are for.

WHAT EACH TEST MUST DO:

  1. G1-2 — NO NUMERIC COERCION ON AN AMOUNT PATH.
     A static check over `web/app`, `web/components` and `web/lib`. Read the
     files, find every occurrence of `Number(`, `parseFloat`, `parseInt`,
     `toFixed`, `toLocaleString`, and arithmetic or comparison operators applied
     to an identifier whose name marks it as money.
     The money-name test must at minimum catch: `amount`, `amount_raw`,
     `amount_usd`, `confirmed`, `confirmed_raw`, `pending`, `chain_raw`, `usd`,
     `balance`, `fee_raw`, `total_cost_trx`, `energy_cost_trx`,
     `bandwidth_cost_trx`, `network_fee_trx`, `resource_fee_trx`, `used_usd`,
     `remaining_usd`, `daily_limit_usd`, `price_usd`, `min_deposit`.
     The check MUST have an explicit, listed allowlist of known-good hits, each
     with a one-line reason, and MUST FAIL on anything not in it. Today's
     legitimate hits are Unix-timestamp conversions in date filters and the
     session TTL — nothing else. An allowlist entry is a file plus the exact
     line content, not a whole-file exemption: if the file changes, the check
     must notice.
     PROVE IT FAILS: include, in the test file, a self-check that runs the same
     detector over a small inline string containing e.g. `Number(w.amount_usd)`
     and asserts the detector flags it. Without that, a broken regex silently
     passes everything.

  2. G1-5 — THE PROXY NEVER RE-SENDS A POST.
     A behavioural test of the proxy route handler, not a grep. Drive the handler
     with a stubbed upstream `fetch` that COUNTS calls, and assert the count is
     exactly 1 for each of: a request that times out, one that returns 500, one
     that returns 502, one that returns 429, and one whose connection is reset
     (a rejected fetch). One upstream call per POST, always, whatever came back.
     Also assert the same for the ambiguous cases specifically — timeout and
     connection reset are where a well-meaning resilience layer gets added.
     If driving the real handler needs more scaffolding than the existing test
     pattern supports, drive the exact module that performs the upstream call
     rather than reimplementing its logic in the test. A test that re-implements
     the thing it is testing proves nothing.

  3. G1-6 — SESSION EXPIRY.
     Assert that an expired session is rejected: an expired or tampered session
     value does not verify, and the proxy refuses a request carrying one rather
     than forwarding it upstream. Reuse whatever `lib/session.ts` already
     exposes; `session.test.ts` shows how it is exercised.

THE SIX INVARIANTS still apply to anything you write:
  INV-1  No retry, resume, re-broadcast, or automatic re-send on any mutation
         path. You are testing this property, not adding an exception to it.
  INV-2  Money is a string, start to finish.
  INV-3  Confirmed and pending balances are never merged.
  INV-4  No API key, TOTP code, or secret reaches the browser or a test fixture
         that gets committed. Use obvious dummy values.
  INV-5  No business logic in the client.
  INV-6  Anything scoped to a UTC day is labelled UTC in visible text.

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean
  - `npm run lint` clean
  - `npm test` runs all three and passes against the CURRENT tree
  - each test demonstrably fails when its property is broken. State in your
    report, for each one, exactly what you changed temporarily to see it go red,
    and confirm you reverted it. "It should fail" is not evidence.
  - `npm run build` still succeeds (move `.env` aside for the build, then MOVE
    IT BACK — it holds three required-but-empty variables that beat process env)

YOU MUST NOT:
  - add a dependency, including a test framework or a linter plugin
  - modify application code to make a test pass — report it instead
  - weaken a check to get green
  - modify anything under `backend/`
  - commit, push, or change git branches

REPORT AT THE END:
  - files changed
  - for each of G1-2, G1-5, G1-6: the test that covers it, and the evidence it
    genuinely fails when the property is broken
  - anything you could not test, and why
