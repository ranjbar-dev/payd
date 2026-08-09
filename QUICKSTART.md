# QUICKSTART — running and testing `payd` on your own machine

This guide takes you from a fresh clone to a running daemon on the **Tron Nile
testnet**, then walks a test plan that checks the API responses and the SQLite
rows behind them.

Everything below was executed end-to-end on Windows 11 against
`https://nile.trongrid.io` before this document was written. Where a real
result is quoted, it is a real result.

---

## 1. What this project is

`payd` is a **self-hosted, single-tenant TRON payment processor**. One binary,
one SQLite file, one HD wallet, one operator. It:

1. Issues a **deposit address** to your application when you create an order.
2. **Watches the Nile/mainnet chain** every 3 seconds for TRX and TRC-20
   transfers into any address it owns.
3. **Attributes** each transfer to an order (handling partial and overpayment).
4. **Notifies your backend** with signed IPN webhooks as the order changes state.
5. **Withdraws** funds out on operator command, with TOTP and a no-retry
   fund-safety policy.

It is *not* a multi-merchant gateway and has *no* frontend — it is an API your
own services call. Design docs live in `docs/specs/`, routed by
`docs/index.md`.

### The ten workers inside the one process

| Worker | Package | Job |
|---|---|---|
| Chain follower | `internal/follower` | Poll blocks, detect gaps and reorgs |
| Decoder | `internal/decode` | Decode TRX transfers and TRC-20 `Transfer` logs |
| Matcher | `internal/matcher` | Bind a payment to an order |
| Confirmation tracker | `internal/confirm` | Promote `seen` → `confirmed` at solidification |
| Lifecycle | `internal/lifecycle` | Expire orders, return addresses, top up the pool |
| IPN dispatcher | `internal/ipn` | Deliver signed webhooks in order, per consumer |
| Price poller | `internal/price` | Binance TRX/USDT quote every 60s |
| Wallet monitor + reconciler | `internal/wallet` | Balances, resources, chain-vs-DB drift |
| Withdrawal engine | `internal/withdraw` | Sign, broadcast once, reconcile |
| API | `internal/api` | The HTTP boundary you talk to |

---

## 2. What API keys you actually need

Short answer for local Nile testing: **none are mandatory.**

| Service | Needed? | How to get it |
|---|---|---|
| **TronGrid** | Optional on Nile, recommended on mainnet | Sign up at <https://www.trongrid.io/dashboard>, create a key, paste into `tron.endpoints[].api_key`. Sent as the `TRON-PRO-API-KEY` header. Without a key, Nile still answers but throttles harder. |
| **Binance price feed** | No key. **But see the note below.** | Public endpoint, no registration. |
| **TronZap (energy rental)** | No — keep `energy.enabled: false` | Only relevant on mainnet, to buy energy instead of burning TRX. Register at the provider and fill `energy.api_key` / `energy.api_secret`. |
| **Nile test TRX/USDT** | Yes, to test payments | Free faucet, no key: <https://nileex.io/join/getJoinPage> |

> **Binance note (important, and it bit this exact machine).** The default
> `price.url` is `https://api.binance.com/api/v3/ticker/price`. From some
> networks, `curl` reaches it fine but Go's HTTP client times out — the
> connection is accepted and then never answers headers. The symptom is
> `/readyz` stuck on `{"reasons":["price_stale"],"status":"degraded"}` and
> repeating `refresh prices ... context deadline exceeded` in the log.
>
> Fix: use Binance's public market-data mirror, which serves the identical
> JSON shape:
>
> ```yaml
> price:
>   url: "https://data-api.binance.vision/api/v3/ticker/price"
> ```
>
> After that change `/readyz` returned `{"status":"ready"}` here.

---

## 3. Prerequisites

* **Go 1.24+** — `go version`
* **`sqlite3` CLI** — to inspect the database. Windows:
  `winget install SQLite.SQLite`. (Or use DB Browser for SQLite / any GUI.)
* **`curl`** — ships with Windows 10+.
* **TronLink** browser wallet (or any BIP-39 wallet) — used to generate a
  throwaway mnemonic and to send test payments from the faucet.

---

## 4. First-time setup

### 4.1 Create the deployment key (`KEY-002/003`)

`internal/seed/seed.key` is embedded into the binary at compile time with
`go:embed` and encrypts your mnemonic. It is gitignored and **must be exactly
32 bytes**.

The repo already has one. To generate a fresh one (do this for any deployment
you care about — and note it invalidates any existing `seed.age`):

```bash
# PowerShell
$b = New-Object byte[] 32
[System.Security.Cryptography.RandomNumberGenerator]::Fill($b)
[System.IO.File]::WriteAllBytes("$PWD\internal\seed\seed.key", $b)
```

```bash
# Git Bash
head -c 32 /dev/urandom > internal/seed/seed.key
```

> **Back this file up separately from `seed.age`.** Losing `seed.key` means
> `seed.age` cannot be decrypted. Losing both means the wallet is gone.

### 4.2 Build

```bash
go build -o payd.exe ./cmd/payd
go build -o seedtool.exe ./cmd/seedtool
go build -o paydev.exe ./tools/paydev
```

