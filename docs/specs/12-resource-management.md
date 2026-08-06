# 12. Resource management (energy and bandwidth)

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §12
**ID prefixes in this file:** `RES-*`, `ENR-*`
**Related:** [`13-withdrawal-engine.md`](13-withdrawal-engine.md) (WDR-009* energy checks, WDR-000 no-retry applies to RES-013/RES-008 broadcasts), [`02-tech-stack-and-dependencies.md`](02-tech-stack-and-dependencies.md) (GAP-002 raw_json workaround), [`04-configuration.md`](04-configuration.md) (`energy`/`resources` blocks)

---

## 12.1 Monitoring

| ID | Requirement |
|---|---|
| RES-001 | The monitor MUST call `POST /wallet/getaccountresource` for addresses with a non-zero `confirmed_raw` balance in any asset, on the tiered cadence in RES-001a |
| RES-001a | **Resource polling MUST be tiered by balance.** Addresses holding more than `resources.poll_threshold_usd` (default 10) are polled every `resources.check_interval`; all others every `resources.slow_check_interval` (6h). The fast tier MUST be bounded by `resources.max_polled_addresses` (default 50), selected by descending balance. Rationale: because sweeping is rejected (NG-003), balances never leave deposit addresses except by explicit withdrawal, and dust and unattributed payments never leave at all — so the set of addresses with a non-zero balance grows monotonically. At a flat 5-minute cadence, 240 such addresses would consume 69,120 calls/day from this line alone and silently push the service past the 100,000/day quota some months after launch, with a complete detection outage as the first symptom |
| RES-002 | The monitor MUST NOT poll addresses with zero balance in every asset — they cannot be withdrawn from, so their resource state is irrelevant |
| RES-003 | `needs_resources` MUST be set true when `(energy_limit - energy_used) < resources.min_energy` OR `(bandwidth_limit - bandwidth_used) < resources.min_bandwidth` |
| RES-004 | Defaults MUST be `min_energy: 131000` and `min_bandwidth: 345`. A TRC-20 transfer needs roughly 65,000 energy when the recipient already holds the token and up to 131,000 when they do not. Since withdrawal destinations include client addresses that may hold no balance, the threshold MUST assume the worst case. Both values MUST remain configurable |
| RES-004a | The service MAY reduce the required energy to 65,000 for a specific transfer when it has verified via `/wallet/triggerconstantcontract` that the destination holds a non-zero balance of the token being sent. This is an optimisation, not a requirement |
| RES-005 | For native TRX withdrawals, energy is irrelevant; only bandwidth matters. The API MUST report resource sufficiency per asset kind, not as a single flag |

## 12.2 Energy sourcing strategy

Energy is sourced through a three-tier fallback chain, attempted in order. Each tier is optional and independently disableable.

| Tier | Source | Cost per TRC-20 transfer | Trade-off |
|---|---|---|---|
| 1 | Rented from a third-party market | ~3–5 TRX | Third-party dependency, prepaid balance |
| 2 | Self-delegated from a staked resource wallet | Free ongoing | Locks capital, 14-day unstake period |
| 3 | Burned TRX (network fallback) | `energy_required × getEnergyFee` sun — **computed live, never assumed** | No integration, unpredictable, most expensive |

> **Tier 3 cost is a formula, not a figure.** v1.1's table stated ~6.5 TRX and ~13 TRX, which is arithmetic that only works at **100 sun per energy unit**. TRON's energy unit price is a **governance-controlled chain parameter** (`getEnergyFee`) that has been raised by proposal more than once. At 210 sun the same transfers cost ~13.65 and ~27.5 TRX; at 420 sun, ~27.3 and ~55 TRX — at which point v1.1's `max_burn_trx: "20"` would refuse every burn, disabling the fallback of last resort in exactly the scenario R-009 claims it covers. Verify the live value at `POST /wallet/getchainparameters`; the structural rule is ENR-016.

