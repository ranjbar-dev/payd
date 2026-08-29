# QUICKSTART — run payd locally from scratch

Brings up **both** halves of the project on your machine against the **TRON Nile
testnet**: the Go backend (`backend/`) and the Next.js operator dashboard
(`web/`). Switching to mainnet is a config change, noted at the end.

For the backend-only API test plan (T1–T12, on-chain payment/withdrawal
scenarios), see [`backend/QUICKSTART.md`](backend/QUICKSTART.md) after you finish
section 3 here.

---

## 0. Prerequisites

| Tool | Version | Check |
|---|---|---|
| Go | 1.24+ | `go version` |
| Node.js | 24+ (needs `crypto.argon2`) | `node --version` |
| npm | ships with Node | `npm --version` |
| `sqlite3` CLI | any | `sqlite3 --version` — optional, for inspecting the DB |
| A BIP-39 wallet | e.g. TronLink | to create a throwaway mnemonic and send test funds |

Everything below is written for a POSIX shell (Git Bash on Windows). PowerShell
equivalents are only given where they differ.

---

## 1. Backend — build

```bash
cd backend
go build -o payd.exe    ./cmd/payd
go build -o seedtool.exe ./cmd/seedtool
go build -o paydev.exe  ./tools/paydev
```

- `payd` — the daemon.
- `seedtool` — one-shot: encrypts your mnemonic into `seed.age`.
- `paydev` — **local dev helper only** (never touches the DB, seed, or chain).
  It exists because you cannot hand-write an Argon2id hash or a TOTP code.

`internal/seed/seed.key` (32 bytes, gitignored, `go:embed`-ed at compile time)
encrypts the mnemonic. The repo ships one. To regenerate (invalidates any
existing `seed.age`):

```bash
head -c 32 /dev/urandom > internal/seed/seed.key   # Git Bash
```
```powershell
$b = New-Object byte[] 32
[System.Security.Cryptography.RandomNumberGenerator]::Fill($b)
[System.IO.File]::WriteAllBytes("$PWD\internal\seed\seed.key", $b)
```

Back up `seed.key` **separately** from `seed.age` — losing `seed.key` means
`seed.age` can never be decrypted.

---

## 2. Backend — first-time setup

### 2.1 Throwaway Nile mnemonic

Create a **brand-new** wallet in TronLink (switch it to the **Nile** network) and
copy the 12 words. Never fund it with real money.

> Do **not** use the public `abandon abandon … about` test vector. Its address is
> shared by every tutorial; payd's reconciler will back-fill dozens of stranger
> payments and your test data becomes unreadable.

No BIP-39 wallet handy? Generate one:

```bash
# throwaway generator, run from backend/
cat > /tmp/genmn.go <<'EOF'
package main
import ("fmt"; "github.com/tyler-smith/go-bip39")
func main(){ e,_ := bip39.NewEntropy(128); m,_ := bip39.NewMnemonic(e); fmt.Print(m) }
EOF
mkdir -p tools/_genmn && mv /tmp/genmn.go tools/_genmn/main.go
go run ./tools/_genmn ; rm -rf tools/_genmn
```

### 2.2 Encrypt it

`seedtool` reads the mnemonic from **stdin only** (it never touches a file) and
refuses to overwrite an existing `seed.age`.

```bash
printf 'word1 word2 ... word12' | ./seedtool.exe --out seed.age --account 0
```

It prints the account **xpub** — your watch-only backup key.

### 2.3 API key and TOTP secret

```bash
./paydev.exe apikey
#   X-API-Key:  <keep this — shown once>
#   key_hash:   argon2id$v=19$m=65536,t=3,p=2$...$...

./paydev.exe totp-secret
#   <base32 string>
```

Keep the **`X-API-Key`** value somewhere safe — only its hash goes in the config.
Get a live 6-digit code any time with `./paydev.exe totp <base32-secret>`.

### 2.4 Write `backend/payd.nile.yaml`

Copy `backend/config.example.yaml` and change the marked lines. Working Nile
config (validated by the live test run):

```yaml
server:
  listen: "127.0.0.1:8080"
  trusted_proxy: false
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
    - url: "https://nile.trongrid.io"   # CHANGED from mainnet
      api_key: ""                       # optional on Nile
      weight: 100
  solidity_url: "https://nile.trongrid.io"
  poll_interval: 3s                     # must be exactly 3s
  confirmations_required: 19
  reorg_depth: 64
  request_timeout: 10s                  # must be exactly 10s
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
  fallback_to_burn: true
  max_burn_trx: "60"
  timeout: 15s
price:
  provider: "binance"
  url: "https://data-api.binance.vision/api/v3/ticker/price"   # see note below
  interval: 60s                        # must be exactly 60s
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
               "withdrawals:read", "withdrawals:write", "resources:write", "admin:read"]
  totp_secret: "PASTE FROM paydev totp-secret"
log:
  level: "info"
  format: "console"
```

Config gotchas — the validator refuses to start otherwise:
- Unknown YAML keys are **rejected**, not ignored — no typos.
- `poll_interval` / `request_timeout` / `price.interval` must be exactly
  `3s` / `10s` / `60s`.