`tools/paydev` is a **local development helper only** — it is not part of the
daemon and never opens the database, the seed, or the chain. It exists because
you cannot hand-write an Argon2id hash or a TOTP code.

### 4.3 Generate a throwaway testnet mnemonic

Open TronLink → create a new wallet → copy the 12 words. **Use a brand-new
wallet you will never fund with real money.**

> **Do not use the public `abandon abandon … about` test vector.** This guide's
> smoke run did, and the reconciler immediately back-filled **39 historical
> USDT payments from strangers** into the database, because that address
> (`TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH`) is shared by every tutorial on the
> internet. Your test data will be unreadable.

Encrypt it:

```bash
# Git Bash — no trailing newline problems, and the words never hit a file
printf 'your twelve words go right here ...' | ./seedtool.exe --out seed.age --account 0
```

```powershell
# PowerShell
"your twelve words go right here ..." | .\seedtool.exe --out seed.age --account 0
```

It prints the account **xpub** — that is your watch-only backup key
(`KEY-008`). `seedtool` refuses to overwrite an existing `seed.age`
(`KEY-004`), so delete the file first if you are regenerating.

### 4.4 Generate the API key and TOTP secret

```bash
./paydev.exe apikey
# X-API-Key:  DBk0trtWPYmvBy-doklNBbig6Bx1FCvzhUmOtuQopm8
# key_hash:   argon2id$v=19$m=65536,t=3,p=2$Nv47nHqEnRP...$An5QGeVYt5a...

./paydev.exe totp-secret
# WPFTISLL6W6PT3ULFGOEVSKNU3DZEZG7
```

Keep the **`X-API-Key` value** — it is shown once and only its hash goes in the
config. Put the base32 TOTP secret into Google Authenticator / Aegis too if you
want codes on your phone; `paydev totp <secret>` prints the same code.

### 4.5 Write `payd.nile.yaml`

Copy `config.example.yaml` and change the marked lines. The full working Nile
config:

```yaml
server:
  listen: "127.0.0.1:8080"
  read_timeout: 15s
  write_timeout: 30s
database:
  path: "payd.nile.db"
wallet:
  seed_file: "seed.age"
  account: 0
  pool_initial_size: 5          # small pool = readable test data
  pool_min_free: 2
  pool_max_size: 50
  cooldown: 2h
tron:
  endpoints:
    - url: "https://nile.trongrid.io"   # CHANGED: Nile, not mainnet
      api_key: ""                       # optional on Nile
      weight: 100
  solidity_url: "https://nile.trongrid.io"
  poll_interval: 3s               # must be exactly 3s (CHN-001)
  confirmations_required: 19
  reorg_depth: 64
  request_timeout: 10s            # must be exactly 10s (CHN-024)
  daily_request_quota: 100000
assets:
  - symbol: "TRX"
    kind: "native"
    decimals: 6
    verified: true
  - symbol: "USDT"
    kind: "trc20"
    contract: "TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf"   # CHANGED: Nile USDT
    decimals: 6
    min_deposit: "0.5"
    verified: true
orders:
  default_ttl: 30m
  underpayment_policy: "partial"
  overpayment_policy: "credit_and_log"
ipn:
  consumers:
    - name: "local"
      url: "http://127.0.0.1:9090/ipn"
      secret: "nile-test-secret-do-not-reuse"
      receives_global: true
      enabled: true
  default_consumer: "local"
  timeout: 10s
  max_attempts: 2
  workers: 1
  backoff: [10s, 30s]
energy:
  enabled: false
  provider: ""
  api_url: ""
  api_key: ""
  api_secret: ""
  timeout: 15s
  rent_amount: 0
  rent_duration: 0s
  max_price_trx: ""
  balance_warn_trx: ""
  poll_interval: 0s
  poll_timeout: 0s
  fallback_to_burn: true
  max_burn_trx: "60"
price:
  provider: "binance"
  url: "https://data-api.binance.vision/api/v3/ticker/price"   # CHANGED: see §2
  interval: 60s                   # must be exactly 60s (PRC-001)
  pairs: ["TRXUSDT"]
  stale_after: 5m
resources:
  min_energy: 131000
  min_bandwidth: 345
  check_interval: 5m
  slow_check_interval: 6h
  poll_threshold_usd: "10"
  max_polled_addresses: 50
  resource_wallet_index: 1000
  bandwidth_strategy: "topup"
  bandwidth_topup_trx: "2"
withdrawal:
  enabled: true
  daily_limit_usd: "5000"
  fee_limit_trx: 100
  expiration: 60s
  require_totp: true
auth:
  api_keys:
    - name: "local"
      key_hash: "PASTE key_hash FROM paydev apikey"
      scopes: ["orders:read", "orders:write", "wallets:read", "wallets:write",
               "withdrawals:read", "withdrawals:write", "resources:write"]
  totp_secret: "PASTE FROM paydev totp-secret"
log:
  level: "info"
  format: "console"
```

Config gotchas that will refuse to start:

