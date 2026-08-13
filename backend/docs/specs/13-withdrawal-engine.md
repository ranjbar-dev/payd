# 13. Withdrawal engine

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §13
**ID prefixes in this file:** `WDR-*`
**Related:** [`12-resource-management.md`](12-resource-management.md) (energy/bandwidth sourcing before signing), [`14-key-management.md`](14-key-management.md) (signing), [`05-data-model.md`](05-data-model.md) (`withdrawals` table, ARC-006a FULL connection), [`18-testing.md`](18-testing.md) (TST-014/015/016/021 are all anchored here)

**This is the highest-stakes file in the spec set.** §13.0's no-retry policy overrides any general retry rule stated elsewhere (including CHN-024's read-retry, which does NOT apply to broadcast — see CHN-024a).

---

Withdrawals are fully automated: the dashboard requests one, the daemon signs and broadcasts it without operator intervention beyond the API call.

## 13.0 No-retry policy

**This section governs the entire withdrawal and transfer path and overrides any general rule elsewhere in this specification.**

| ID | Requirement |
|---|---|
| WDR-000 | **No action that moves funds may ever be automatically re-attempted.** This covers: broadcasting a withdrawal, re-signing a withdrawal, bandwidth top-up transfers (RES-007b), and self-delegation broadcasts (RES-013). Each is attempted **at most once, ever**. There MUST be no retry count, no retry backoff, and no configuration key that enables one |
| WDR-000a | An ambiguous outcome MUST be resolved by **reconciliation**, never by re-attempting. Reconciliation means querying the chain — `gettransactionbyid` against the txid persisted before broadcast — to discover what the earlier attempt actually did. Reconciliation is unlimited and MUST be run at startup, on every engine tick, and after any worker restart |
| WDR-000b | When reconciliation cannot determine an outcome, the withdrawal MUST transition to **`needs_operator`**, emit `withdrawal.needs_operator`, and stop. The service MUST NOT guess. An operator decision is a valid terminal outcome; a duplicate payout is not |
| WDR-000c | A withdrawal in `failed`, `rejected`, or `needs_operator` MUST NOT be resumed, restarted, or reused. A new movement of funds requires a **new withdrawal request with a new `Idempotency-Key`**, which the operator makes deliberately after reading the reason. The service MUST NOT expose an endpoint that re-broadcasts an existing withdrawal row |
| WDR-000d | Retry remains correct and MUST be retained for operations with **no external side effect**: chain reads (CHN-024), the DET-005a receipt re-fetch, IPN delivery (IPN-008), and price polling. The distinguishing test is: *if this runs twice, can money move twice?* If yes, it may not be retried |

**Why.** A lost HTTP response is the most ordinary failure in web software, and on a blockchain it does not mean the transaction was lost. TronGrid frequently accepts and propagates a broadcast whose response never reaches the caller. A retry then returns `DUP_TRANSACTION_ERROR` — *"I already have this"* — which is confirmation of success, not rejection. Any design that treats an ambiguous broadcast as a failure and re-attempts it will eventually pay twice, and will do so with a clean audit trail on both payments. Removing retry from this path costs an occasional operator decision; keeping it costs the full withdrawal amount.

```
                        ┌─────────────────────────────────────┐
                        │  Broadcast attempted (exactly once) │
                        └──────────────────┬──────────────────┘
                                           │
              ┌────────────────────────────┼────────────────────────────┐
              │                            │                            │
        result: true            deterministic rejection          anything else
              │                 (SIGERROR, TAPOS_ERROR,      (DUP_TRANSACTION_ERROR,
              │                  CONTRACT_VALIDATE_ERROR…)    SERVER_BUSY, 5xx,
              │                            │                  timeout, conn reset)
              ▼                            ▼                            ▼
        ┌───────────┐              ┌──────────────┐            ┌──────────────┐
        │ broadcast │              │  chain check │            │  broadcast   │
        └─────┬─────┘              │  (WDR-022a)  │            │ (assume sent)│
              │                    └──────┬───────┘            └──────┬───────┘
              │                           │                           │
              └───────────────────────────┴─────────┬─────────────────┘
                                                    ▼
                                         ┌─────────────────────┐
                                         │ poll gettransaction │
                                         │  byid until final   │
                                         └──────────┬──────────┘
                                                    │
                        ┌───────────────────────────┼───────────────────────┐
                        ▼                           ▼                       ▼
                  ┌───────────┐             ┌─────────────┐        ┌────────────────┐
                  │ confirmed │             │   failed    │        │ needs_operator │
                  └───────────┘             │ (absent AND │        │  (unresolvable)│
                                            │  expired)   │        └────────────────┘
                                            └─────────────┘
```

