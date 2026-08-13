# Documentation index — Tron & TRC-20 Merchant Payment Service (v1.2)

This is the entry point for the design spec. The original monolithic
`tron-payment-service-design.md` has been split into topic-scoped files under
`docs/specs/`, one per section, so an AI agent (or a human) can load only the
file relevant to the task at hand instead of the whole 1,478-line document.

**How to use this file:** find the row that matches your task's topic or
requirement-ID prefix, open only that file (and its `Related` links, if the
task touches an interacting subsystem). Requirement IDs are stable identifiers
(e.g. `WDR-017`) — grep `docs/specs/*.md` for an ID if you know it but not
which file it lives in.

## Spec files

| # | File | Covers | ID prefixes | Read this when… |
|---|---|---|---|---|
| 1 | [`specs/01-overview-and-goals.md`](specs/01-overview-and-goals.md) | Overview, goals, non-goals, glossary | `G-*`, `NG-*` | You need the big picture, a term defined, or to check whether something is in scope |
| 2 | [`specs/02-tech-stack-and-dependencies.md`](specs/02-tech-stack-and-dependencies.md) | Language/library choices, `hd-wallet` capabilities and gaps | `TD-*`, `GAP-*` | Choosing a dependency, or wondering what `hd-wallet` can/can't do |
| 3 | [`specs/03-architecture-and-workers.md`](specs/03-architecture-and-workers.md) | Process/package layout, the 10 workers, worker lifecycle rules | `W-0xx`, `ARC-*` | Adding a worker, deciding which package owns something, touching SQLite connection handling |
| 4 | [`specs/04-configuration.md`](specs/04-configuration.md) | Full YAML config shape and validation rules | `CFG-*` | Adding/changing a config key, startup validation |
| 5 | [`specs/05-data-model.md`](specs/05-data-model.md) | Full SQL schema, all tables | `DB-*`, `BAL-*` | Any schema change, any query, understanding a table's columns |
| 6 | [`specs/06-chain-follower.md`](specs/06-chain-follower.md) | Block polling, gap detection, reorg handling, endpoint failover | `CHN-*` | Working in `internal/follower` or `internal/chain` |
| 7 | [`specs/07-payment-detection.md`](specs/07-payment-detection.md) | Two-tier TRX/TRC-20 decoding, safety-net reconciliation, address activation | `DET-*` | Working in `internal/decode`, or on the 5-min/6h reconciler |
| 8 | [`specs/08-order-lifecycle-and-address-pool.md`](specs/08-order-lifecycle-and-address-pool.md) | Address pool states, order state machine, unattributed payments, Lifecycle Worker | `POOL-*`, `ORD-*`, `LIF-*` | Working in `internal/matcher`, `internal/lifecycle`, or the orders API |
| 9 | [`specs/09-confirmation-tracking.md`](specs/09-confirmation-tracking.md) | Solidified-height tracking, seen→confirmed promotion | `CNF-*` | Working in `internal/confirm` |
| 10 | [`specs/10-ipn-dispatcher.md`](specs/10-ipn-dispatcher.md) | Outbox pattern, HMAC signing, delivery ordering, event catalogue | `IPN-*` | Working in `internal/ipn`, or defining a new event type |
| 11 | [`specs/11-price-service.md`](specs/11-price-service.md) | Binance price polling, staleness gate | `PRC-*` | Working in `internal/price` |
| 12 | [`specs/12-resource-management.md`](specs/12-resource-management.md) | Energy/bandwidth monitoring, 3-tier energy sourcing, self-delegation, bandwidth sourcing, chain parameters | `RES-*`, `ENR-*` | Working in `internal/wallet` resource code or an `energy.Provider` implementation |
| 13 | [`specs/13-withdrawal-engine.md`](specs/13-withdrawal-engine.md) | **The no-retry policy**, request validation, signing, broadcast classification, crash recovery | `WDR-*` | Touching **anything** in `internal/withdraw`. Read §13.0 first, always |
| 14 | [`specs/14-key-management.md`](specs/14-key-management.md) | `seedtool`, mnemonic encryption, wallet loading | `KEY-*` | Working in `cmd/seedtool` or startup wallet loading |
| 15 | [`specs/15-rest-api.md`](specs/15-rest-api.md) | All HTTP endpoints, auth, TOTP, rate limiting, error envelope | `API-*` | Working in `internal/api` |
| 16 | [`specs/16-rate-limit-budget.md`](specs/16-rate-limit-budget.md) | TronGrid daily quota budget and projection | `RL-*` | Adding any new TronGrid call — check the budget first |
| 17 | [`specs/17-operations.md`](specs/17-operations.md) | Health checks, metrics, backup/recovery | `OPS-*` | Working on `/healthz`, `/readyz`, `/metrics`, or writing recovery docs |
| 18 | [`specs/18-testing.md`](specs/18-testing.md) | Required test coverage and the specific scenarios each phase gate depends on | `TST-*` | Writing tests for any phase |
| 19 | [`specs/19-implementation-phases.md`](specs/19-implementation-phases.md) | The 15-phase build order and each phase's gate | `P1`–`P15` | Deciding what to build next — see also `Roadmap.md` at the project root |
| 20 | [`specs/20-risks-and-rejected-features.md`](specs/20-risks-and-rejected-features.md) | Open risks and mitigations, rejected alternatives, deferred features | `R-*` | Before proposing sweeping, fresh-address mode, or retry — these were considered and rejected, read why first |
| 21 | [`specs/21-appendix-review-findings.md`](specs/21-appendix-review-findings.md) | Traceability from the 30 v1.1 review findings to the requirements that close them | `F-1`…`F-30` | You want to understand *why* a requirement exists |