* Unknown YAML keys are **rejected**, not ignored — no typos allowed.
* `poll_interval` must be exactly `3s`, `request_timeout` exactly `10s`,
  `price.interval` exactly `60s`. These are spec constants, not defaults.
* Every asset needs `verified: true` spelled out (`CFG-014`).
* Two endpoints may not share a hostname (`CFG-015`).
* On Linux/macOS the config file must be mode `0600` (`CFG-004`). Windows
  skips this check.
* `resource_wallet_index` must be ≥ 1000 (`CFG-013`).

### 4.6 Run

```bash
./payd.exe --config payd.nile.yaml
```

Expected startup lines — the two `WARN`s about assets are intentional
(`CFG-014` wants configured tokens shouted about):

```
level=WARN msg="verified asset configured" symbol=TRX contract="" decimals=6
level=WARN msg="verified asset configured" symbol=USDT contract=TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf decimals=6
level=WARN msg="solidity endpoint shares a TronGrid host; independent host preferred" host=nile.trongrid.io
level=INFO msg="payd started" pool_addresses=5 resource_wallet_index=1000
```

One `ERROR msg="confirmation tracker tick failed" error="load confirmation
cursor: sql: no rows in result set"` on the very first tick is normal — the
follower has not written the crawler cursor yet.

---

## 5. Test plan

Set these once per shell:

```bash
KEY="<the X-API-Key from paydev apikey>"
BASE="http://127.0.0.1:8080"
DB="payd.nile.db"
```

Read the database **read-only** while the daemon is running, so you never
contend with its writer:

```bash
sqlite3 -header -column "file:$DB?mode=ro" "SELECT ...;"
```

---

### T1 — Wallet derivation and the address pool

**API**

```bash
curl -s $BASE/healthz
```
→ `{"status":"ok"}`

**Database**

```bash
sqlite3 -header -column "file:$DB?mode=ro" \
  "SELECT hd_index, address, state FROM addresses ORDER BY hd_index;"
```

**Pass if:** exactly `pool_initial_size` rows with `hd_index` 0..N-1 in state
`free`, plus one row at `hd_index = 1000` in state `disabled` — that is the
resource wallet, deliberately kept out of rotation.

```
hd_index  address                             state
0         T....                               free
1         T....                               free
...
1000      T....                               disabled
```

Cross-check derivation against TronLink: the addresses must match accounts
1..N of the same mnemonic (path `m/44'/195'/0'/0/n`).

---

### T2 — Auth, scopes, rate limits

```bash
curl -s -o /dev/null -w "%{http_code}\n" $BASE/api/v1/orders
# 401  — no key, and the body says nothing about why (API-021)

curl -s -H "X-API-Key: wrong" -o /dev/null -w "%{http_code}\n" $BASE/api/v1/orders
# 401

curl -s -H "X-API-Key: $KEY" $BASE/api/v1/orders
# {"next_cursor":"","orders":[]}
```

To test scope enforcement, add a second key to the config with only
`["orders:read"]` and confirm `POST /api/v1/orders` returns **403** for it.

Rate limits are per key: 100 req/min general, 10 req/min on `/withdrawals`
(`API-023`). Fire 105 quick reads and expect **429** at the tail.

---

### Testing routes in Swagger UI

Start the daemon and open <http://127.0.0.1:8080/docs>. Click **Authorize** and
paste the `X-API-Key` value produced by `./paydev.exe apikey` in §4.4. Use the
key itself—not the `key_hash` stored in the config.

For an end-to-end check, expand `POST /api/v1/orders`, click **Try it out**,
paste the T5 request body below, and click **Execute**. Confirm the response is
**201** and that `amount` is the JSON string `"1.5"`, not the number `1.5`.

`POST /api/v1/withdrawals` additionally requires an `Idempotency-Key` header
and a fresh code from `./paydev.exe totp <secret>`. Each TOTP is single-use: a
second Execute with the same code returns 401 `invalid_totp` by design
(`API-022`), so generate a new code before every attempt.

Swagger UI loads its assets from a CDN and therefore needs internet access.
The OpenAPI document itself works fully offline and can be saved and imported
into Postman, Insomnia, or Bruno:

```bash
curl -s http://127.0.0.1:8080/openapi.yaml -o payd-openapi.yaml
```

**Warning:** `/docs` and `/openapi.yaml` are unauthenticated; keep
`server.listen` on `127.0.0.1`, or put payd behind a proxy that blocks these
paths, consistent with §6.4.

---

### T3 — The chain follower is actually following

```bash
sqlite3 -header -column "file:$DB?mode=ro" "SELECT * FROM crawler_state;"
sqlite3 "file:$DB?mode=ro" "SELECT count(*), min(height), max(height) FROM blocks;"
curl -s $BASE/metrics | grep payd_chain_lag_blocks
```

**Pass if:** `last_height` climbs about one per 3 seconds, `solidified_height`
trails it by roughly 19–20, and `payd_chain_lag_blocks` is 0 or a small
number. Verified run:

```
id  last_height  solidified_height  reorg_suspected_from  solidified_updated_at
1   69921841     69921816                                 1786277771
28|69921814|69921841
payd_chain_lag_blocks 0
```