## 13.1 Request and validation

| ID | Requirement |
|---|---|
| WDR-001 | `POST /api/v1/withdrawals` MUST require a valid API key with `withdrawals:write` scope, a valid TOTP code in the `X-TOTP` header (API-022a), and an `Idempotency-Key` header |
| WDR-001a | **`Idempotency-Key` MUST be resolved before TOTP validation.** If the key already exists, the stored withdrawal record MUST be returned with HTTP 200 and **no TOTP check performed**. TOTP is validated only on the path that creates a new row. Without this ordering, a legitimate client retry — which necessarily carries the same key *and the same TOTP code* — is indistinguishable from the replay attack API-022 exists to stop, and is rejected with 401. The operator then reasonably waits for a fresh code and resubmits, producing a second withdrawal for the same money |
| WDR-002 | The request MUST create a row in `requested` state and return immediately; signing happens asynchronously |
| WDR-002a | **Validation MUST be split explicitly.** *Synchronous, before the row is created, returning 4xx per API-024:* destination address validity (WDR-004), asset configured, source address owned and not `disabled`, amount parseable and positive, `drift_detected = 0` (BAL-002), daily limit (WDR-006), price freshness (WDR-006b), and confirmed balance (WDR-005) evaluated against `confirmed_raw`. *Asynchronous, in the engine:* resource acquisition, re-validation of balance under the claim lock, signing, broadcast. v1.1 returned 201 for a withdrawal exceeding the balance and then failed it seconds later out of band, where the dashboard — a separate project with no IPN endpoint — could not see the reason |
| WDR-002b | **`rejected` MUST denote a withdrawal refused before any on-chain action was attempted. `failed` MUST denote one where signing or broadcast was attempted and the transaction was confirmed absent from the chain.** The two MUST NOT be used interchangeably. In v1.1 `rejected` was in the enum and no requirement produced it |
| WDR-003 | A repeated `Idempotency-Key` MUST return the original withdrawal record, not create a duplicate |
| WDR-003a | If a request presents an existing `Idempotency-Key` with a **different** `from_address`, `to_address`, `asset`, or `amount`, the server MUST return HTTP 409 `idempotency_key_reuse` and MUST NOT return the stored record. That is a caller bug, not a retry |
| WDR-004 | The destination MUST be validated with `hdwallet.IsValidAddress(hdwallet.TRX, to)` before acceptance |
| WDR-005 | A withdrawal whose amount exceeds the source address's **`confirmed_raw`** for that asset MUST be rejected. `pending_raw` MUST NOT be spendable: paying out irreversible funds against a deposit that is still reorg-reversible is an unbounded loss with roughly a 60-second exposure window per deposit. The check MUST be re-evaluated inside the WDR-008 claim transaction, subtracting amounts held by other non-terminal withdrawals from the same address |
| WDR-006 | The sum of all withdrawals created since the start of the current **UTC** day (DB-002a) in states `requested`, `awaiting_resources`, `awaiting_energy`, `signing`, `broadcast`, or `confirmed`, plus this one, MUST NOT exceed `withdrawal.daily_limit_usd`, valued at the current price. Withdrawals in `failed` or `rejected` MUST be excluded **only once WDR-022a has confirmed the transaction is absent from the chain**. v1.1 counted only `broadcast` and `confirmed`, so two withdrawals from different addresses overlapping in `awaiting_energy` — the normal case, since energy acquisition takes up to 90 seconds — both saw a zero total and both passed a cap they jointly doubled |
| WDR-006a | The limit check and the row insert MUST occur in **one transaction**, expressed as a conditional insert, not a read-then-write |
| WDR-006b | If the price used for USD valuation is older than `price.stale_after`, withdrawal creation MUST be rejected with HTTP 503, matching ORD-009 |
| WDR-007 | **Exactly one withdrawal per source address may be in `awaiting_resources`, `awaiting_energy`, `signing`, or `broadcast` at a time.** The rationale is **balance contention and duplicated resource acquisition — not nonce conflict.** Tron has no account nonce; replay protection comes from the reference block, the expiration, and txID uniqueness, so two concurrent transfers from one address are both perfectly valid and both confirm if the balance covers them. v1.1 stated the wrong rationale and consequently omitted `awaiting_energy` from the set, allowing two withdrawals from one address to each rent energy and the second to fail on-chain after its rental was already paid for |
| WDR-008 | The engine MUST claim a withdrawal with a **single conditional update asserting both the row's own state and the address's exclusivity**: `UPDATE withdrawals SET status='awaiting_resources' WHERE id=? AND status='requested' AND NOT EXISTS (SELECT 1 FROM withdrawals WHERE address_id=? AND status IN ('awaiting_resources','awaiting_energy','signing','broadcast'))` |

