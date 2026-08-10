# 4. Configuration

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §4
**ID prefixes in this file:** `CFG-*`
**Related:** [`10-ipn-dispatcher.md`](10-ipn-dispatcher.md) for consumer config semantics (CFG-006/007/008/009/010); [`12-resource-management.md`](12-resource-management.md) for the `energy`/`resources` blocks; [`13-withdrawal-engine.md`](13-withdrawal-engine.md) for the `withdrawal` block

---

Single YAML file, path via `--config` flag. Loaded once at startup, validated before any worker starts.

```yaml
server:
  listen: "127.0.0.1:8080"
  trusted_proxy: false                       # true only behind a TLS reverse proxy
  read_timeout: 15s
  write_timeout: 30s

database:
  path: "/var/lib/payd/payd.db"

wallet:
  seed_file: "/var/lib/payd/seed.age"      # encrypted by seedtool
  account: 0                                # BIP-44 account index
  pool_initial_size: 20
  pool_min_free: 5                          # derive more when free count drops below this
  pool_max_size: 500                         # hard ceiling; order creation 503s beyond this
  cooldown: 2h                              # address quarantine before returning to pool

tron:
  endpoints:
    # CHN-025: entries MUST be distinct hosts. Two entries pointing at the same
    # hostname are not a failover — TronGrid quota is per account and the keyless
    # path is throttled per source IP from the same server.
    - url: "https://api.trongrid.io"
      api_key: "your-trongrid-api-key"
      weight: 100
    - url: "https://tron-mainnet.gateway.tatum.io"   # independent provider
      api_key: "your-secondary-api-key"
      weight: 10
  solidity_url: "https://api.trongrid.io"
  poll_interval: 3s
  confirmations_required: 19                # additional depth requirement; see CNF-002
  reorg_depth: 64                           # block hashes retained for reorg detection
  request_timeout: 10s
  daily_request_quota: 100000               # CHN-023 derives its 70% soft cap from RL-001

assets:
  - symbol: "TRX"
    kind: "native"
    decimals: 6
    verified: true                          # CFG-014: required for every asset
  - symbol: "USDT"
    kind: "trc20"
    contract: "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
    decimals: 6
    min_deposit: "0.5"                      # amounts below this are flagged is_dust
    verified: true
  - symbol: "USDC"
    kind: "trc20"
    contract: "TEkxiTehnzSmSe2XqrBj4w32RUN966rdz8"
    decimals: 6
    min_deposit: "0.5"
    verified: true

orders:
  default_ttl: 30m
  underpayment_policy: "partial"            # partial | reject
  overpayment_policy: "credit_and_log"

ipn:
  consumers:
    - name: "shop-backend"
      url: "http://localhost:8080/api/tron-ipn-callback"
      secret: "shared-hmac-secret-a"
      receives_global: true          # withdrawal.* and payment.unattributed
      enabled: true
    - name: "analytics"
      url: "http://localhost:9090/hooks/tron"
      secret: "shared-hmac-secret-b"
      receives_global: false
      enabled: true
  default_consumer: "shop-backend"   # used when an order names none
  timeout: 10s
  max_attempts: 8
  workers: 4
  backoff: [10s, 30s, 2m, 10m, 1h, 6h, 12h, 24h]

energy:
  enabled: true
  provider: "tronzap"                # registered energy.Provider implementation
  api_url: "https://api.tronzap.com"
  api_key: "provider-api-key"
  api_secret: "provider-api-secret"
  timeout: 15s
  rent_amount: 131000                # energy units per request
  rent_duration: 1h                  # shortest window that covers the transfer
  max_price_trx: "6"                 # refuse a quote above this; fall through
  balance_warn_trx: "50"             # alert when prepaid credit drops below
  poll_interval: 2s                  # how often to check delegation arrival
  poll_timeout: 90s                  # give up waiting and fall through
  fallback_to_burn: true
  max_burn_trx: "60"                 # hard ceiling per transaction; validated at
                                     # startup against 131000 × live getEnergyFee
                                     # (ENR-017). NOT a fixed figure — the old
                                     # default of 20 assumed 100 sun/energy, which
                                     # is a governance-controlled chain parameter.

price:
  provider: "binance"
  url: "https://api.binance.com/api/v3/ticker/price"
  interval: 60s
  pairs: ["TRXUSDT"]
  stale_after: 5m                    # blocks order creation AND withdrawal creation

resources:
  min_energy: 131000
  min_bandwidth: 345
  check_interval: 5m                 # fast-tier cadence only
  slow_check_interval: 6h            # everything below poll_threshold_usd
  poll_threshold_usd: "10"           # RES-001a: fast tier only above this balance
  max_polled_addresses: 50           # RES-001a: hard cap on the fast tier
  resource_wallet_index: 1000        # MUST be outside the deposit pool range (CFG-013)
  bandwidth_strategy: "topup"        # topup | delegate  (RES-007)
  bandwidth_topup_trx: "2"           # TRX sent to a source address short on bandwidth

withdrawal:
  enabled: true
  daily_limit_usd: "5000"
  fee_limit_trx: 100                 # max TRC-20 execution fee per tx
  expiration: 60s
  require_totp: true
  # There is deliberately no retry, retry_attempts, or retry_backoff key here.
  # See the withdrawal-engine spec §13.0. A withdrawal is attempted at most once, ever.

auth:
  api_keys:
    - name: "internal-services"
      key_hash: "argon2id$..."              # hash, never plaintext
      scopes: ["orders:write", "wallets:read"]
    - name: "dashboard"
      key_hash: "argon2id$..."
      scopes: ["orders:read", "wallets:read", "withdrawals:write", "resources:write"]
  totp_secret: "BASE32SECRET"

log:
  level: "info"
  format: "json"
```