On a fresh database the follower starts at the **current tip**, not at genesis
— so it will not see payments made before you started it (the reconciler
back-fills those separately; see T9).

---

### T4 — Readiness (`/readyz`) and degradation

```bash
curl -s $BASE/readyz
```

Healthy: `{"status":"ready"}`.

Degraded returns **503** and names the reason, e.g.
`{"reasons":["price_stale"],"status":"degraded"}`. The full reason set is
`database_unwritable`, `database_unavailable`, `chain_lag`,
`solidified_stale`, `price_stale`, `reorg_depth_exceeded`,
`clock_unavailable`, `clock_skew`, `trongrid_quota_projection`,
`energy_burn_ceiling`.

Two easy ways to see a real 503:

* Point `price.url` at a dead host and restart → `price_stale` within 5m.
* Set your OS clock 40s off → `clock_skew` (`OPS-005`); the daemon compares
  local time against the latest block header timestamp.

---

### T5 — Order creation and response correctness

```bash
curl -s -X POST $BASE/api/v1/orders \
  -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"asset":"USDT","amount":"1.50","external_ref":"smoke-1","ttl_seconds":1800,"metadata":{"user_id":1}}'
```

Verified response — **201**:

```json
{"address":"TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH","amount":"1.5","amount_usd":"1.50",
 "asset":"USDT","consumer":"local","created_at":1786277759,"expires_at":1786279559,
 "external_ref":"smoke-1","id":"01KZK772CKH6G9KTEM09TB7AFS","metadata":{"user_id":1},
 "overpaid":"0","price_usd":"1.00","received":"0","status":"pending","updated_at":1786277759}
```

Check each of these — they are the requirements, not decoration:

* `amount`, `received`, `overpaid` are **decimal strings**, never numbers
  (`API-003`, `DB-001`). If you ever see a JSON number here, that is a bug.
* `id` is a ULID.
* `consumer` filled in from `ipn.default_consumer` because the request omitted
  it (`API-004`).
* `price_usd` is `1.00` for USDT — the price service short-circuits stablecoins
  when no `USDTUSDT` pair is configured, so USDT orders work even with the
  price feed down.

**Database**

```bash
sqlite3 -header -column "file:$DB?mode=ro" \
  "SELECT id, external_ref, address, asset, expected_raw, received_raw, status, consumer FROM orders;"
```

**Pass if:** `expected_raw` is **base units** — `1500000` for 1.5 USDT at 6
decimals. The API formats; the database stores raw. And:

```bash
sqlite3 "file:$DB?mode=ro" "SELECT hd_index, state, assigned_order_id FROM addresses WHERE state='assigned';"
```
→ the order's address flipped `free` → `assigned`.

#### T5b — USD-denominated orders

```bash
curl -s -X POST $BASE/api/v1/orders -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"asset":"TRX","amount_usd":"3.30","ttl_seconds":600}'
```

Verified: `"amount":"10"`, `"amount_usd":"3.30"`, `"price_usd":"0.33000000"` —
the TRX amount was computed from the live Binance quote and **snapshotted**
onto the order (`API-001`). Confirm the snapshot is stored:

```bash
sqlite3 -header -column "file:$DB?mode=ro" "SELECT id, price_usd, price_at FROM orders WHERE price_usd IS NOT NULL;"
sqlite3 -header -column "file:$DB?mode=ro" "SELECT * FROM prices;"
```

Send exactly one of `amount` or `amount_usd`; sending both or neither is a
**400 `invalid_order`**.

#### T5c — `external_ref` conflict (API-002, a real past bug)

```bash
# identical request → 200, returns the SAME order, no new address burned
curl -s -o /dev/null -w "%{http_code}\n" -X POST $BASE/api/v1/orders \
  -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"asset":"USDT","amount":"1.50","external_ref":"smoke-1","ttl_seconds":1800,"metadata":{"user_id":1}}'
# 200

# same ref, DIFFERENT amount → 409, and it names the field
curl -s -X POST $BASE/api/v1/orders \
  -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"asset":"USDT","amount":"9.99","external_ref":"smoke-1","ttl_seconds":1800}'
```

Verified:

```json
{"error":{"code":"external_ref_conflict","details":{"fields":["expected_raw"]},
          "message":"external_ref belongs to a different order request"}}
```

This is the exact defect the spec calls out: an unconditional 200 here would
let a caller request 500 USDT, render a page for 500, and release goods when 25
arrived. **If this returns 200, stop and file a bug.**

---

### T6 — IPN webhooks end to end

Run the receiver in a second terminal (secret must match `ipn.consumers[].secret`):

```bash
./paydev.exe ipnsink "nile-test-secret-do-not-reuse" 127.0.0.1:9090
```

Trigger an event without needing any funds — create an order that expires in
one second, and let the Lifecycle Worker fire:

```bash
curl -s -X POST $BASE/api/v1/orders -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"asset":"USDT","amount":"2.00","external_ref":"smoke-expire","ttl_seconds":1}'
sleep 25
```