## 13.2 Resource acquisition

| ID | Requirement |
|---|---|
| WDR-009a | Before signing a **TRC-20** withdrawal, the engine MUST check the source address's available energy against `resources.min_energy` |
| WDR-009b | If energy is sufficient, the withdrawal MUST proceed to the RES-006 bandwidth check with `energy_source = 'existing'` |
| WDR-009c | If energy is insufficient, the withdrawal MUST transition to `awaiting_energy` and run the [`12-resource-management.md`](12-resource-management.md) §12.2 sourcing chain |
| WDR-009d | On chain success, the withdrawal MUST record `energy_source` and `energy_cost_trx`, then proceed to the bandwidth check |
| WDR-009e | If every energy tier fails, the withdrawal MUST transition to `failed` with a reason naming the last tier attempted, and MUST NOT be retried automatically (WDR-000) |
| WDR-009f | **TRX** withdrawals MUST skip energy acquisition — native transfers consume bandwidth, not energy — but MUST NOT skip RES-006. v1.1 sent TRX withdrawals past the resource check entirely, on exactly the reasoning that makes the bandwidth check necessary |
| WDR-009g | Time spent in `awaiting_energy` MUST be bounded by `energy.poll_timeout`; the engine MUST NOT hold the address's WDR-007 exclusivity indefinitely |
| WDR-009h | Every withdrawal MUST pass RES-006's bandwidth check immediately before signing, regardless of asset |

## 13.3 Signing and broadcast

| ID | Requirement |
|---|---|
| WDR-010 | The engine MUST build a `tronpb.SigningInput` with: `ref_block_bytes` (bytes 6–8 of the big-endian reference block height), `ref_block_hash` (bytes 8–16 of the reference block ID), `expiration`, `timestamp`, and `fee_limit` (`withdrawal.fee_limit_trx` in sun) |
| WDR-010a | **`timestamp` and `expiration` MUST be derived from the timestamp of the reference block used for TAPOS, not from the local clock**: `timestamp = ref_block.timestamp`, `expiration = ref_block.timestamp + withdrawal.expiration`. TRON nodes validate transaction timestamps against their own clocks and reject transactions whose expiration is past or implausibly distant. A host clock 90 seconds fast — a suspended VM, a silently failed NTP sync — would cause every withdrawal to be rejected or to expire immediately, with a failure signature indistinguishable from an RPC problem. The follower already holds an authoritative block timestamp on every tick |
| WDR-011 | The reference block MUST come from the follower's cached recent tip, not a fresh RPC call — the follower already holds it |
| WDR-012 | The reference block MUST be no more than 10 blocks old; otherwise the engine MUST fetch a fresh one |
| WDR-013 | TRX withdrawals MUST use `TransferContract`; TRC-20 withdrawals MUST use the library's TRC-20 transfer support with the token's configured contract address |
| WDR-014 | The signed output MUST be converted with `BroadcastPayload(hdwallet.TRX, out)` and POSTed to `/wallet/broadcasttransaction` |
| WDR-014a | **`/wallet/broadcasttransaction` MUST be exempt from CHN-024's blanket retry** (see [`06-chain-follower.md`](06-chain-follower.md) CHN-024a). A broadcast MUST be attempted **at most once per withdrawal, for the lifetime of that withdrawal** — not once per engine tick, not once per restart |
| WDR-015 | The txid MUST be captured via `TransactionID(out)` before broadcast **and MUST be persisted to `withdrawals.txid` in a committed transaction on the `_synchronous=FULL` connection (ARC-006a), together with `broadcast_attempted_at`, before the broadcast request is issued.** Computing the txid without persisting it leaves a crashed engine unable to tell whether the transaction landed. This one requirement is what makes every ambiguous outcome below recoverable |
| WDR-016 | On `result: true`, status → `broadcast` with `broadcast_at` recorded, committed on the FULL connection |
| WDR-017 | **Broadcast responses MUST be classified into three outcomes.** **(a)** `result: true` → `broadcast`. **(b)** A *deterministic rejection* — `SIGERROR`, `TAPOS_ERROR`, `TRANSACTION_EXPIRATION_ERROR`, `CONTRACT_VALIDATE_ERROR`, `BANDWITH_ERROR` — → chain check per WDR-022a, then `failed` with the code recorded. **(c)** **Any other outcome — `DUP_TRANSACTION_ERROR`, `SERVER_BUSY`, HTTP 5xx, timeout, connection reset — MUST transition to `broadcast` with the persisted txid and MUST NOT be marked `failed`.** `DUP_TRANSACTION_ERROR` in particular means the node already holds the transaction, which is evidence it succeeded. The raw response MUST be stored in `broadcast_response` for audit |
| WDR-017a | The engine MUST NOT re-broadcast under any outcome. Once `broadcast_attempted_at` is set, that withdrawal's broadcast is spent |

