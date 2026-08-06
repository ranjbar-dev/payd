# 18. Testing

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §18
**ID prefixes in this file:** `TST-*`
**Related:** [`19-implementation-phases.md`](19-implementation-phases.md) (each phase gate names specific TST-* IDs), [`13-withdrawal-engine.md`](13-withdrawal-engine.md) (TST-014/015/016/021 are the highest-value tests in the suite)

---

| ID | Requirement |
|---|---|
| TST-001 | A `chain.Client` interface MUST allow substituting a fake TronGrid server in tests |
| TST-002 | Recorded real block fixtures MUST cover: a TRX transfer, a TRC-20 transfer, a failed contract call, a reverted transfer, a transaction with multiple `Transfer` logs, an outbound transfer from an owned address, and an empty block |
| TST-003 | A scripted reorg test MUST verify that orphaned payments are reverted and order state recalculates correctly |
| TST-003a | **A reorg re-inclusion test MUST verify CHN-016**: a payment orphaned by a reorg and re-included in the replacement block MUST return to `seen` and re-credit its order in the same transaction. A test that only removes the transaction does not exercise the common case |
| TST-004 | An IPN test MUST verify retry, backoff, and dead-lettering against a deliberately failing sink |
| TST-005 | A concurrency test MUST verify POOL-006 — that two simultaneous order creations never receive the same address |
| TST-006 | A concurrency test MUST verify WDR-007 — that two simultaneous withdrawals from one address never both reach `awaiting_energy`, `signing`, or `broadcast` |
| TST-007 | Amount arithmetic MUST be property-tested for round-trip correctness through `ParseUnits` / `FormatUnits` at 6 and 18 decimals |
| TST-008 | An end-to-end test MUST run against Shasta or Nile testnet: create order → send payment → observe seen → observe confirmed → withdraw |
| TST-009 | `go test -race ./...` MUST pass with no data races |
| TST-010 | Target coverage: 80% overall, 95% on `internal/decode`, `internal/matcher`, `internal/withdraw`, and `internal/energy` |
| TST-011 | A multi-consumer test MUST verify that one slow consumer does not delay delivery to another, and that each receives a signature valid under its own secret |
| TST-012 | An energy fallback test MUST verify the full chain: provider returns an over-priced quote → falls to self-delegation → self-delegation fails → burns TRX → burn exceeds cap → withdrawal fails cleanly |
| TST-013 | The energy provider MUST be behind an interface with a fake implementation, so no test requires a real prepaid balance |
| TST-014 | **A `DUP_TRANSACTION_ERROR` test MUST verify WDR-017(c)**: a broadcast whose response is lost and which the fake node reports as duplicate MUST end in `broadcast` and then `confirmed`, and MUST NOT reach `failed`. This is the single highest-value test in the suite |
| TST-015 | **An idempotent-replay test MUST verify WDR-001a**: an identical withdrawal request replayed with the same `Idempotency-Key` and the same spent TOTP code MUST return HTTP 200 with the original record, not 401 |
| TST-016 | **A crash-recovery test MUST verify WDR-018/018a**: killing the process between the WDR-015 txid commit and the broadcast response MUST result in on-chain resolution on restart, never a re-sign. A variant with the status column rolled back to `requested` MUST also resolve rather than re-sign |
| TST-017 | **A wrong-asset test MUST verify ORD-002a**: sending TRX to an address with an open USDT order MUST leave the order `pending` and produce an unattributed payment |
| TST-018 | **A bandwidth-exhaustion test MUST verify RES-006**: a second TRC-20 withdrawal from an address with 255 bandwidth remaining and zero TRX MUST enter `awaiting_resources` and source bandwidth, not broadcast and fail |
| TST-019 | **An attribution-window test MUST verify ORD-002b**: a payment with a `block_timestamp` inside the window but a `detected_at` after `expires_at` MUST be credited |
| TST-020 | **A dust-completion test MUST verify DET-007 + ORD-005c**: 24.6 + 0.4 USDT against a 25 USDT order MUST mark the order `paid`, not expire it `partial` |
| TST-021 | **A no-retry audit test MUST assert that the withdrawal path issues exactly one broadcast POST per withdrawal**, across every failure injection in the suite. Implemented as a counting fake node that fails the test on a second broadcast of the same txid |
| TST-022 | An `external_ref` mismatch test MUST verify API-002 returns 409 rather than a mismatched order |
| TST-023 | A `sequence_key` ordering test MUST verify that `withdrawal.broadcast` is always delivered before `withdrawal.confirmed` for the same withdrawal, under concurrent dispatchers |