Verified sink output:

```
signature=true consumer="local" event=01KZK79HD698M0VRZ9MTZ281PZ type=order.expired current_status=expired
{"consumer":"local","current_status":"expired","event_id":"01KZK79HD698M0VRZ9MTZ281PZ",
 "event_type":"order.expired","from_addresses":null,"metadata":{},"occurred_at":1786277840,
 "order_id":"01KZK79CSF0CYHD4155ZWSYR8R","received":"0","refundable":false,
 "snapshot_age_seconds":0,"status":"expired"}
```

**Pass if:**

* `signature=true`. The signature is
  `hex(HMAC-SHA256(consumer_secret, X-Timestamp + "." + raw_body))` — computed
  over the **raw bytes**, so never re-serialize the body before verifying.
* Headers present: `X-Event-Id`, `X-Timestamp`, `X-Consumer`, `X-Signature`.
* `current_status` and `snapshot_age_seconds` exist **in addition to** the
  frozen snapshot (`IPN-021a`). The snapshot describes the transition; the two
  extra fields describe now.
* `refundable:false` and `received:"0"` because the order expired unfunded. An
  expiry **with** funds gives `refundable:true`, a non-zero `received`, and the
  contributing `from_addresses`, so a refund needs no chain lookup
  (`ORD-005a`).

**Database**

```bash
sqlite3 -header -column "file:$DB?mode=ro" \
  "SELECT id, event_type, consumer, status, attempts, last_status_code FROM ipn_outbox ORDER BY created_at;"
```
→ `status = 'delivered'`, `attempts = 1`, `last_status_code = 200`.

**Failure path:** stop the sink, expire another order, and watch `attempts`
climb with `next_attempt_at` following the `ipn.backoff` list. After
`max_attempts` the row goes `dead`. Restart the sink and it will *not*
self-heal — redelivery of dead letters is a documented endpoint that is **not
implemented yet** (see §7).

---

### T7 — A real payment (this is the main event)

1. Create an order for a **small, unusual** amount so it is unmistakable:

   ```bash
   curl -s -X POST $BASE/api/v1/orders -H "X-API-Key: $KEY" \
     -H "Content-Type: application/json" \
     -d '{"asset":"USDT","amount":"1.234567","external_ref":"pay-1","ttl_seconds":3600}'
   ```

2. Copy the `address` from the response.

3. Get test funds at <https://nileex.io/join/getJoinPage> (2000 TRX + 1000 USDT
   per wallet per day, reCAPTCHA only). Send them to **your TronLink wallet**
   first, then forward the exact order amount from TronLink to the order
   address. Make sure TronLink is switched to the **Nile** network.

4. Watch, in order:

   ```bash
   # within ~5s of the block landing
   curl -s -H "X-API-Key: $KEY" $BASE/api/v1/orders/<ORDER_ID>
   ```

**Expected state machine:**

| Stage | Order `status` | Payment `status` | IPN event |
|---|---|---|---|
| Block containing the transfer is ingested | `paid` | `seen` | `order.payment_seen`, then `order.paid` |
| ~19 blocks later, block solidifies | `confirmed` | `confirmed` | `order.confirmed` |

**Database checks**

```bash
sqlite3 -header -column "file:$DB?mode=ro" \
  "SELECT id, txid, direction, asset, amount_raw, status, block_height, order_id FROM payments ORDER BY id DESC LIMIT 5;"
sqlite3 -header -column "file:$DB?mode=ro" \
  "SELECT a.address, b.asset, b.confirmed_raw, b.pending_raw, b.drift_detected
     FROM balances b JOIN addresses a ON a.id = b.address_id;"
```

**Pass if:**

* `payments.amount_raw = '1234567'` — raw base units, matched to `order_id`.
* `direction = 'in'`, `(txid, log_index)` unique.
* While `seen`: the amount sits in `pending_raw` and `confirmed_raw` is `0`.
  After solidification it moves to `confirmed_raw`. They are **separate
  columns and separate API fields on purpose** (`API-014`) — an address is not
  a valid withdrawal source on unsolidified money.
* `orders.received_raw` equals the sum of attributed payments.

**Variants worth running:**

| Variant | Send | Expect |
|---|---|---|
| Underpayment | 0.5 of the expected amount | `status: "partial"`, IPN `order.partial`, order stays open until TTL |
| Overpayment | 2× the expected amount | `status: "paid"`, `overpaid` non-zero, logged not rejected (`credit_and_log`) |
| Two partials | half, then half | `partial` → `paid`, two payment rows, one order |
| Dust | below `min_deposit` (0.5 USDT) | payment recorded with `is_dust = 1`, does not satisfy the order |
| Wrong asset | TRX to a USDT order's address | recorded, but `order_id IS NULL` → shows up in `GET /api/v1/payments/unattributed` |
| Expiry with funds | pay partially, wait out the TTL | `status: "expired_funded"`, appears in `GET /api/v1/orders/funded-terminal`, IPN carries `refundable:true` |

---

### T8 — Confirmation tracking and restart safety