## 13.4 Tracking and resolution

| ID | Requirement |
|---|---|
| WDR-020 | The engine MUST poll `POST /wallet/gettransactionbyid` for `broadcast` withdrawals every 15 seconds until confirmed or resolved. This is a read and is freely repeatable |
| WDR-021 | On solidification, status → `confirmed`, with `fee_raw` and `energy_used` recorded from the receipt |
| WDR-022 | A withdrawal whose transaction is confirmed absent from the chain past its `expiration` MUST transition to `failed` and MUST NOT be retried — the operator decides whether to submit a new request |
| WDR-022a | **Before any withdrawal transitions to `failed`, the engine MUST call `/wallet/gettransactionbyid` with the persisted txid and confirm the transaction is absent from the chain.** A withdrawal MUST NOT reach `failed` on the basis of a broadcast response alone. If the lookup itself cannot be completed, the withdrawal MUST go to `needs_operator`, not `failed` |
| WDR-023 | Outbound transfers MUST be ingested as ledger entries (DET-002b) so address balances decrease correctly |
| WDR-023a | The withdrawal's own on-chain fee (`fee_raw` from the receipt) and any TRX burned for energy or bandwidth MUST be recorded as a TRX debit against the source address **in the same transaction that sets `confirmed`**. Bandwidth top-ups and delegation fees MUST likewise be ledgered |
| WDR-024 | Every withdrawal request, approval, and outcome MUST be written to `audit_log` with the API key name and source IP |
| WDR-025 | The confirmed record MUST expose total cost — network fee plus `energy_cost_trx` plus any bandwidth cost — so the true cost per withdrawal is visible in the dashboard rather than hidden across three tables |

## 13.5 Startup and crash resolution

v1.1's conditional `requested → signing` update prevented double-signing by making `signing` a state with no exit: nothing anywhere transitioned a withdrawal out of it, the tracker polled only `broadcast` rows, and the per-address exclusivity guard meant one stuck row blocked that address permanently, requiring direct SQLite surgery to clear.

| ID | Requirement |
|---|---|
| WDR-018 | **On startup, and on every engine tick**, any withdrawal in `signing` with `broadcast_attempted_at` older than 60s, or in `broadcast` with no confirmation, MUST be resolved by calling `/wallet/gettransactionbyid` with its **persisted** txid: *found* → `broadcast` (then normal tracking); *absent and past `expiration`* → `failed`; *absent and within expiration* → leave and re-check next tick; *lookup unavailable* → leave; after 10 consecutive failed lookups → `needs_operator` |
| WDR-018a | **Startup recovery MUST treat any withdrawal with a non-NULL `txid` as potentially broadcast, regardless of what its `status` column says, and MUST resolve it on-chain before permitting any signing of that row.** This is the real protection against the ARC-006a power-loss window: if a `synchronous=NORMAL` commit is lost, the row may read back as `requested` while the transaction is irreversibly on-chain. `_synchronous=FULL` narrows the window; this check closes it |
| WDR-018b | A withdrawal in `signing` with a NULL `txid` MUST be transitioned to `needs_operator`, never re-signed. A NULL txid means the crash occurred before WDR-015 committed, which *probably* means nothing was broadcast — but "probably" is not a basis for moving funds a second time |
| WDR-019 | On startup, any withdrawal in `awaiting_energy` older than `energy.poll_timeout`, or in `awaiting_resources` older than 5 minutes, MUST be re-evaluated against the current on-chain resources of its source address, and any `energy_purchases` row in `purchased` for it MUST be reconciled or marked `expired` with the cost recorded |
| WDR-019a | Startup resolution MUST complete before the engine claims any new withdrawal, so a stale in-flight row cannot be bypassed |
| WDR-026 | `needs_operator` MUST be surfaced at `GET /api/v1/withdrawals?status=needs_operator`, MUST emit `withdrawal.needs_operator`, and MUST include the persisted txid and the last lookup error so the operator can check Tronscan directly |
