# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Backend of the payd TRON payment processor. Root context:
[`../CLAUDE.md`](../CLAUDE.md).

## Read before coding

1. `docs/index.md` — routing table. Open only the spec rows that match your
   task, not the whole `docs/specs/` tree. Cite requirement IDs (`WDR-017`,
   `CHN-016`, `ORD-002a`) in comments, commit messages, and PR descriptions.
2. Anything moving funds → `docs/specs/13-withdrawal-engine.md` §13.0 first.
3. `docs/specs/21-appendix-review-findings.md` — traceability matrix from the
   adversarial design review to the requirement that closes each finding; read
   it to understand *why* a requirement exists.

**Never guess at a requirement.** If a spec doesn't cover a case you hit, say
so and ask — don't invent a plausible default. This codebase moves real money;
an invented default is a bug indistinguishable from a correct implementation
until it fails. Cross-file requirements apply everywhere, not just in their
home file (e.g. the no-retry policy governs `internal/wallet` and
`internal/energy`, not only `internal/withdraw`).

## Commands

```bash
go build ./cmd/payd                    # build daemon → also produces payd.exe
go build ./cmd/seedtool                # mnemonic encryptor
go build ./tools/paydev                # dev/ops CLI (paydev.exe)

go test ./...                          # full suite
go test ./internal/matcher -run TestAttribute   # single package / test
go vet ./...
golangci-lint run                     # must be clean before ending a phase

./payd.exe --config payd.nile.yaml    # run against Nile testnet, :8080
```

Per `docs/specs/19-implementation-phases.md`: **each phase ends with tests
passing and vet / lint clean before the next begins** — don't move on with a
red suite. Several tests are regression tests for named past defects
(`TST-014` = `DUP_TRANSACTION_ERROR`).

## Architecture

Single process, single SQLite DB, ten supervised workers under
`internal/` (see `docs/specs/03-architecture-and-workers.md` §3):
`chain` (TronGrid client), `follower` (block poll + gap/reorg + ingest),
`decode`, `matcher` (payment→order attribution), `confirm` (solidified height),
`lifecycle` (clock-driven expiry / cooldown / pool top-up), `ipn` (signed
callback outbox), `price` (Binance poller), `wallet` (address pool, balances,
resources), `withdraw` (queue, sign, broadcast, resolve), `api` (HTTP + auth).

Structural rules (`ARC-003`, `ARC-005`, `ARC-007`):

- **Workers never call each other directly** — only via channels or through
  `internal/store`.
- **`internal/store` is the only package that opens a SQLite handle.** All
  writes go through it.
- **No network I/O while a write transaction is open** — finish all RPC for a
  unit of work, then open the write txn.

## Invariants (fund-safety, not style)

- **No automatic retry of any fund-moving action** — withdrawal broadcast,
  re-sign, bandwidth top-up (`RES-008`), self-delegation broadcast (`RES-013`).
  Attempted at most once. Reconcile ambiguous outcomes against the chain.
- **All monetary amounts are decimal strings in base units** (`big.Int`),
  never floats or raw ints (`DB-001`).
- **All date-boundary logic uses UTC midnight** (`DB-002a`) — withdrawal daily
  limit, TronGrid daily request counter.
- `docs/specs/20-risks-and-rejected-features.md` lists explicitly rejected
  features (auto-sweeping, fresh-address-per-order, auto-retry). Don't
  reintroduce without the user asking.

## Build order

Don't freelance it. Follow `docs/specs/19-implementation-phases.md` (P1–P15)
and root `Roadmap.md`, which turns each phase into a concrete prompt. P5 and
P11 must not begin until the five highest-damage fixes from the v1.1 review are
in code (note at the bottom of `19-implementation-phases.md`).
