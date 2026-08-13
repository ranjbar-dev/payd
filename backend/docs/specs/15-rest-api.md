# 15. REST API

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §15
**ID prefixes in this file:** `API-*`
**Related:** [`08-order-lifecycle-and-address-pool.md`](08-order-lifecycle-and-address-pool.md) (orders endpoints), [`13-withdrawal-engine.md`](13-withdrawal-engine.md) (withdrawals endpoints), [`12-resource-management.md`](12-resource-management.md) (wallets/resources endpoints), [`04-configuration.md`](04-configuration.md) (`auth.api_keys`)

---

Base path `/api/v1`. All requests require `X-API-Key`. All responses are JSON.

## 15.1 Orders

| Method | Path | Scope | Description |
|---|---|---|---|
| POST | `/orders` | `orders:write` | Create an order, get a deposit address |
| GET | `/orders/{id}` | `orders:read` | Fetch order with payments |
| GET | `/orders` | `orders:read` | List, filtered by `status`, `asset`, date range, paginated |
| POST | `/orders/{id}/cancel` | `orders:write` | Cancel an order (409 if funded, unless `force: true`) |
| GET | `/orders/funded-terminal` | `orders:read` | **New.** Terminal orders holding funds with no recorded resolution |
| POST | `/orders/{id}/resolve` | `orders:write` | **New.** Record how a funded terminal order was dealt with |

**`POST /orders` request:**

```json
{
  "asset": "USDT",
  "amount": "25.00",
  "external_ref": "invoice-2291",
  "consumer": "shop-backend",
  "ttl_seconds": 1800,
  "metadata": {"user_id": 4471}
}
```

**Response `201`:**

```json
{
  "id": "01J8XQZ0A1B2C3D4E5F6G7H8J9",
  "address": "TXYZabc...",
  "asset": "USDT",
  "amount": "25.000000",
  "amount_usd": "25.00",
  "status": "pending",
  "expires_at": 1754501263,
  "created_at": 1754499463
}
```

| ID | Requirement |
|---|---|
| API-001 | `amount` MAY be given either as an asset amount or, with `"amount_usd"`, as a USD value converted at the current price and snapshotted |
| API-002 | Creating an order with an `external_ref` that already exists MUST return the existing order with HTTP 200 **only if `asset`, `expected_raw`, and `consumer` match the request exactly**. On any mismatch the service MUST return HTTP 409 `external_ref_conflict` with the conflicting fields in `details`. v1.1 returned the stored order unconditionally: a caller reusing an invoice number after a fiscal rollover would request 500 USDT, receive a 200 with a valid-looking 25 USDT order body, render a payment page for 500, and release goods when 25 arrived |
| API-003 | Responses MUST use human-readable decimal strings for all amounts |
| API-004 | `consumer` is optional; omitting it MUST route the order's events to `ipn.default_consumer`. Naming an unknown or disabled consumer MUST return HTTP 400 |
| API-005 | An order's `consumer` MUST be immutable after creation — events already enqueued cannot be rerouted |
| API-006 | Order creation MUST return HTTP 503 when the address pool has reached `wallet.pool_max_size` and no address is free (LIF-003) |

## 15.2 Wallets and resources

| Method | Path | Scope | Description |
|---|---|---|---|
| GET | `/wallets` | `wallets:read` | All addresses with balances, states, resource status |
| GET | `/wallets/{address}` | `wallets:read` | Single address detail with payment history |
| GET | `/wallets/with-balance` | `wallets:read` | Only addresses holding **confirmed** funds — the withdrawal source list |
| GET | `/wallets/needs-resources` | `wallets:read` | Addresses flagged `needs_resources` |
| POST | `/wallets/{address}/delegate` | `resources:write` + TOTP | Delegate energy or bandwidth from the resource wallet |
| POST | `/wallets/{address}/disable` | `wallets:write` | Remove from rotation |
| POST | `/wallets/{address}/clear-drift` | `wallets:write` + TOTP | **New.** Clear one asset's `drift_detected` after acknowledging its current `chain_raw` (BAL-002) |

**`GET /wallets/needs-resources` response:**

