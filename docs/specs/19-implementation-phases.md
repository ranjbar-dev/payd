# 19. Implementation phases

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §19
**ID prefixes in this file:** `P1`–`P15` (phase IDs)
**Related:** `Roadmap.md` at the project root turns this table into copy-pasteable Codex CLI prompts, one per phase, with a review checklist after each. [`18-testing.md`](18-testing.md) for the TST-* IDs named in the gates.

---

| Phase | Deliverable | Gate |
|---|---|---|
| **P1** | Project skeleton, config loading (incl. CFG-013/014/015), SQLite store with both connections, migrations, `seedtool` | `seedtool` round-trips a mnemonic; `payd` starts, derives 20 pool addresses, and refuses to start on a resource-wallet index collision |
| **P2** | TronGrid client: failover, circuit breaker, request accounting, **broadcast exemption from retry**, chain parameters (W-011) | Integration test against Nile testnet passes; a unit test proves broadcast is never retried |
| **P3** | Chain follower: polling, gap detection, height-regression guard, double-confirmed reorg detection | Replays 1,000 recorded blocks; TST-003 and TST-003a pass |
| **P4** | Decoder: TRX + TRC-20, two-tier screening, canonical `log_index`, bidirectional screening | All fixture cases decode correctly, including multi-log transactions and outbound transfers |
| **P5** | Address pool, order lifecycle, matcher, **Lifecycle Worker (W-010)** | TST-005, TST-017, TST-019, TST-020 pass; orders expire on a quiet chain |
| **P6** | Confirmation tracker with block-identity check | Two-stage transition verified on testnet; an orphaned block at a solidified height does not promote |
| **P7** | IPN dispatcher: outbox, per-consumer HMAC, `sequence_key` ordering, dead-letter | TST-011 and TST-023 pass |
| **P8** | Price poller | Stale-price gate blocks both order and withdrawal creation |
| **P9** | REST API: orders, payments, wallets, auth, `used_totp`, rate limiting | Full API test suite passes; TST-022 passes |
| **P10** | Wallet monitor, three-column balances, tiered polling, drift consumption | `needs-resources` returns correct data on testnet; BAL-002 blocks a drifting address |
| **P11** | Withdrawal engine: sync/async validation split, signing, single broadcast, on-chain resolution, daily limit | Testnet withdrawal completes end to end; **TST-014, TST-015, TST-016, TST-021 pass** |
| **P12** | Self-delegation via raw_json mode (tier 2) + bandwidth sourcing (§12.4) | Delegation confirmed on testnet; TST-018 passes |
| **P13** | Energy provider integration (tier 1) + full fallback chain on live chain parameters | TST-012 passes; a real rented withdrawal completes on mainnet with a small amount |
| **P14** | Metrics, health checks, clock-skew detection, reconciler (both assets, both directions), backup docs | Runs 72h on testnet with no drift; quota projection reports correctly |
| **P15** | Mainnet soak with small amounts | 100 real orders processed correctly; energy cost per withdrawal matches expectations; zero `needs_operator` withdrawals |

Each phase MUST end with tests passing and `go vet` / `golangci-lint` clean before the next begins.

**P5 and P11 MUST NOT begin until the five highest-damage fixes are reflected in code:** WDR-017 broadcast classification, WDR-001a idempotency ordering, RES-006 bandwidth sourcing, CHN-016 reorg re-inclusion, and ORD-002's asset filter.