```bash
sqlite3 -header -column "file:$DB?mode=ro" "SELECT status, count(*) FROM payments GROUP BY status;"
```

Stop `payd` with Ctrl-C **while a payment is `seen`**, wait a minute, restart.

**Pass if:** on restart the follower resumes from `crawler_state.last_height`
(no re-scan from tip, no gap), the `seen` payment still promotes to
`confirmed`, and **no duplicate payment row** appears — the `(txid, log_index)`
unique index is what guarantees that (`G-007`).

---

### T9 — The reconciler safety net (DET-010)

The balance reconciler independently queries
`/v1/accounts/{address}/transactions` for owned addresses and ingests anything
the follower missed — including transfers that happened before the daemon ever
ran.

In the verified run this fired hard: 39 historical USDT payments to the
shared-test-vector address were pulled in and correctly filed as
`unattributed` (no matching order), producing a `pending_raw` of
`11254.654444` USDT and `confirmed_raw` of `0`.

```bash
curl -s -H "X-API-Key: $KEY" $BASE/api/v1/payments/unattributed
sqlite3 -header -column "file:$DB?mode=ro" \
  "SELECT last_verified_at, drift_detected, chain_raw FROM balances;"
```

To attribute one by hand:

```bash
curl -s -X POST -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"order_id":"<ORDER_ID>"}' $BASE/api/v1/payments/<PAYMENT_ID>/attribute
```

If the reconciler finds the chain and the database disagreeing, it sets
`drift_detected = 1`, emits `balance.drift_detected`, and **blocks withdrawals
from that address** with HTTP 409 `balance_drift` until you review and clear it
via `POST /api/v1/wallets/{address}/clear-drift` (`BAL-002`).

---

### T10 — Withdrawals

Withdrawals are the highest-stakes path. Read `docs/specs/13-withdrawal-engine.md`
§13.0 before trusting anything here.

**Preconditions:** the source address must have a `balances` row with real
`confirmed_raw` — that is, a payment that has solidified. TRC-20 transfers also
need **energy and bandwidth** on the source address; on Nile you get those by
sending the address some TRX from the faucet.

```bash
TOTP=$(./paydev.exe totp WPFTISLL6W6PT3ULFGOEVSKNU3DZEZG7)

curl -s -X POST $BASE/api/v1/withdrawals \
  -H "X-API-Key: $KEY" -H "Idempotency-Key: w-001" -H "Content-Type: application/json" \
  -d "{\"from_address\":\"<SOURCE>\",\"to_address\":\"<DEST>\",\"asset\":\"USDT\",\"amount\":\"1.0\",\"totp\":\"$TOTP\"}"
```

**Negative tests that must all pass — verified results:**

| Test | Expected |
|---|---|
| Source address has no balance row | `{"error":{"code":"invalid_source","message":"source address is unavailable"}}` ✔ verified |
| Reuse the same TOTP code with a **new** `Idempotency-Key` | `{"error":{"code":"invalid_totp","message":"TOTP is invalid or already used"}}` ✔ verified — single-use is persisted in the `used_totp` table, so it survives a restart (`API-022`) |
| Omit `Idempotency-Key` | 400 `missing_idempotency_key` |
| Same `Idempotency-Key`, identical body | 200, returns the existing withdrawal, does **not** create a second one |
| Same `Idempotency-Key`, different body | 409 `idempotency_key_reuse` |
| Invalid `to_address` | 400 `invalid_withdrawal` |

```bash
curl -s -H "X-API-Key: $KEY" $BASE/api/v1/withdrawals/limits
# {"daily_limit_usd":"5000","remaining_usd":"5000","used_usd":"0"}   ✔ verified
```

The daily window is **UTC midnight**, not local midnight (`DB-002a`).

**Successful path** — poll `GET /api/v1/withdrawals/{id}` and expect
`requested` → `broadcast` → `confirmed`, then:

```bash
sqlite3 -header -column "file:$DB?mode=ro" \
  "SELECT id, status, txid, fee_raw, energy_source, bandwidth_source, resolved_by, failure_reason FROM withdrawals;"
```

**Pass if:** `txid` is present, the transaction is visible on
<https://nile.tronscan.org>, and `fee_raw` is populated. `GET` also returns
`network_fee_trx`, `resource_fee_trx`, `total_cost_trx`, and
`broadcast_response`, which is enough to explain any terminal outcome without
a chain lookup (`API-017`).

**What you must NOT find:**

* Any endpoint that retries or re-broadcasts an existing withdrawal
  (`API-015`, `WDR-000c`). There isn't one, by design.
* Two on-chain transactions for one withdrawal row. Ever.
* A withdrawal that moved from `failed` or `needs_operator` back to active.

A `needs_operator` withdrawal is a **valid terminal outcome**, not a crash: it
means reconciliation could not determine what happened and the service refuses
to guess. Watch `payd_withdrawals_needs_operator` in `/metrics`.

---

### T11 — Metrics

```bash
curl -s $BASE/metrics
```

Verified sample:

```
payd_chain_lag_blocks 0
payd_trongrid_requests_total 29
payd_trongrid_quota_projection_ratio 0.000290
payd_reorg_suspected_total 0
payd_orders_total{status="pending"} 1
payd_price_age_seconds +Inf
payd_clock_skew_seconds 5
payd_withdrawals_needs_operator 0
```

`payd_price_age_seconds +Inf` means no price has ever been fetched — that is
the Binance symptom from §2.

`payd_trongrid_quota_projection_ratio` is your 7-day projected usage against
`tron.daily_request_quota` (`RL-006`); `/readyz` degrades at ≥ 0.90.

---

### T12 — Config reload (SIGHUP)

Only five sections may change at runtime: `assets`, `ipn`, `resources`,
`energy`, `withdrawal` (`CFG-006`). Anything else is rejected and the old
config stays live.

On Windows there is no SIGHUP — restart the process instead. On Linux/macOS:

```bash
kill -HUP $(pgrep payd)
```

**Pass if:** disabling a consumer that a non-terminal order still names is
**refused** with `consumers are named by non-terminal orders ...`, and changing
`server.listen` is refused with the `CFG-006` message.

---

## 6. Using `payd` from your own project

`payd` is API-only (`NG-005`). Your service is a **consumer**: it creates
orders and receives IPNs.

### 6.1 Register your service as a consumer

```yaml
ipn:
  consumers:
    - name: "shop-backend"
      url: "https://shop.internal/webhooks/payd"
      secret: "<32+ random bytes, unique per consumer>"
      receives_global: false      # true also sends withdrawal.* / balance.* events
      enabled: true
    - name: "local"
      url: "http://127.0.0.1:9090/ipn"
      secret: "<a different secret — reuse is rejected, CFG-008>"
      receives_global: true
      enabled: true
  default_consumer: "local"
auth:
  api_keys:
    - name: "shop-backend"
      key_hash: "<paydev apikey>"
      scopes: ["orders:read", "orders:write"]
```

Give each service its **own API key with the narrowest scopes**. A storefront
never needs `withdrawals:write`.

### 6.2 Create an order

```http
POST /api/v1/orders
X-API-Key: <your key>
Content-Type: application/json

{
  "asset": "USDT",
  "amount": "25.00",
  "external_ref": "invoice-2291",
  "consumer": "shop-backend",
  "ttl_seconds": 1800,
  "metadata": {"user_id": 4471}
}
```

Render `address` + `amount` on your payment page and count down to
`expires_at`.

* Set `external_ref` to your invoice ID and make **retries safe**: an identical
  repeat returns 200 with the same order; a *different* one returns 409. Never
  reuse an invoice number for a different amount.
* `consumer` is immutable after creation (`API-005`).
* Naming an unknown or disabled consumer is a 400, never a silent fallback
  (`API-015`).
* Expect **503** when the pool is exhausted at `pool_max_size` (`API-006`) —
  surface it as "try again shortly", not as a payment failure.

### 6.3 Receive IPNs

```python
# Flask sketch. The rules matter more than the framework.
import hmac, hashlib

@app.post("/webhooks/payd")
def payd_ipn():
    raw = request.get_data()                       # RAW bytes — do not re-serialize
    ts  = request.headers["X-Timestamp"]
    expected = hmac.new(SECRET.encode(), (ts + ".").encode() + raw, hashlib.sha256).hexdigest()
    if not hmac.compare_digest(expected, request.headers.get("X-Signature", "")):
        return "", 401                              # IPN-024

    event = json.loads(raw)
    if already_processed(event["event_id"]):        # IPN-022 — at-least-once delivery
        return "", 200

    # IPN-025: act on event_type AND current_status together.
    if event["event_type"] == "order.confirmed" and event["current_status"] == "confirmed":
        release_goods(event["order"]["external_ref"])

    mark_processed(event["event_id"])
    return "", 200                                  # 2xx = delivered; anything else retries
```

Four rules, all of which exist because of a specific past failure:

1. **Verify `X-Signature` against your own secret** and reject anything
   unsigned or mis-signed (`IPN-024`).
2. **Treat `event_id` as an idempotency key** (`IPN-022`). Redelivery is
   guaranteed, not exceptional.
3. **Read `event_type` together with `current_status`** (`IPN-025`). An
   `order.paid` arriving with `current_status: "partial"` means a reorg undid
   it — ignore it.
4. **Release goods on `order.confirmed`, not `order.paid`.** `paid` means seen
   in a block; `confirmed` means solidified and irreversible.

Return 2xx fast and do the work asynchronously — the dispatcher's timeout is
`ipn.timeout` and a slow consumer blocks its own event queue.

### 6.4 Recommended deployment shape

* One `payd` process per environment. **Never two against the same SQLite
  file** — the single-writer constraint is why this is one binary (`TD-003`).
* Bind to `127.0.0.1` and reach it over a private network or a reverse proxy
  with mTLS. There is no TLS in `payd` itself.
* Back up `payd.db` with `sqlite3 payd.db ".backup out.db"` (safe while
  running), plus `seed.age` **and** `internal/seed/seed.key` stored separately.
  See `docs/operations/backup-and-recovery.md`.
