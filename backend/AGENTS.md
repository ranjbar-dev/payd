# AGENTS.md

This file tells Codex CLI (or any other coding agent working in this repo) how
this project's documentation is organized, and what the target project layout
is. Read this file in full before touching any code.

## What this project is

A self-hosted, single-tenant Go service (`payd`) that accepts TRX and TRC-20
payments on the Tron network: issues deposit addresses, watches the chain,
attributes payments to orders, notifies consumer services via signed IPN
callbacks, and runs fully-automated withdrawals from a dashboard. One process,
one SQLite database, ten supervised workers.

## How to use the docs

**Always start at `docs/index.md`.** It is a routing table: topic and
requirement-ID prefix → the one spec file that covers it. Do not read the
whole `docs/specs/` directory for a small task — read `docs/index.md`, find
the row(s) that match what you're about to touch, and open only those files
(plus anything they link to under "Related").

Rules for working with the specs:

1. **Requirement IDs are stable identifiers**, e.g. `WDR-017`, `CHN-016`,
   `ORD-002a`. When you implement or test something a requirement describes,
   reference its ID in the code comment, commit message, or PR description —
   e.g. `// WDR-017: classify broadcast response into three outcomes`. This is
   how a human reviewer (or a future agent) maps code back to spec without
   re-deriving your reasoning.
2. **Never guess at a requirement.** If a spec file doesn't cover a case
   you've hit, say so explicitly in your response and ask, rather than
   inventing behavior that sounds plausible. This codebase moves real money;
   an invented default is a bug with a signature indistinguishable from a
   correct implementation until it fails.
3. **Cross-file requirements apply everywhere, not just in their "home"
   file.** In particular: the no-retry policy on fund-moving actions
   (`docs/specs/13-withdrawal-engine.md` §13.0) governs code in
   `internal/withdraw`, but also the bandwidth top-up and self-delegation
   broadcasts in `internal/wallet`/`internal/energy` (RES-008, RES-013). When
   in doubt, check `docs/index.md`'s "Non-negotiable invariants" section.
4. **`docs/specs/20-risks-and-rejected-features.md`** lists what was
   considered and explicitly rejected (automatic sweeping, fresh-address-per-
   order, automatic retry). Don't reintroduce these without the user asking —
   they were rejected for stated reasons, not overlooked.
5. **`docs/specs/21-appendix-review-findings.md`** is the traceability matrix
   from the design's adversarial review to the requirements that close each
   finding — read it when you want to understand *why* a requirement exists,
   not just what it says.

## Project file structure

Target layout (from `docs/specs/03-architecture-and-workers.md` §3):

```
cmd/
  payd/          — the daemon entrypoint
  seedtool/      — one-shot mnemonic encryptor (docs/specs/14-key-management.md)
internal/
  chain/         — TronGrid client: failover, rate budget, chain parameters
  follower/      — block polling, gap detection, reorg detection, ingest + matching (W-001)
  decode/        — TRX + TRC-20 transaction/log decoding (two-tier screening)
  matcher/       — payment attribution to orders (in-process stage of follower, own package for testability)
  confirm/       — solidified-height tracking, seen → confirmed promotion (W-003)
  lifecycle/     — clock-driven transitions: order expiry, cooldown return, pool top-up (W-010)
  ipn/           — outbox dispatcher, HMAC signing, delivery ordering (W-004)
  price/         — Binance price poller (W-005)
  wallet/        — address pool, three-column balances, resource monitoring (W-006, W-007)
  withdraw/      — request queue, signing, broadcast, on-chain resolution (W-008)
  api/           — HTTP handlers, auth middleware (W-009)
  store/         — SQLite access layer, migrations — the ONLY package that opens a DB handle
  config/        — config loading and validation
```

Structural rules that apply across this whole tree (`ARC-003`, `ARC-005` in
`docs/specs/03-architecture-and-workers.md`):

- **Workers never call each other directly.** They communicate only via
  channels or by reading/writing through `internal/store`.
- **All writes go through `internal/store`.** No other package opens a SQLite
  handle.
- **No network I/O while a SQLite write transaction is open** (`ARC-007`) —
  finish all RPC calls for a unit of work, then open the write transaction.

## Non-negotiable invariants

Restated here because violating any of these is a fund-safety bug, not a
style issue:

- **No automatic retry of any action that moves funds** — broadcasting a
  withdrawal, re-signing, bandwidth top-ups, self-delegation broadcasts. Each
  is attempted at most once, ever. Ambiguous outcomes are resolved by
  reconciling against the chain, never by re-attempting. Full detail:
  `docs/specs/13-withdrawal-engine.md` §13.0.
- **All monetary amounts are decimal strings in base units** (`big.Int`),
  never floats or raw integers (`DB-001`).
- **All date-boundary logic uses UTC midnight**, not local time (`DB-002a`) —
  this includes the withdrawal daily limit and the TronGrid daily request
  counter.

## Testing expectations

See `docs/specs/18-testing.md` for the full list of required test scenarios,
several of which are the actual regression tests for specific past defects
(e.g. `TST-014` for `DUP_TRANSACTION_ERROR` handling). Per
`docs/specs/19-implementation-phases.md`: **each phase must end with tests
passing and `go vet` / `golangci-lint` clean before the next phase begins.**
Don't move on with a red test suite even if the user hasn't explicitly asked
you to run tests for that step.

## Build order

Don't freelance the build order. Follow `docs/specs/19-implementation-phases.md`
(P1–P15) and `Roadmap.md` at the project root, which turns each phase into a
concrete prompt. Note the explicit prerequisite: P5 and P11 must not begin
until the five highest-damage fixes from the v1.1 review are already in code
(see the note at the bottom of `docs/specs/19-implementation-phases.md`).