| ID | Requirement |
|---|---|
| CFG-001 | The service MUST fail to start on any invalid config value rather than applying a default silently |
| CFG-002 | `assets[].contract` MUST be validated with `hdwallet.IsValidAddress(hdwallet.TRX, …)` at startup |
| CFG-003 | API keys MUST be stored as Argon2id hashes, never plaintext |
| CFG-004 | The config file MUST be mode `0600`; the service MUST refuse to start if it is world-readable |
| CFG-005 | Changing `assets` MUST NOT require a database migration; new tokens are watched from the next block onward |
| CFG-006 | Config MUST be reloadable on `SIGHUP` for `assets`, `ipn`, `resources`, `energy`, and `withdrawal` sections only; wallet and database changes require a restart. **A reload MUST be rejected in full — retaining the previous config — if it would remove or disable a consumer named by any order in a non-terminal state.** The rejection MUST be logged with the IDs of the blocking orders |
| CFG-007 | Consumer names MUST be unique, non-empty, and stable — they are referenced by orders and stored in `ipn_outbox` |
| CFG-008 | Each consumer MUST have its own `secret`; reusing one secret across consumers MUST be rejected at startup, since a shared secret lets any consumer forge events to another |
| CFG-009 | `ipn.default_consumer` MUST name an existing enabled consumer |
| CFG-010 | Removing a consumer from config MUST NOT delete its pending outbox rows; those rows MUST move to `dead` with a clear reason. **Rows moved to `dead` for this reason MUST NOT be automatically re-queued if the consumer is later re-added** — redelivery is via IPN-010 only, so a re-add cannot silently replay hours of stale events |
| CFG-011 | `energy.api_key` and `energy.api_secret` MUST be redacted from all logs and from any config-dump endpoint |
| CFG-012 | If `energy.enabled` is true, the provider MUST be reachable at startup or the service MUST log a warning and continue with burn-only sourcing — an unreachable energy provider MUST NOT prevent startup |
| CFG-013 | `resources.resource_wallet_index` MUST NOT fall within the deposit pool range. Indices **0–999 are the deposit pool**; **1000+ are operational** (resource wallet, future cold-storage destinations). At startup the service MUST insert the resource wallet address with `state = 'disabled'` if absent, MUST verify it is `disabled` if present, and MUST refuse to start otherwise |
| CFG-014 | Every entry in `assets` MUST carry an explicit `verified: true` flag. Startup and each accepted SIGHUP reload MUST emit one deterministic **info** record listing every configured token's symbol, contract address, and decimals, sorted by symbol. Adding a token is a security-relevant act (see DET-004a) and MUST be deliberate rather than incidental |
| CFG-015 | Startup MUST reject a configuration in which two enabled `tron.endpoints` share a hostname (see CHN-025) |
| CFG-016 | `server.listen` MUST name a numeric loopback IP unless `server.trusted_proxy` is explicitly `true`. Enabling `trusted_proxy` is an operator acknowledgement that all remote access terminates TLS at a reverse proxy; it does not add TLS to `payd` or enable trust of forwarded identity headers |
