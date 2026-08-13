# Roadmap — building this project with Codex CLI

This is a step-by-step guide to building `payd` entirely with Codex CLI, one
implementation phase at a time, mirroring `docs/specs/19-implementation-phases.md`
(P1–P15). Each phase below gives you:

- **Goal** — what the phase delivers
- **Docs to point Codex at** — the exact spec files for this phase (Codex
  also always reads `AGENTS.md` automatically if it's in the repo root, which
  it is)
- **Prompt** — paste this into Codex CLI verbatim (adjust wording if you like,
  but keep the doc references and the "explain what you built" instruction —
  that's what makes the step reviewable)
- **How to review this step** — what to open and check before moving on,
  plus a question to ask Codex if something doesn't make sense
- **Gate** — copied from the phase table; don't move to the next phase until
  this is true

## Before you start

1. Run Codex CLI from the project root (`C:\Users\root\Desktop\tron-payment-proccesor`)
   so it picks up `AGENTS.md` and `docs/` automatically.
2. **Do not skip or merge phases.** Every phase's gate is a real checkpoint —
   later phases assume earlier ones actually work, not just compile.
   `docs/specs/19-implementation-phases.md` also flags that P5 and P11 depend
   on five specific fixes being in code first (broadcast classification,
   idempotency-before-TOTP ordering, bandwidth sourcing, reorg re-inclusion,
   and the asset filter in attribution) — since you're building straight from
   the v1.2 spec (not the flawed v1.1 this project's spec supersedes), these
   are simply part of the P2/P3/P4/P6 deliverables done correctly the first
   time. Don't let Codex shortcut them "to get something running."
3. After every phase, **read the code before starting the next prompt.** The
   review checklist below tells you where to look. If you don't understand
   something, ask Codex directly — it has full context on what it just wrote.
4. Set up a Nile or Shasta testnet wallet before P2; several gates require it.

---

## P1 — Project skeleton, config, store, seedtool

**Goal:** `cmd/payd`, `cmd/seedtool`, config loading with startup validation,
SQLite store opened with both connections, embedded migrations.

**Docs to point Codex at:**
`AGENTS.md`, `docs/specs/02-tech-stack-and-dependencies.md`,
`docs/specs/03-architecture-and-workers.md`, `docs/specs/04-configuration.md`,
`docs/specs/05-data-model.md`, `docs/specs/14-key-management.md`

**Prompt:**
```
Read AGENTS.md, then docs/specs/02-tech-stack-and-dependencies.md,
docs/specs/03-architecture-and-workers.md, docs/specs/04-configuration.md,
docs/specs/05-data-model.md, and docs/specs/14-key-management.md.

Implement Phase P1 from docs/specs/19-implementation-phases.md:
- cmd/payd and cmd/seedtool skeletons
- internal/config: YAML loading, validation per every CFG-* requirement
  (especially CFG-013/014/015), fail-fast on invalid config
- internal/store: SQLite opened per ARC-006, with the two connections
  described in ARC-006a (NORMAL and FULL), the full schema from
  docs/specs/05-data-model.md as go:embed migrations, tracked in
  schema_migrations
- cmd/seedtool: reads a BIP-39 mnemonic from stdin into a memguard buffer,
  encrypts it per KEY-001..008, writes seed.age at mode 0600, prints the xpub

When done, explain in plain English: which files you created, what each one
is responsible for, and which requirement IDs (CFG-*, ARC-*, KEY-*, DB-*) each
piece satisfies. Also tell me exactly how to run seedtool and payd locally to
verify the gate below.
```

**How to review this step:**
- Open `internal/config` — confirm it fails to start (not silently defaults)
  on a bad config value; check the CFG-013 resource-wallet-index collision
  check exists and is exercised.
- Open `internal/store` — find the two `sql.Open` (or equivalent) calls; confirm
  one sets `_synchronous=FULL` and is documented as reserved for irreversible
  writes (ARC-006a). Confirm nothing else opens a DB handle outside `internal/store`.
- Open `cmd/seedtool` — confirm the mnemonic is read into a `memguard` buffer,
  never assigned to a plain Go `string`.
- Ask Codex: *"Walk me through what happens, line by line, from `seedtool`
  writing seed.age to `payd` successfully loading the wallet at startup."*

**Gate:** `seedtool` round-trips a mnemonic; `payd` starts, derives 20 pool
addresses, and refuses to start on a resource-wallet index collision.

---

## P2 — TronGrid client

**Goal:** failover across distinct-host endpoints, circuit breaker, request
accounting, broadcast exempted from retry, chain-parameter polling (W-011).

**Docs to point Codex at:**
`docs/specs/06-chain-follower.md` §6.3, `docs/specs/12-resource-management.md` §12.5,
`docs/specs/13-withdrawal-engine.md` (read §13.0 and CHN-024a/WDR-014a even
though signing comes later — the client's retry exemption has to exist now),
`docs/specs/16-rate-limit-budget.md`

**Prompt:**
```
Read docs/specs/06-chain-follower.md §6.3 (Endpoint management),
docs/specs/12-resource-management.md §12.5 (Chain parameters),
docs/specs/13-withdrawal-engine.md §13.0 and CHN-024a/WDR-014a specifically,
and docs/specs/16-rate-limit-budget.md.

Implement Phase P2 from docs/specs/19-implementation-phases.md:
- internal/chain: a TronGrid client satisfying CHN-020..026 (API key header,
  429/403 backoff and failover, circuit breaker, distinct-host validation
  per CHN-025/CFG-015, requests-per-day accounting per DB-002a's UTC boundary)
- Read endpoints (getnowblock, getblockbynum, gettransactioninfobyblocknum,
  gettransactionbyid, getaccountresource, getchainparameters, account-history)
  get the CHN-024 retry-twice-on-network-error behavior
- broadcasttransaction gets NO retry under any circumstance — make this
  structurally obvious in the code (e.g. a distinct method with no retry
  wrapper), not just a comment
- W-011: fetch getEnergyFee/getTransactionFee at startup and every 6h into
  chain_params (RES-020..023)

Write a unit test that proves broadcast is never retried even under
simulated network errors (this is this phase's explicit gate).

When done, explain which files you created, how the retry-vs-no-retry split
is enforced in the type system or method structure (not just by convention),
and which requirement IDs each piece satisfies.
```

**How to review this step:**
- Find the broadcast method in `internal/chain` — confirm there is no loop,
  no retry count, and ideally that it's a different function signature or
  type than the read methods, so a future change can't accidentally wrap it
  in retry logic.
- Check the CHN-025 distinct-host validation runs at startup, not just in a test.
- Run the new unit test yourself; read what it actually asserts.
- Ask Codex: *"If I added a third TronGrid endpoint pointing at the same
  hostname as the first, what exactly stops the service from starting?"*

**Gate:** Integration test against Nile testnet passes; a unit test proves
broadcast is never retried.

---

## P3 — Chain follower

**Goal:** 3s block polling, gap detection, height-regression guard, and
double-confirmed reorg detection.

**Docs to point Codex at:** `docs/specs/06-chain-follower.md` (full),
`docs/specs/05-data-model.md` (`blocks`, `crawler_state`),
`docs/specs/18-testing.md` (TST-002, TST-003, TST-003a)

**Prompt:**
```
Read docs/specs/06-chain-follower.md in full, the blocks/crawler_state tables
in docs/specs/05-data-model.md, and TST-002/003/003a in docs/specs/18-testing.md.

Implement Phase P3 from docs/specs/19-implementation-phases.md:
- internal/follower: the W-001 worker polling getnowblock every 3s
- Gap detection and catch-up mode per CHN-003/004/005
- The crawler cursor advances in the same transaction as the block's payment
  writes (CHN-006), with all RPC completed before that transaction opens
  (CHN-006a, ARC-007) — payment writes themselves land in P4/P5, so for now
  make sure the transaction boundary is structurally correct even with an
  empty or stub matcher call
- Height-regression guard (CHN-007a) and idempotent same-height polls (CHN-007)
- Reorg detection requiring two confirmed mismatches one poll apart (CHN-011a)
  before invoking reorg handling (CHN-012), and the reorg-depth-exceeded halt
  (CHN-015)
- Recorded block fixtures per TST-002, and the reorg tests TST-003/TST-003a

When done, explain which files you created, how the "two mismatches, one poll
apart" confirmation works, and which requirement IDs each piece satisfies.
```

**How to review this step:**
- Find where the crawler cursor UPDATE and the payment writes happen — confirm
  they're in the same DB transaction (grep for the transaction begin/commit).
- Find the reorg-suspicion code — confirm a single parent-hash mismatch does
  NOT trigger reorg handling, only a second one after a real poll interval.
- Run `go test ./internal/follower/...` yourself and read the reorg test to
  understand what scenario it's actually replaying.
- Ask Codex: *"Show me the exact sequence of RPC calls and DB writes for one
  normal 3-second tick, and for one tick that detects a suspected reorg."*

**Gate:** Replays 1,000 recorded blocks; TST-003 and TST-003a pass.

---

## P4 — Decoder

**Goal:** TRX + TRC-20 two-tier decoding with the canonical `log_index` and
bidirectional (in/out) screening.

**Docs to point Codex at:** `docs/specs/07-payment-detection.md` §7.1,
`docs/specs/05-data-model.md` (`payments` table)

**Prompt:**
```
Read docs/specs/07-payment-detection.md §7.1 (Two-tier decoding) and the
payments table in docs/specs/05-data-model.md.

Implement Phase P4 from docs/specs/19-implementation-phases.md:
- internal/decode: Tier 1 raw-block screening (DET-001..004) matching BOTH
  directions per DET-002b, Tier 2 receipt-based crediting for TRC-20 that
  reads ONLY the Transfer event log, never calldata (DET-004a)
- The canonical log_index definition (DET-002a) applied identically
  everywhere log_index is computed — this is what makes DB-004's uniqueness
  constraint actually hold
- Tier 2 replaces Tier 1 data outright on disagreement (DET-006)
- Wire this into the follower from P3: on a Tier 1 hit, fetch the Tier 2
  receipt; on receipt-fetch failure, do NOT commit the block or advance the
  cursor (DET-005a)

Write the fixture-based tests from TST-002: a TRX transfer, a TRC-20 transfer,
a failed contract call, a reverted transfer, a transaction with multiple
Transfer logs, an outbound transfer from an owned address, and an empty block.

When done, explain which files you created, walk through exactly how
log_index is computed for a TRC-20 payment with two Transfer logs in one
transaction, and which requirement IDs each piece satisfies.
```

**How to review this step:**
- Open the TRC-20 crediting code path — confirm the amount and recipient come
  from the decoded event log, not from the transaction's input calldata.
- Check the log_index computation used for TRC-20 is "index within this
  transaction's own log array," not a block-wide counter.
- Run the fixture tests; read the multi-log-transaction fixture specifically
  since it's the one most likely to expose an off-by-one in log_index.
- Ask Codex: *"Give me a concrete example of a transaction where Tier 1 and
  Tier 2 disagree, and show me which value wins and why."*

**Gate:** All fixture cases decode correctly, including multi-log transactions
and outbound transfers.

---

## P5 — Address pool, order lifecycle, matcher, Lifecycle Worker

**Goal:** the address pool state machine, order state machine, payment-to-order
matching, and the clock-driven Lifecycle Worker (W-010).

**Docs to point Codex at:** `docs/specs/08-order-lifecycle-and-address-pool.md`
(full), `docs/specs/05-data-model.md` (`addresses`, `orders`)

**Prompt:**
```
Read docs/specs/08-order-lifecycle-and-address-pool.md in full and the
addresses/orders tables in docs/specs/05-data-model.md.

Implement Phase P5 from docs/specs/19-implementation-phases.md:
- internal/wallet (or wherever addresses live): the pool state machine
  free -> assigned -> cooling -> free per POOL-001..008, with POOL-006's
  exclusivity enforced by a WHERE state='free' guard in the update, not an
  application-level check
- internal/matcher: order attribution per ORD-002/002a/002b — the asset
  filter and the assignment-window check are both mandatory from day one
  (this is exactly what F-1 and F-7 in docs/specs/21-appendix-review-findings.md
  were about)
- The full order state machine (ORD-001..011), including expired_funded/
  cancelled_funded (ORD-005a/005b/005d) and unattributed payments (ORD-020..024)
- internal/lifecycle: the W-010 worker per LIF-001..005 — expiry, cooldown
  return, pool top-up, used_totp pruning — with NO network I/O (LIF-005)

Write TST-005 (concurrent order creation never double-assigns an address),
TST-017 (wrong-asset payment stays unattributed), TST-019 (attribution window
uses block_timestamp not detected_at), and TST-020 (dust top-up completes a
partial order).

When done, explain which files you created, what triggers an order to move
between each state, and which requirement IDs each piece satisfies.
```

**How to review this step:**
- Open the address-assignment SQL — confirm it's a single conditional UPDATE,
  not a SELECT followed by an UPDATE.
- Open the matcher — find where asset is compared and where the assignment
  window (`[created_at, released_at)`) is checked; confirm `detected_at` is
  never used for attribution decisions.
- Confirm `internal/lifecycle` makes zero RPC/HTTP calls — grep for any client
  usage in that package; there should be none.
- Ask Codex: *"Walk me through what happens to an order and its address if
  the order expires with a partial payment already received."*

**Gate:** TST-005, TST-017, TST-019, TST-020 pass; orders expire on a quiet chain.

---

## P6 — Confirmation tracker

**Goal:** solidified-height tracking and `seen → confirmed` promotion by
block identity, not just height.

**Docs to point Codex at:** `docs/specs/09-confirmation-tracking.md` (full)

**Prompt:**
```
Read docs/specs/09-confirmation-tracking.md in full.

Implement Phase P6 from docs/specs/19-implementation-phases.md:
- internal/confirm: the W-003 worker polling walletsolidity/getnowblock every
  20s (CNF-001)
- The seen->confirmed promotion per CNF-002 — ALL FOUR conditions (solidified
  height, block_id identity match, unbroken ancestry, confirmations_required
  depth), not height alone
- Monotonic solidified_height storage (CNF-002a) and deference to unresolved
  reorg suspicion (CNF-002b)
- Order promotion to confirmed when all contributing payments are confirmed
  (CNF-003)
- The confirmations field computation for IPN payloads (CNF-007)

Write a test proving that an orphaned block sitting at a solidified height
does NOT get its payments promoted to confirmed just because the height
threshold is met.

When done, explain which files you created, exactly which four conditions
gate a promotion, and which requirement IDs each piece satisfies.
```

**How to review this step:**
- Find the promotion query/logic — count the conditions checked; there should
  be four, and block_id identity must be one of them (not height alone).
- Confirm `solidified_height` is written with a `MAX()`-style monotonic update.
- Ask Codex: *"If the solidity endpoint briefly reports a lower height than
  before, what happens to solidified_height in the database?"*

**Gate:** Two-stage transition verified on testnet; an orphaned block at a
solidified height does not promote.

---

## P7 — IPN dispatcher

**Goal:** transactional outbox, per-consumer HMAC signing, strict per-pair
delivery ordering, dead-lettering.

**Docs to point Codex at:** `docs/specs/10-ipn-dispatcher.md` (full),
`docs/specs/04-configuration.md` (consumer config)

**Prompt:**
```
Read docs/specs/10-ipn-dispatcher.md in full and the ipn.consumers section of
docs/specs/04-configuration.md.

Implement Phase P7 from docs/specs/19-implementation-phases.md:
- internal/ipn: the W-004 dispatcher
- Transactional outbox writes co-located with state changes (IPN-001) —
  since P5/P6 already produce state changes, wire outbox inserts into those
  transactions now
- Per-consumer HMAC signing (IPN-006/007), 2xx-only success (IPN-008),
  backoff/dead-lettering (IPN-009/010)
- Strict delivery ordering per (sequence_key, consumer) pair (IPN-011), with
  head-of-line blocking scoped to that pair only (IPN-012), concurrent across
  pairs (IPN-013)
- The immutable payload snapshot plus current_status/snapshot_age_seconds
  added only at send time (IPN-021a) — do not rebuild the payload at send time

Write TST-011 (one slow consumer doesn't delay another, each gets a
correctly-signed request) and TST-023 (withdrawal.broadcast always precedes
withdrawal.confirmed for the same withdrawal under concurrent dispatch).

When done, explain which files you created, how ordering is enforced per
(sequence_key, consumer) pair specifically, and which requirement IDs each
piece satisfies.
```

**How to review this step:**
- Confirm outbox rows are written inside the SAME transaction as the order/
  payment state change that produced them, not in a follow-up write.
- Find the signature computation — confirm it uses each consumer's own secret,
  not a single global secret.
- Read the ordering implementation — confirm grouping is on `(sequence_key,
  consumer)`, and that `sequence_key` is never NULL for global events.
- Ask Codex: *"Show me the two events for one withdrawal — broadcast and
  confirmed — and prove to me they can't be delivered out of order."*

**Gate:** TST-011 and TST-023 pass.

---

## P8 — Price poller

**Goal:** Binance price polling with a staleness gate.

**Docs to point Codex at:** `docs/specs/11-price-service.md` (full)

**Prompt:**
```
Read docs/specs/11-price-service.md in full.

Implement Phase P8 from docs/specs/19-implementation-phases.md:
- internal/price: the W-005 poller (PRC-001), price.Provider interface with
  Binance as the default implementation (PRC-003)
- USDT/USDC treated as 1.00 USD without an API call (PRC-002)
- Failed fetch leaves the last known good price untouched (PRC-004)
- Expose a staleness check consumable by order creation and withdrawal
  creation (PRC-005) — even though those callers land in later phases, build
  the check now as a clean function/method they'll call

When done, explain which files you created, how a caller checks "is the
price stale," and which requirement IDs each piece satisfies.
```

**How to review this step:**
- Confirm a failed Binance fetch does not overwrite the existing `prices` row.
- Confirm the staleness check is a reusable function, not duplicated logic
  waiting to happen in two later call sites.
- Ask Codex: *"If Binance is down for an hour, what does the price service do,
  and what will order creation see?"*

**Gate:** Stale-price gate blocks both order and withdrawal creation (you can
verify this now with a stub caller, and re-verify for real once P9/P11 land).

---

## P9 — REST API

**Goal:** orders, payments, wallets endpoints; auth; TOTP with persisted
single-use state; rate limiting.

**Docs to point Codex at:** `docs/specs/15-rest-api.md` (full),
`docs/specs/04-configuration.md` (`auth`), `docs/specs/05-data-model.md`
(`used_totp`), `docs/specs/08-order-lifecycle-and-address-pool.md` (orders
endpoints' backing logic)

**Prompt:**
```
Read docs/specs/15-rest-api.md in full, the auth section of
docs/specs/04-configuration.md, the used_totp table in
docs/specs/05-data-model.md, and re-check
docs/specs/08-order-lifecycle-and-address-pool.md for the orders logic these
endpoints expose.

Implement Phase P9 from docs/specs/19-implementation-phases.md:
- internal/api: orders endpoints (§15.1), payments endpoints (§15.4 payments
  portion), auth middleware (API-020/021), TOTP validation persisted in
  used_totp (API-022) — NOTE the validation ORDER isn't fully wired until
  P11 (WDR-001a), but build TOTP validation itself correctly now
- API-002's exact-match idempotency for external_ref (not a blind 200)
- Rate limiting per API-023, the consistent error envelope per API-024,
  cursor pagination per API-025

Write TST-022 (external_ref mismatch returns 409, not a wrong order).

When done, explain which files you created, how a request maps from route to
handler to internal/store call, and which requirement IDs each piece satisfies.
```

**How to review this step:**
- Trigger `POST /orders` twice with the same `external_ref` but different
  amounts — confirm you get a 409 with details, not a 200 with the old order.
- Confirm TOTP replay within the same 30s step is rejected, and that this
  survives a process restart (i.e. it's checked against the DB, not memory).
- Ask Codex: *"What exactly happens if two requests hit POST /orders with the
  same external_ref at the same moment?"*

**Gate:** Full API test suite passes; TST-022 passes.

---

## P10 — Wallet monitor and balances

**Goal:** three-column balance maintenance, tiered resource polling, drift
detection consumed by withdrawal validation.

**Docs to point Codex at:** `docs/specs/12-resource-management.md` §12.1,
`docs/specs/05-data-model.md` (`balances`, BAL-001/002),
`docs/specs/16-rate-limit-budget.md`

**Prompt:**
```
Read docs/specs/12-resource-management.md §12.1 (Monitoring), the balances
table and BAL-001/002 in docs/specs/05-data-model.md, and
docs/specs/16-rate-limit-budget.md.

Implement Phase P10 from docs/specs/19-implementation-phases.md:
- internal/wallet: the W-006 resource monitor with tiered polling per
  RES-001a — fast tier (<=50 addresses, by balance) on check_interval, slow
  tier on slow_check_interval, zero-balance addresses never polled (RES-002)
- confirmed_raw/pending_raw recomputed from payments in the SAME transaction
  as any payment status change or insert (BAL-001) — this likely means
  revisiting the P3-P6 payment-write code to add this recomputation, not
  just adding it here in isolation
- W-007 reconciler: 6-hourly chain_raw verification and drift_detected
  (RL-003), and BAL-002's 409 balance_drift rejection plus the clear-drift
  endpoint and balance.drift_detected IPN

When done, explain which files you created (including any changes to earlier
phases' payment-write code for BAL-001), and which requirement IDs each piece
satisfies.
```

**How to review this step:**
- Confirm balance recomputation happens inside the same transaction as the
  payment write — this is easy to get wrong by doing it as a follow-up query.
- Confirm the fast-tier address selection is bounded by `max_polled_addresses`
  and sorted by descending balance.
- Ask Codex: *"Show me the query that decides which addresses get polled on
  the fast 5-minute tier versus the slow 6-hour tier."*

**Gate:** `needs-resources` returns correct data on testnet; BAL-002 blocks a
drifting address.

---

## P11 — Withdrawal engine

**Goal:** the full withdrawal path — sync/async validation split, resource
acquisition trigger, signing, single broadcast, on-chain resolution, daily
limit, crash recovery. **This is the highest-risk phase in the project.**

**Docs to point Codex at:** `docs/specs/13-withdrawal-engine.md` (full — read
every section, don't skim), `docs/specs/14-key-management.md`,
`docs/specs/12-resource-management.md` (WDR-009* interacts with §12.1),
`docs/specs/18-testing.md` (TST-014/015/016/021)

**Prompt:**
```
Read docs/specs/13-withdrawal-engine.md in FULL — every subsection, including
the ASCII diagram in §13.0. This is the most safety-critical file in the
project. Also read docs/specs/14-key-management.md and re-check
docs/specs/12-resource-management.md for how WDR-009a..h call into resource
acquisition. Read TST-014, TST-015, TST-016, TST-021 in docs/specs/18-testing.md
before writing any code, since your implementation must satisfy them.

Implement Phase P11 from docs/specs/19-implementation-phases.md:
- internal/withdraw: the W-008 engine
- POST /api/v1/withdrawals with WDR-001a's exact ordering: resolve
  Idempotency-Key BEFORE validating TOTP
- The sync/async validation split per WDR-002a exactly as specified
- WDR-005/006/006a/006b/007/008: confirmed_raw-only spending, the UTC daily
  limit computed in one transaction, and the per-address exclusivity claim
  as a single conditional UPDATE
- Reference-block-derived timestamp/expiration per WDR-010a (NOT the local
  clock)
- WDR-015: persist txid + broadcast_attempted_at on the FULL connection
  BEFORE issuing the broadcast request — this single ordering is what makes
  crash recovery possible
- WDR-017's exact three-way broadcast response classification — get this
  exactly right, especially that DUP_TRANSACTION_ERROR and similar ambiguous
  outcomes go to `broadcast`, never `failed`
- WDR-018/018a/018b/019/019a: startup and per-tick resolution that treats any
  non-NULL txid as potentially broadcast regardless of the status column, and
  resolves via gettransactionbyid before any new signing is permitted

Implement TST-014 (DUP_TRANSACTION_ERROR ends in confirmed, never failed),
TST-015 (idempotent replay returns 200 not 401), TST-016 (crash between txid
commit and broadcast response resolves on restart, never re-signs), and
TST-021 (exactly one broadcast POST per withdrawal across every failure
injection in the suite).

When done, explain which files you created, walk through what happens to a
withdrawal if the process is killed one second after the broadcast request is
sent but before the response arrives, and which requirement IDs each piece
satisfies. Be explicit about where in the code the "attempted at most once,
ever" guarantee actually lives.
```

**How to review this step — spend real time here:**
- Find the broadcast call site. Confirm there is exactly one call to it per
  withdrawal ID anywhere in the codebase, and that `broadcast_attempted_at`
  is checked before any code path could call it again.
- Confirm the txid-persist-before-broadcast ordering (WDR-015) in the actual
  commit sequence, not just in a comment.
- Read the three-way response classification (WDR-017) line by line against
  the requirement text — this is the single most important piece of logic in
  the whole project.
- Run TST-014, TST-015, TST-016, TST-021 yourself and read what each one
  actually simulates.
- Ask Codex: *"Show me every single code path that can set
  withdrawals.status = 'failed', and for each one, prove that
  WDR-022a's absent-from-chain check ran first."*
- Ask Codex: *"If I kill the process right after WDR-015's commit but before
  the HTTP request to broadcasttransaction is even sent, what happens on
  restart?"*

**Gate:** Testnet withdrawal completes end to end; TST-014, TST-015, TST-016,
TST-021 pass. Do not proceed to P12 until you personally understand and agree
with the answers to the two questions above.

---

## P12 — Self-delegation and bandwidth sourcing

**Goal:** tier-2 energy self-delegation via raw_json signing, and the
bandwidth-sourcing mechanism that lets USDT-only addresses pay for their own
withdrawal.

**Docs to point Codex at:** `docs/specs/12-resource-management.md` §12.3 and
§12.4, `docs/specs/02-tech-stack-and-dependencies.md` (GAP-002, raw_json mode)

**Prompt:**
```
Read docs/specs/12-resource-management.md §12.3 (Self-delegation) and §12.4
(Bandwidth sourcing) in full, and re-read GAP-002 in
docs/specs/02-tech-stack-and-dependencies.md for why raw_json mode is needed
here.

Implement Phase P12 from docs/specs/19-implementation-phases.md:
- RES-010..015: delegateresource call, raw_json signing with the txID guard
  NOT bypassed (RES-012), broadcast subject to the same §13.0 no-retry rule,
  tracked in resource_grants
- RES-006/007/008/009: the bandwidth check before EVERY withdrawal (not just
  TRC-20), sourcing via delegation or TRX top-up per bandwidth_strategy, and
  the top-up broadcast also following the no-retry rule
- RES-016: can_withdraw in the wallets API reflects bandwidth sufficiency,
  not just energy

Write TST-018 (a second TRC-20 withdrawal from an address with low bandwidth
and zero TRX enters awaiting_resources and sources bandwidth, rather than
broadcasting and failing on-chain).

When done, explain which files you created, why the raw_json txID guard
matters, and which requirement IDs each piece satisfies.
```

**How to review this step:**
- Confirm the raw_json signing path checks the txID guard and does not skip
  it under any configuration.
- Confirm TRX withdrawals (not just TRC-20) go through the RES-006 bandwidth
  check — this was a real defect in the v1.1 design this spec supersedes.
- Ask Codex: *"Explain the txID guard in raw_json mode — what attack does it
  actually prevent?"*

**Gate:** Delegation confirmed on testnet; TST-018 passes.

---

## P13 — Energy provider integration

**Goal:** tier-1 rented energy via a third-party provider, and the complete
three-tier fallback chain computed against live chain parameters.

**Docs to point Codex at:** `docs/specs/12-resource-management.md` §12.2
(full), `docs/specs/04-configuration.md` (`energy` block)

**Prompt:**
```
Read docs/specs/12-resource-management.md §12.2 in full, paying particular
attention to the note that tier-3 cost is a live formula, not a fixed figure,
and the energy block in docs/specs/04-configuration.md.

Implement Phase P13 from docs/specs/19-implementation-phases.md:
- ENR-001: the energy.Provider interface exactly as specified, including the
  required resourceType parameter
- ENR-003..012: tier-1 attempt-first ordering, max_price_trx quote rejection,
  poll-for-arrival with timeout, fallback-through on failure, purchase
  auditing in energy_purchases, provider balance checks, low-balance alerts,
  and the 5-failures/10-minutes circuit breaker
- ENR-013/014: no signing authority given to the provider; provider calls
  excluded from the TronGrid quota
- ENR-016/017: burn cost computed from the LIVE chain_params, and
  max_burn_trx validated at startup against the live getEnergyFee with a
  /readyz degradation if it would refuse a worst-case transfer
- TST-013's fake provider implementation so no test needs a real prepaid
  balance

Write TST-012: the full fallback chain — over-priced quote -> self-delegation
fails -> burn exceeds cap -> withdrawal fails cleanly.

When done, explain which files you created, show me the exact computation for
estimated_burn_trx, and which requirement IDs each piece satisfies.
```

**How to review this step:**
- Confirm nowhere in the codebase is `getEnergyFee` hardcoded as a constant —
  grep for suspicious numeric literals near energy/burn calculations.
- Confirm the provider's API key/secret never appear in a signing call.
- Ask Codex: *"If getEnergyFee doubles between two 6-hour polls, what changes
  in the service's behavior, automatically, without a restart?"*

**Gate:** TST-012 passes; a real rented withdrawal completes on mainnet with a
small amount.

---

## P14 — Observability, reconciler, backup docs

**Goal:** metrics, health checks, clock-skew detection, the full both-assets/
both-directions reconciler, and documented backup/recovery.

**Docs to point Codex at:** `docs/specs/17-operations.md` (full),
`docs/specs/07-payment-detection.md` §7.2 (safety net, now fully wired),
`docs/specs/16-rate-limit-budget.md` (RL-006 projection)

**Prompt:**
```
Read docs/specs/17-operations.md in full, re-read
docs/specs/07-payment-detection.md §7.2 to confirm the safety net covers both
assets and both directions with full cursor iteration, and re-read
docs/specs/16-rate-limit-budget.md for RL-006.

Implement Phase P14 from docs/specs/19-implementation-phases.md:
- OPS-001..007: /readyz with every listed degradation condition, /healthz,
  the full Prometheus metrics list, clock-skew detection against the latest
  block header timestamp (OPS-005)
- RL-006: 7-day quota projection with the 60%/90% thresholds
- The DET-010/010a/010b/010c/011 safety net exactly as specified, if not
  already fully wired from earlier phases — verify TRX is covered, not just
  TRC-20
- OPS-010..014: verify sqlite3 .backup works while running, and write the
  actual recovery documentation described in OPS-011/012/013/014 (a markdown
  doc, not just code)

When done, explain which files and docs you created/changed, run through what
/readyz reports under a simulated 40-second clock skew, and which requirement
IDs each piece satisfies.
```

**How to review this step:**
- Hit `/readyz` yourself in a few degraded conditions (stale price, stopped
  follower) and confirm it actually returns 503 with a reason.
- Read the recovery documentation Codex wrote — could you actually follow it
  during a real incident, or is it vague?
- Ask Codex: *"Walk me through recovering this service from a total database
  loss, using only the mnemonic."*

**Gate:** Runs 72h on testnet with no drift; quota projection reports correctly.

---

## P15 — Mainnet soak

**Goal:** validate the whole system on mainnet with small real amounts before
trusting it with real volume.

**Docs to point Codex at:** `docs/specs/19-implementation-phases.md` (the
gate itself), `docs/specs/17-operations.md` (what to watch)

**Prompt:**
```
I'm about to run Phase P15 from docs/specs/19-implementation-phases.md —
mainnet soak with small amounts. Before I do, review the codebase against
every MUST requirement in docs/specs/13-withdrawal-engine.md and
docs/specs/12-resource-management.md one more time, and give me a written
checklist of anything you're not fully confident satisfies its requirement,
with the specific requirement ID and file/line. Do not implement fixes yet —
just report gaps.
```

**How to review this step:** This phase is primarily manual operation, not
new code. Read Codex's gap report carefully — treat any flagged item touching
`internal/withdraw` as blocking until resolved. Only after that, run the soak
with small real amounts and watch `/metrics` and `payd_withdrawals_needs_operator`
closely.

**Gate:** 100 real orders processed correctly; energy cost per withdrawal
matches expectations; zero `needs_operator` withdrawals.