```json
{
  "addresses": [
    {
      "address": "TXYZabc...",
      "hd_index": 7,
      "balances": [
        {"asset": "USDT", "confirmed": "142.500000", "pending": "0.000000", "usd": "142.50"}
      ],
      "energy": {"available": 0, "limit": 0, "required": 131000, "sufficient": false},
      "bandwidth": {"available": 255, "limit": 600, "required": 345, "sufficient": false},
      "trx_for_bandwidth_burn": "0.000000",
      "can_withdraw": {"TRX": false, "USDT": false},
      "blocked_by": ["bandwidth"],
      "estimated_burn_trx": "27.51",
      "estimated_rent_trx": "3.8",
      "energy_fee_sun": 210,
      "checked_at": 1754499400
    }
  ],
  "total": 1
}
```

| ID | Requirement |
|---|---|
| API-010 | `estimated_burn_trx` and `estimated_rent_trx` MUST both be shown, the former computed from the **live** `getEnergyFee` in `chain_params` (ENR-016) and the latter from the live provider quote, so the dashboard displays the real choice. v1.1's figure was computed against an implied 100 sun/energy and could misreport by 2–4× |
| API-011 | `can_withdraw` MUST be per-asset, since TRX transfers need no energy |
| API-012 | `estimated_rent_trx` MUST be omitted when `energy.enabled` is false or the provider is unreachable, rather than showing a stale or fabricated figure |
| API-013 | `can_withdraw` MUST account for **bandwidth as well as energy** (RES-016), and `blocked_by` MUST name which resource is short. In v1.1 an address with ample energy and no bandwidth reported `can_withdraw: true` and then failed on-chain |
| API-014 | Balances MUST be reported as separate `confirmed` and `pending` figures. A single merged number invites spending unsolidified deposits |

## 15.3 Withdrawals

| Method | Path | Scope | Description |
|---|---|---|---|
| POST | `/withdrawals` | `withdrawals:write` | Request a withdrawal (requires TOTP) |
| GET | `/withdrawals/{id}` | `withdrawals:read` | Status, including `failure_reason` and `resolved_by` |
| GET | `/withdrawals` | `withdrawals:read` | List, paginated, filterable by status |
| GET | `/withdrawals/limits` | `withdrawals:read` | Remaining daily allowance |

**Request:**

```json
{
  "from_address": "TXYZabc...",
  "to_address": "TClientAddr...",
  "asset": "USDT",
  "amount": "100.00"
}
```

Headers: `X-API-Key`, `Idempotency-Key`, `X-TOTP`.

| ID | Requirement |
|---|---|
| API-015 | There MUST be no endpoint that retries, resumes, or re-broadcasts an existing withdrawal (WDR-000c). Moving the funds again requires a new request with a new `Idempotency-Key` |
| API-016 | `GET /withdrawals/limits` MUST compute remaining allowance over the same state set as WDR-006, so the figure it reports matches the figure that will be enforced |
| API-017 | `GET /withdrawals/{id}` MUST expose enough for the dashboard to explain any terminal outcome without a chain lookup: `status`, `failure_reason`, `txid`, `resolved_by`, and `broadcast_response` |

## 15.4 Payments and operations

| Method | Path | Scope | Description |
|---|---|---|---|
| GET | `/payments/unattributed` | `orders:read` | Payments with no matching order |
| GET | `/payments/orphaned` | `orders:read` | **New.** Payments orphaned past `reorg_depth` without re-inclusion (CHN-017) |
| POST | `/payments/{id}/attribute` | `orders:write` | Manually bind to an order |
| GET | `/ipn/dead` | `orders:read` | Dead-lettered notifications |
| POST | `/ipn/{id}/retry` | `orders:write` | Redeliver |
| GET | `/ipn/consumers` | `orders:read` | Configured consumers, enabled state, pending/dead counts |
| GET | `/energy/status` | `wallets:read` | Provider balance, recent purchases, tier success rates |
| GET | `/energy/purchases` | `wallets:read` | Purchase history with costs, paginated |
| GET | `/chain/params` | `wallets:read` | **New.** Live `getEnergyFee` / `getTransactionFee` and when they were read |
| GET | `/prices` | any | Current cached prices |
| GET | `/stats` | any | Order/payment/volume summary for the dashboard |
| GET | `/healthz` | none | Liveness |
| GET | `/readyz` | none | Readiness (see OPS-001) |
| GET | `/metrics` | any | Prometheus |