| — | [`../internal/api/openapi.yaml`](../internal/api/openapi.yaml) and [`specs/15-rest-api.md`](specs/15-rest-api.md) | OpenAPI document | `API-*` | Updating the served API contract |

## Glossary quick-lookup

(Full definitions in `specs/01-overview-and-goals.md` §1.3.)

| Term | Meaning |
|---|---|
| Deposit address | Address derived from the HD wallet, assigned to an order to receive payment |
| Order | A request for payment: expected asset, expected amount, deadline, assigned address |
| Payment | A single detected on-chain transfer into a deposit address |
| Seen | Payment included in a block but not yet solidified |
| Confirmed | Payment in a solidified (irreversible) block |
| Solidified | A Tron block confirmed by >2/3 of Super Representatives; irreversible |
| IPN | Instant Payment Notification — signed HTTP POST to a consumer on state change |
| Sun | Smallest TRX unit; 1 TRX = 1,000,000 sun |
| Retry | Re-attempting an action with an external side effect — **forbidden in the withdrawal path** |
| Reconciliation | Querying the chain to discover what an earlier action actually did — always permitted |
| Assignment window | The interval an address belongs to a given order (ORD-002b) |

## Non-negotiable invariants (apply everywhere, not just their "home" file)

- **No retry on any fund-moving action, ever.** See `specs/13-withdrawal-engine.md` §13.0.
- **All monetary amounts are decimal strings in base units**, never floats. See `DB-001` in `specs/05-data-model.md`.
- **All date-boundary logic uses UTC midnight.** See `DB-002a` in `specs/05-data-model.md`.
- **No network I/O while a SQLite write transaction is open.** See `ARC-007` in `specs/03-architecture-and-workers.md`.

## See also

- `AGENTS.md` at the project root — how Codex CLI should use this doc set and the target Go project layout.
- `Roadmap.md` at the project root — phase-by-phase Codex CLI prompts and review checklists for building this project.
- [`operations/backup-and-recovery.md`](operations/backup-and-recovery.md) — live SQLite backup, restore, total-loss replay, and withdrawal reconciliation runbook (OPS-010..014).