* Keep the mnemonic offline. The xpub from `seedtool` is enough to audit
  balances without it.

---

## 7. Known gaps — endpoints the spec defines but the code does not have

Verified against `internal/api/api.go`. These return **404** today:

| Missing | Spec | Workaround while testing |
|---|---|---|
| `GET /wallets` | API §15.2 | `sqlite3 ... "SELECT * FROM addresses;"` |
| `GET /wallets/{address}` | API §15.2 | query `payments` / `balances` by address |
| `GET /wallets/with-balance` | API §15.2 | `SELECT ... FROM balances WHERE confirmed_raw != '0'` |
| `POST /wallets/{address}/delegate` | API §15.2 | send TRX to the address manually on Nile |
| `POST /wallets/{address}/disable` | API §15.2 | `UPDATE addresses SET state='disabled'` (daemon stopped) |
| `GET /ipn/dead`, `POST /ipn/{id}/retry`, `GET /ipn/consumers` | API §15.4 | `SELECT * FROM ipn_outbox WHERE status='dead'` |
| `GET /energy/status`, `GET /energy/purchases` | API §15.4 | `energy_purchases`, `energy_provider_state` tables |
| `GET /chain/params` | API §15.4 | `SELECT * FROM chain_params` |
| `GET /prices` | API §15.4 | `SELECT * FROM prices` |
| `GET /stats` | API §15.4 | `/metrics` covers most of it |

Everything that exists today:

```
POST   /api/v1/orders                        orders:write
GET    /api/v1/orders                        orders:read
GET    /api/v1/orders/{id}                   orders:read
GET    /api/v1/orders/funded-terminal        orders:read
POST   /api/v1/orders/{id}/cancel            orders:write
POST   /api/v1/orders/{id}/resolve           orders:write
GET    /api/v1/payments/unattributed         orders:read
GET    /api/v1/payments/orphaned             orders:read
POST   /api/v1/payments/{id}/attribute       orders:write
GET    /api/v1/wallets/needs-resources       wallets:read
POST   /api/v1/wallets/{address}/clear-drift wallets:write
POST   /api/v1/withdrawals                   withdrawals:write
GET    /api/v1/withdrawals                   withdrawals:read
GET    /api/v1/withdrawals/{id}              withdrawals:read
GET    /api/v1/withdrawals/limits            withdrawals:read
GET    /healthz  /readyz  /metrics           (no auth)
GET    /docs  /openapi.yaml                   (no auth)
```

The thirteen missing endpoints above are deliberately absent from the OpenAPI
document because it describes the routes the code serves, not the larger
design specification.

---

## 8. Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `validate config: ... must be positive` etc. | Config validation refuses defaults by design | Read every line of the error; it lists all problems at once |
| `decode config: field X not found` | Unknown YAML key — no typos allowed | Fix the key name |
| `config mode is 0644, want 0600 (CFG-004)` | Linux/macOS only | `chmod 600 payd.nile.yaml` |
| `encrypted seed authentication failed` | `seed.key` changed after `seed.age` was written | Restore the original `seed.key`, or delete `seed.age` and re-run `seedtool` |
| `embedded deployment key must be exactly 32 bytes` | `internal/seed/seed.key` is the wrong size — often a trailing newline | Regenerate with the §4.1 commands, rebuild |
| `create encrypted seed: ... file exists` | `seedtool` will not overwrite (`KEY-004`) | Delete `seed.age` deliberately first |
| `/readyz` stuck on `price_stale` | Go can't reach `api.binance.com` from your network | Use `https://data-api.binance.vision/api/v3/ticker/price` |
| `/readyz` reports `clock_skew` | Local clock is >30s off the chain | Sync NTP |
| `/readyz` reports `chain_lag` | >20 blocks behind | Check TronGrid reachability; add an API key |
| Payments never detected | Follower starts at the current tip on a fresh DB | Pay *after* starting the daemon; the reconciler back-fills older transfers on its own schedule |
| `invalid_source` on withdrawal | No `balances` row for that (address, asset) | The address must have actually received that asset first |
| `balance_drift` 409 | Chain and database disagree (`BAL-002`) | Investigate, then `POST /wallets/{address}/clear-drift` |
| Database locked | Two processes on one SQLite file | Run exactly one `payd`; read with `?mode=ro` |

---

## 9. Reference

* `docs/index.md` — routing table from topic / requirement-ID to spec file
* `AGENTS.md` — invariants, package layout, build order
* `Roadmap.md` — phases P1–P15 and how each was reviewed
* `docs/operations/backup-and-recovery.md` — backup and disaster recovery
* Nile explorer — <https://nile.tronscan.org>
* Nile faucet — <https://nileex.io/join/getJoinPage>
* TronGrid dashboard — <https://www.trongrid.io/dashboard>

**Non-negotiable invariants**, restated because breaking one is a fund-safety
bug rather than a style problem:

* **No automatic retry of anything that moves funds.** Ambiguity is resolved by
  reconciling against the chain, never by re-attempting.
* **All monetary amounts are decimal strings in base units.** No floats, ever.
* **All date boundaries are UTC midnight.**