## 15.5 Tier B support and operator additions

These endpoints were added after the original dashboard surface. Each closes a
specific support or operator-visibility gap without adding a fund-moving retry.

| ID | Method and path | Scope | Requirement and rationale |
|---|---|---|---|
| API-027 | `GET /payments` | `orders:read` | MUST filter by `txid`, `address`, `order_id`, `status`, `direction`, `asset`, and inclusive Unix `from`/`to`, with API-025 pagination, so support can find a customer transaction directly |
| API-028 | `GET /orders` (extended) | `orders:read` | MUST additionally filter by `external_ref`, `consumer`, and `address`, so consumer invoice IDs do not require a write-path idempotency probe |
| API-029 | `POST /orders/{id}/extend` | `orders:write` | MUST accept `ttl_seconds`, reject terminal orders with 409, update `updated_at`, and reject any expiry later than 24 hours after `created_at`, preventing indefinite address retention |
| API-030 | `GET /orders/{id}/events` | `orders:read` | MUST expose the order's outbox consumer, event type, status, attempts, last response/error, creation, and delivery times with API-025 pagination, making missing-webhook investigations self-service |
| API-031 | `POST /withdrawals/{id}/resolve` | `withdrawals:write` + TOTP | MUST accept only `{"outcome":"confirmed"\|"failed","failure_reason":"..."}` for `needs_operator` rows, set `resolved_by=operator`, audit actor and IP, preserve `txid`, and never sign, broadcast, retry, or resume; TOTP is supplied in `X-TOTP` so the JSON body remains exactly the decision record (WDR-018, API-015, API-022) |
| API-032 | `POST /withdrawals/estimate` | `withdrawals:read` | MUST perform zero state writes and require no TOTP while reporting projected energy source, projected TRX cost from live chain parameters, daily-cap blocking, and `blocked_by`, allowing a safe preflight without moving funds. **Asset-balance and TRX-for-resources sufficiency MUST be reported as separate fields (`confirmed_balance_sufficient`, `trx_for_resources_sufficient`) with distinct `blocked_by` entries**, and a single `can_proceed` MUST summarise them. A TRC-20 transfer spends two balances on the source address and the remedies differ — deposit more of the asset versus top the address up with TRX. Collapsing both into one `confirmed_balance` verdict told operators the balance was short while the asset balance sat well above the request, sending them to top up the wrong one |
| API-033 | `GET /auth/whoami` | any authenticated | MUST return only the authenticated key name and sorted scopes, letting clients diagnose their own authorization without weakening API-021 |
| API-034 | `GET /assets` | any authenticated | MUST expose symbol, kind, contract, decimals, minimum deposit, and verified state, preventing clients from hardcoding amount precision |
| API-035 | `POST /ipn/test` | `orders:write` | MUST send one signed `test.ping` directly to the named configured consumer, return status code and latency, reuse the production signature implementation, and never write an outbox row, allowing webhook validation without fake business events |
| API-036 | `POST /ipn/replay` | `orders:write` | MUST filter dead events by consumer and inclusive Unix `from`/`to`, default `dry_run` to true, return only a count, and mutate no more than 200 events per call, making bulk recovery possible within the API rate limit |

## 15.6 Tier C operations, accounting, and compliance additions