| ID | Requirement |
|---|---|
| ENR-001 | Energy and bandwidth sourcing MUST be defined by an `energy.Provider` interface with methods `Quote(receiver, resourceType, amount, duration) (Quote, error)`, `Purchase(Quote) (Order, error)`, `Status(orderID) (Status, error)`, and `Balance() (trx string, err error)`. **`resourceType` is required** — v1.1's signature had no such parameter and therefore could not express a bandwidth rental at all |
| ENR-002 | The concrete provider MUST be selected by `energy.provider` in config, so adding a second market means adding an implementation, not editing callers |
| ENR-003 | Tier 1 MUST be attempted first when `energy.enabled` is true |
| ENR-004 | A quote whose price exceeds `energy.max_price_trx` MUST be rejected without purchase, and the chain MUST fall through to the next tier |
| ENR-005 | After purchase, the engine MUST poll `getaccountresource` every `energy.poll_interval` until the delegated energy is visible or `energy.poll_timeout` elapses. This polling MUST be counted in the rate-limit budget spec at up to `poll_timeout / poll_interval` calls per withdrawal |
| ENR-006 | On poll timeout, the purchase MUST be marked `failed`, an `energy.purchase_failed` event MUST be emitted, and the chain MUST fall through |
| ENR-007 | Tier 2 MUST use the raw_json delegation path described in §12.3, and MUST be skipped when the resource wallet has no delegatable energy |
| ENR-008 | Tier 3 MUST be attempted only when `energy.fallback_to_burn` is true, and MUST be refused when the **live-computed** estimated burn exceeds `energy.max_burn_trx` — in which case the withdrawal fails with a clear reason rather than burning without bound |
| ENR-009 | Every purchase MUST be recorded in `energy_purchases` with the quoted price, the actual price, and the delegation txid, so the true cost of each withdrawal is auditable |
| ENR-010 | The provider's prepaid balance MUST be checked every 15 minutes and recorded in `energy_provider_state` |
| ENR-011 | A balance below `energy.balance_warn_trx` MUST emit an `energy.balance_low` global IPN event and set a metric |
| ENR-012 | After 5 consecutive provider failures, tier 1 MUST be skipped entirely for 10 minutes rather than attempted per transaction |
| ENR-013 | The provider API MUST NOT be given any private key, mnemonic, or signing authority. It receives only a receiver address and a resource amount |
| ENR-014 | Provider calls MUST NOT count against the TronGrid quota — they are a separate host with a separate budget |
| ENR-015 | Rentals MUST use the shortest duration that covers the transaction (`energy.rent_duration`, default 1h). Longer rentals cost more for no benefit in this workload |
| ENR-016 | **The service MUST read `getEnergyFee` and `getTransactionFee` from `POST /wallet/getchainparameters` at startup and every 6 hours** (W-011), storing them in `chain_params`. All burn-cost estimates — `estimated_burn_trx`, ENR-008's ceiling check, and the §12.2 tier table — MUST be computed from the live values, never from a compiled-in constant |
| ENR-017 | `energy.max_burn_trx` MUST be validated at startup against `131000 × getEnergyFee`. If the configured ceiling would refuse a worst-case transfer, the service MUST log a warning naming both figures and expose it on `/readyz` as a degraded condition. **A silently unreachable tier 3 is not an acceptable steady state** — it converts a provider outage from a cost problem into a total withdrawal outage |

**Rejected alternatives.** Address-based purchase (send TRX to a provider address, receive energy) was rejected because it yields no order ID to reconcile against and no failure signal — arrival can only be detected by polling one's own resource state. Aggregator routing across many providers was rejected as premature: it adds a dependency on top of the dependencies it manages, and a single provider plus a burn fallback already caps the downside.

## 12.3 Self-delegation (tier 2)

`hd-wallet` has no `DelegateResourceContract` (GAP-002). The workaround:

| ID | Requirement |
|---|---|
| RES-010 | To delegate a resource, the service MUST call `POST /wallet/delegateresource` on TronGrid with `owner_address` (the resource wallet), `receiver_address`, `balance`, `resource` (`"ENERGY"` or `"BANDWIDTH"` — **not hardcoded**), and `lock: false` |
| RES-011 | TronGrid returns an unsigned transaction; the service MUST sign its `raw_data_hex` using `hd-wallet`'s **raw_json mode**, which validates the provided txID against the raw data before signing |
| RES-012 | The txID guard MUST NOT be bypassed — it is the only protection against a malicious or compromised endpoint returning a different transaction than requested |
| RES-013 | The signed transaction MUST be broadcast via `POST /wallet/broadcasttransaction` and tracked in `resource_grants` with the appropriate `source` and `resource_type`. **This broadcast is subject to the withdrawal-engine spec's §13.0 no-retry rule** — it is a fund-moving transaction and MUST be attempted at most once, then resolved on-chain |
| RES-014 | The resource wallet (HD index `resources.resource_wallet_index`, which CFG-013 forces outside the deposit pool) MUST hold staked TRX; the service MUST NOT attempt to stake or unstake automatically |
| RES-015 | Delegation failure MUST NOT abort the withdrawal; the chain falls through to tier 3, and the burn amount MUST be logged |

## 12.4 Bandwidth sourcing

v1.1 monitored bandwidth and never sourced it. `min_bandwidth: 345` was the correct figure and nothing acted on it: the whole three-tier chain sourced energy only, the provider interface could not express a bandwidth rental, the delegation call hardcoded `ENERGY`, and TRX withdrawals skipped the resource check entirely on the grounds that they consume bandwidth rather than energy — which is precisely why they need a bandwidth check.

The structural problem underneath is that **a deposit address that has only ever received USDT holds zero TRX.** Bandwidth burn is paid in TRX from the account's own balance, rented or delegated *energy* does not cover it, sweeping is rejected (NG-003), and nothing in v1.1 ever sent TRX *to* a deposit address. The result is that each pooled address supports exactly one TRC-20 withdrawal per UTC day and then becomes unwithdrawable until midnight — with funds concentrating precisely in the addresses that hit the wall, and no remedy inside the design.

| ID | Requirement |
|---|---|
| RES-006 | Before signing **any** withdrawal — TRX or TRC-20 — the engine MUST verify that `(bandwidth_limit - bandwidth_used) >= resources.min_bandwidth` **OR** the source address holds at least `resources.min_bandwidth × getTransactionFee` sun of TRX. If neither holds, the withdrawal MUST transition to `awaiting_resources` and enter the RES-007 sourcing path |
| RES-007 | Bandwidth MUST be sourced by one of: **(a)** `POST /wallet/delegateresource` with `resource: "BANDWIDTH"` from the resource wallet via the §12.3 raw_json path, or **(b)** a TRX top-up transfer of `resources.bandwidth_topup_trx` (default 2) from the resource wallet to the source address. Which is used MUST be configurable via `resources.bandwidth_strategy`. The chosen source MUST be recorded in `withdrawals.bandwidth_source` |
| RES-008 | A bandwidth top-up transfer is itself a fund-moving broadcast and MUST follow the withdrawal-engine spec's §13.0 — attempted once, resolved on-chain, never retried |
| RES-009 | If bandwidth cannot be sourced, the withdrawal MUST transition to `failed` with `failure_reason = 'bandwidth_unavailable'` and MUST NOT be retried. The operator is notified via `withdrawal.failed` |
| RES-016 | `GET /wallets/needs-resources` MUST include `bandwidth.sufficient` in `can_withdraw`, which in v1.1 reflected energy only |

## 12.5 Chain parameters (W-011)

| ID | Requirement |
|---|---|
| RES-020 | At startup and every 6 hours, the service MUST fetch `POST /wallet/getchainparameters` and upsert `getEnergyFee` and `getTransactionFee` into `chain_params` |
| RES-021 | A failed fetch MUST NOT overwrite the last known good values; the service MUST continue on the cached values and increment an error counter |
| RES-022 | If `chain_params` has never been populated, the withdrawal engine MUST refuse to compute a burn estimate and MUST hold withdrawals in `awaiting_resources` rather than assume a price |
| RES-023 | A change in `getEnergyFee` between polls MUST be logged at warn level with both values, and MUST re-run ENR-017's ceiling validation |
