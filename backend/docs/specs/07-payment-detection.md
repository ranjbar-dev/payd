# 7. Payment detection

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §7
**ID prefixes in this file:** `DET-*`
**Related:** [`06-chain-follower.md`](06-chain-follower.md) (Tier 1/2 run inside the follower's block processing), [`05-data-model.md`](05-data-model.md) (`payments.log_index`, DB-004), [`08-order-lifecycle-and-address-pool.md`](08-order-lifecycle-and-address-pool.md) (dust interacts with ORD-005c)

---

Tron's free tier does not permit fetching a receipt for every block. The pipeline therefore screens cheaply from the raw block and pays for receipts only on a hit.

## 7.1 Two-tier decoding

| ID | Requirement |
|---|---|
| DET-001 | **Tier 1 — raw block screen.** For each transaction in the block, inspect `raw_data.contract[0].type` |
| DET-002 | `TransferContract` → a native TRX transfer. Extract `owner_address`, `to_address`, `amount` |
| DET-002a | **`log_index` MUST be defined canonically, and identically in all three ingest paths:** `0` for native TRX `TransferContract` payments; the **zero-based index of the `Transfer` event within that transaction's own log array** for TRC-20 payments. Block-wide indexing is forbidden. No other derivation is permitted anywhere. DB-004's idempotency guarantee rests entirely on this |
| DET-002b | **Tier 1 screening MUST match on both directions.** A `TransferContract` whose `owner_address` is owned, and a TRC-20 `transfer` whose `owner_address` is owned, MUST be recorded with `direction = 'out'` as ledger debits. v1.1 screened only on owned *destinations*, so no withdrawal ever reduced a balance and the ledger inflated permanently with every outbound transfer |
| DET-003 | `TriggerSmartContract` → check `contract_address` against the configured token list. If matched, check `data` for the `a9059cbb` selector (`transfer(address,uint256)`), decode the 32-byte destination and 32-byte amount arguments |
| DET-004 | Transaction success MUST be read from `ret[0].contractRet == "SUCCESS"` in the raw block. Failed and reverted transactions MUST be discarded |
| DET-004a | **A TRC-20 payment MUST be credited only from a `Transfer` event log in the Tier 2 receipt, never from calldata.** Tier 1 calldata screening determines *whether* to fetch the receipt; it MUST NOT determine the amount or the fact of transfer. `contractRet == "SUCCESS"` is necessary but not sufficient: the TRC-20 standard permits a token to return `false` instead of reverting, producing a successful transaction containing no transfer, and fee-on-transfer or rebasing tokens deliver less than the calldata amount. Calldata is a request; the event log is the record of what happened |
| DET-005 | **Tier 2 — receipt fetch on hit.** Only when Tier 1 finds at least one transfer to or from an owned address does the follower call `POST /wallet/gettransactioninfobyblocknum` for that block, to obtain authoritative `Transfer` event logs (`topic0 = ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef`) and the actual energy/fee consumed |
| DET-005a | **If the Tier 2 receipt fetch fails for a block containing a Tier 1 hit, the block MUST NOT be committed and the crawler cursor MUST NOT advance.** The follower MUST re-attempt the block on the next tick under the client's backoff. A block MUST NOT be committed from Tier 1 data alone. (This is a read retry, permitted under CHN-024 — nothing has been written and nothing has moved) |
| DET-006 | Tier 2 data MUST **replace** Tier 1 data, never merely enrich it. Where the two disagree on amount, recipient, or the existence of a transfer, Tier 2 is authoritative and Tier 1's value MUST be discarded |
| DET-007 | A payment below the asset's configured `min_deposit` MUST be recorded with `is_dust = 1`. A dust payment MUST update the address balance and MUST NOT, **on its own**, trigger an order state change or an IPN. It MUST still contribute to `received_raw`, so that a dust top-up completing a partial order is credited on the next non-dust event or at the ORD-005c expiry re-check. v1.1 had no column to hold the flag and simultaneously required ORD-002 to sum all non-orphaned payments, so the two requirements could not both be satisfied — one reading stranded full payments from customers whose wallets split a transfer |

**Known limitation.** Tier 1 misses TRC-20 transfers executed *internally* by another contract — for example a payment routed through a DEX aggregator or a smart-contract wallet. These produce a `Transfer` log without matching `a9059cbb` calldata to a watched address. Mitigated by DET-010.

## 7.2 Safety net

| ID | Requirement |
|---|---|
| DET-010 | Every 5 minutes, the reconciler MUST query **both** `GET /v1/accounts/{address}/transactions/trc20?only_to=true&min_timestamp=…&limit=200` **and** `GET /v1/accounts/{address}/transactions?only_to=true&min_timestamp=…&limit=200` (native TRX) for every address in `assigned` state, following the `fingerprint` cursor **until the result set is exhausted**, and ingest any transfer not already present in `payments`. v1.1 queried the TRC-20 endpoint only, leaving native TRX — a first-class configured asset — with exactly one detection path and no backstop |
| DET-010a | The safety net MUST resolve `log_index` by fetching `POST /wallet/gettransactioninfobyid` for any txid it intends to insert, and MUST NOT insert with an assumed index. An assumed index writes the same economic event under a different key than the follower would, defeating DB-004 |
| DET-010b | `min_timestamp` MUST be `(block_timestamp of the newest payment ingested for that address) − 600`, never the previous run's start or end time. A run that partially fails must not create a permanent gap |
| DET-010c | The safety net MUST run a **second pass without `only_to`** to capture outbound transfers as ledger debits (DET-002b) |
| DET-011 | Every 6 hours, the reconciler MUST run the same checks — both endpoints, both directions, full cursor iteration — across all addresses in `assigned` and `cooling` state |
| DET-012 | Payments discovered by the safety net MUST follow the identical matching path as those from the follower |
| DET-013 | If the safety net finds a payment the follower missed, it MUST log a warning with the txid so the gap can be investigated |

## 7.3 Address activation

A Tron address does not exist on-chain until it receives its first transfer. Deposit addresses will therefore be unactivated on issue.

| ID | Requirement |
|---|---|
| DET-020 | `addresses.is_activated` MUST be set on the first confirmed inbound payment |
| DET-021 | The API MUST NOT report an unactivated address as an error condition — it is the normal state for a fresh pool entry |
| DET-022 | Documentation MUST note that the *sender* pays the account-creation fee (approximately 1 TRX, or bandwidth plus a smaller fee where the sender has staked bandwidth) when paying to an unactivated address, and that a TRC-20 transfer to a fresh address costs the sender roughly twice the energy of one to an address already holding that token |