| ID | Method and path | Scope | Requirement and rationale |
|---|---|---|---|
| API-037 | `GET /chain/status` | `wallets:read` | MUST return last and solidified heights, lag in blocks and seconds, reorg suspicion, and the last block timestamp, so degraded readiness has a numeric diagnosis |
| API-038 | `GET /chain/quota` | `wallets:read` | MUST return today's requests, configured daily cap, exact percent used, and today plus the six prior UTC days from the persisted RL-006 counter, so quota exhaustion is visible before detection stops |
| API-039 | `GET /workers` | `wallets:read` | MUST expose worker heartbeat age, last error, error count, and restart count from `worker_health` with API-025 pagination, so a wedged worker is distinguishable from a healthy process |
| API-040 | `GET /audit` | `admin:read` | MUST list `audit_log` newest first, filter by actor/action/subject and inclusive Unix `from`/`to`, and use API-025 pagination, making the existing compliance trail reviewable |
| API-041 | `GET /resources/grants` | `wallets:read` | MUST list resource grants with API-025 pagination and filters for withdrawal, status, and resource type, exposing why a withdrawal is waiting for resources |
| API-042 | `GET /resources/wallet` | `wallets:read` | MUST return the configured resource wallet's address, confirmed TRX, available/limit energy and bandwidth, and non-failed self-delegation count and stake by resource, exposing this withdrawal-path dependency |
| API-043 | `GET /config` | `admin:read` | MUST return only allowlisted asset, withdrawal, chain-depth, order-TTL, energy-enabled, and consumer-name fields; it MUST have no field capable of containing endpoint/API/TOTP/key-hash/consumer credentials (CFG-011), preventing environment mistakes without creating a secret endpoint |
| API-044 | `GET /reports/volume` | `orders:read` | MUST require inclusive Unix `from`/`to`, group by UTC day, asset, or consumer, and return order count, paid-or-confirmed count, actual received volume per asset, exact snapshotted USD total, and `unpriced_paid_count`; orders without an immutable price snapshot MUST NOT be assigned a guessed historical USD value |
| API-045 | `GET /reports/fees` | `wallets:read` | MUST require inclusive Unix `from`/`to` over withdrawal `created_at` and return exact TRX totals by energy source, by bandwidth source, and provider-attempt rental spend using the same energy total calculation as operational metrics, enabling resource-strategy comparisons |
| API-046 | `GET /export/orders.csv`, `GET /export/withdrawals.csv` | `orders:read`, `withdrawals:read` | MUST stream `encoding/csv` attachments without materializing all rows, reuse the JSON list filters, default to 10,000 rows, and reject caps outside 1–100,000, supporting bounded accounting exports |

`admin:read` is intentionally limited to API-040 and API-043. No other new
scope is introduced by the Tier C additions.

## 15.7 API conventions

| ID | Requirement |
|---|---|
| API-020 | Auth MUST be by `X-API-Key` matched against Argon2id hashes in config, with per-key scopes enforced per route |
| API-021 | Failed auth MUST return 401 with no detail about which part failed |
| API-022 | TOTP MUST be verified with a ±1 step (30s) window, and each code MUST be single-use — a replay within the window MUST be rejected. **Single-use state MUST be persisted in the `used_totp` table**, not held in memory: an in-memory set reopens the entire replay window on every restart. Validation order is governed by WDR-001a. If `POST /withdrawals` returns 409 after successfully consuming a code, `error.details.totp_consumed` MUST be `true` so the operator knows to wait for a fresh code before correcting the request |
| API-022a | **Every route taking a TOTP MUST read it from the `X-TOTP` header, and MUST reject a code supplied in the request body with 400 `totp_in_body` rather than ignoring it.** One transport for one credential: two accepted forms means an integrator following the wrong example gets a 401 that names the code rather than its placement. Rejecting beats ignoring, because a silently dropped code leaves the caller believing it presented a second factor when it presented none — and on a route that moves funds that belief is the whole control. The rejected request MUST NOT consume the code, so the corrected retry still succeeds |
| API-023 | Rate limiting MUST be applied per API key: 100 req/min default, 10 req/min on withdrawal routes |
| API-024 | All errors MUST use a consistent envelope: `{"error": {"code": "insufficient_balance", "message": "...", "details": {}}}` |
| API-025 | List endpoints MUST use cursor pagination with `limit` (default 50, max 200) and `cursor` |
| API-026 | Every request MUST be logged with method, path, key name, status, and duration — never request bodies, and never headers, since API-022a moves TOTP codes into `X-TOTP` |