- Every asset needs `verified: true` explicitly.
- `resource_wallet_index` must be ≥ 1000.
- **Binance:** Go's HTTP client times out on `api.binance.com` from some
  networks. Use `https://data-api.binance.vision/api/v3/ticker/price` (identical
  JSON). USDT orders still work with the price feed down (stablecoins
  short-circuit to `1.00`).

---

## 3. Run the backend

```bash
cd backend
./payd.exe --config payd.nile.yaml
```

Expected startup lines (the two asset `WARN`s and the solidity-host `WARN` are
intentional; one first-tick `confirmation tracker tick failed … no rows` is
normal):

```
level=INFO msg="payd started" pool_addresses=5 resource_wallet_index=1000
```

Health checks (no auth):

```bash
curl -s http://127.0.0.1:8080/healthz   # {"status":"ok"}
curl -s http://127.0.0.1:8080/readyz    # {"status":"ready"} — allow ~60s for the first price
```

Authenticated probe:

```bash
KEY="<the X-API-Key from step 2.3>"
curl -s -H "X-API-Key: $KEY" http://127.0.0.1:8080/api/v1/orders
# {"next_cursor":"","orders":[]}
```

IPN sink for local webhook testing, in a second terminal:

```bash
./paydev.exe ipnsink "nile-test-secret-do-not-reuse" 127.0.0.1:9090
```

---

## 4. Web — dependencies and credentials

```bash
cd web
npm install
```

The dashboard needs three secrets generated out of band. `web/scripts/dev-auth.mjs`
does it (dev only, never used at runtime):

```bash
node scripts/dev-auth.mjs hash 'choose-a-dashboard-password'   # -> DASH_PASSWORD_HASH
node scripts/dev-auth.mjs base32                               # -> DASH_TOTP_SECRET
node scripts/dev-auth.mjs session-secret                       # -> SESSION_SECRET
node scripts/dev-auth.mjs totp '<DASH_TOTP_SECRET>'            # current login code, any time
```

The password you type into `hash` is what you log in with — **remember it**.
This repo's local setup uses `12345678`. To change it later: re-run `hash` with
the new password, paste the result into `DASH_PASSWORD_HASH` (keep the `\$`
escaping), and restart `npm run dev`.

Create `web/.env.local` — **escape every `$` as `\$`** (Next's env loader runs
`dotenv-expand`, which otherwise eats the `$` in the Argon2id hash and login
silently fails):

```dotenv
PAYD_BASE_URL=http://127.0.0.1:8080
PAYD_API_KEY=<the X-API-Key from step 2.3>
DASH_PASSWORD_HASH=\$argon2id\$v=19\$m=65536,t=3,p=2\$<salt>\$<hash>
DASH_TOTP_SECRET=<base32 from dev-auth.mjs base32>
SESSION_SECRET=<48-byte base64 from dev-auth.mjs session-secret>
SESSION_TTL_SECONDS=28800
TRONSCAN_BASE_URL=https://nile.tronscan.org
```

Rules the app enforces at startup:
- `PAYD_BASE_URL` must be a loopback host.
- `DASH_TOTP_SECRET` must differ from every `PAYD_*` secret in the process env.
- `SESSION_SECRET` ≥ 32 bytes and must not appear anywhere in the repo.
- `TRONSCAN_BASE_URL` must be an https origin with no path, and has **no default**
  — an unset value would make a Nile deployment look identical to mainnet.

---

## 5. Run the web dashboard

```bash
cd web
npm run dev        # http://localhost:3000
```

Smoke test:
1. Open `http://localhost:3000/` → it redirects to `/login`.
2. Sign in with the **dashboard password** you chose in §4 (this repo's local
   setup uses `12345678`) and a live 6-digit code:

   ```bash
   # from web/ — reads DASH_TOTP_SECRET straight out of .env.local
   node scripts/dev-auth.mjs totp "$(grep '^DASH_TOTP_SECRET=' .env.local | cut -d= -f2)"
   ```

   The login code comes from **`DASH_TOTP_SECRET`** (`web/.env.local`) — a
   different secret from the backend's `auth.totp_secret` in
   `backend/payd.nile.yaml`. Do **not** use `paydev.exe totp` or the backend
   secret here; that pair is only for the withdrawal API and will fail login
   with "invalid credentials".
3. You land on the Overview page with live chain/quota/worker data.

Create an order from **Orders → Create order**, then follow
[`backend/QUICKSTART.md`](backend/QUICKSTART.md) §5 (T5–T10) to send a test
payment from the Nile faucet (<https://nileex.io/join/getJoinPage>) and watch it
attribute, confirm, and become withdrawable.

> On slower machines `next dev` (Turbopack) can rebuild slowly. For a stable
> local run use the production build instead: `npm run build && npm start`.

---

## 6. Switching to mainnet (local)

In `backend/payd.nile.yaml` (rename it if you like):

- `tron.endpoints[].url` → `https://api.trongrid.io`, and add a real TronGrid API
  key (`api_key:`); set `solidity_url` to an **independent** host.
- USDT `contract` → `TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t` (mainnet).
- Real IPN consumer URLs and unique 32+ byte secrets.
- Consider `energy.enabled: true` with a provider, or keep burn-only.

In `web/.env.local`: `TRONSCAN_BASE_URL=https://tronscan.org`.

Then read [`PRODUCTION.md`](PRODUCTION.md) before exposing anything.
