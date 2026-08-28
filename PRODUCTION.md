# PRODUCTION.md — running payd in production

payd moves real funds. Read this in full before exposing anything. It assumes
you have already run the stack locally per [`QUICKSTART.md`](QUICKSTART.md).

Authoritative backend rules live in `backend/docs/specs/` (esp. `04-configuration.md`,
`14-key-management.md`, `17-operations.md`) and
`backend/docs/operations/backup-and-recovery.md`. Dashboard rules live in
`web/docs/specs/` (esp. `02`, `03`, `04`). This file is the deployment runbook,
not a re-statement of those.

---

## 1. Topology

```
            Internet
               │  HTTPS
        ┌──────▼───────────────┐
        │  reverse proxy (TLS) │   nginx / Caddy / a cloud LB
        │  - terminates TLS    │
        │  - blocks /openapi.yaml and /metrics from the public
        └───┬──────────────┬───┘
            │ 127.0.0.1    │ 127.0.0.1
     ┌──────▼─────┐  ┌─────▼──────────┐
     │  web       │  │  payd daemon   │   both bind loopback only
     │  next start│──▶  127.0.0.1:8080│   web is the ONLY caller of payd
     │  :3000     │  │  SQLite (WAL)  │
     └────────────┘  └────────────────┘
```

Non-negotiable:

- **`payd` is never exposed directly** (`OPS-009`). It serves plain HTTP; the API
  key and TOTP codes travel in headers. Remote access is only through a
  TLS-terminating reverse proxy. Keep `server.listen` on `127.0.0.1` and
  `server.trusted_proxy: false` unless a trusted proxy is genuinely in front of it.
- **The browser never talks to payd.** Every dashboard call goes through the
  Next.js BFF proxy (`web/app/api/payd/[...path]/route.ts`), which is the only
  place the `X-API-Key` is set. There is no `NEXT_PUBLIC_*` variable anywhere.
- **One `payd` process per environment.** Never two against one SQLite file
  (`TD-003`) — the single-writer constraint is the whole reason it is one binary.
- **The `X-API-Key` never reaches the browser**, a response body, a cookie, a
  URL, `localStorage`, or a client log (`INV-4` / `BFF-002`).

---

## 2. Backend — production config

Build once on the target platform (`go build -o payd ./cmd/payd`, plus `seedtool`
and — only on an operator workstation, never the server — `paydev`).

Start from `backend/config.example.yaml`. Production differences from the
QUICKSTART Nile config:

| Key | Production value |
|---|---|
| `server.listen` | `127.0.0.1:8080` (unchanged — loopback only) |
| `server.trusted_proxy` | `true` **only** once a TLS proxy is in front; else `false` |
| `tron.endpoints` | `https://api.trongrid.io` with a real `api_key`, plus a **second** provider on a different host for failover (weights e.g. 100 / 10) |
| `tron.solidity_url` | an **independent** host from the fullnode endpoints (a shared host logs a startup `WARN`) |
| `assets` USDT `contract` | `TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t` (mainnet), `verified: true` |
| `ipn.consumers` | real HTTPS URLs; each `secret` is 32+ random bytes, **unique per consumer** (reuse is rejected). Give each consuming service its own `auth.api_keys` entry with the narrowest scopes — a storefront never needs `withdrawals:write` |
| `ipn.max_attempts` / `backoff` | size to your consumers' real availability |
| `energy` | `enabled: true` with a provider (`provider`, `api_url`, `api_key`, `api_secret`, `rent_amount`, `rent_duration`, `max_price_trx`, `balance_warn_trx`) if you rent energy; otherwise keep burn-only and set a deliberate `max_burn_trx` ceiling |
| `price.url` | `https://api.binance.com/...` or `https://data-api.binance.vision/...` — verify Go can actually reach it from the server (curl is not a proof; Go's client times out on some networks) |
| `withdrawal.daily_limit_usd` | your real UTC-day cap |
| `withdrawal.require_totp` | **`true`**. Do not disable it. `POST /withdrawals` is the only TOTP-gated route whose enforcement is config-conditional; a `false` here lets a direct API caller move funds with no second factor |
| `auth.api_keys[].key_hash` | Argon2id hash from `paydev apikey`, generated on the operator workstation |
| `auth.totp_secret` | base32 from `paydev totp-secret`; also enrol it in the operator's authenticator app |
| `log.level` | `info`; `log.format` | `json` for shipping to a log store |

Constants the validator pins (startup fails otherwise): `poll_interval: 3s`,
`request_timeout: 10s`, `price.interval: 60s`, every asset `verified: true`,
`resource_wallet_index >= 1000`, no unknown keys.

### File permissions (`CFG-004`, `KEY-004`, `DB-006`) — Linux/macOS

```bash
chmod 600 payd.yaml seed.age internal/seed/seed.key payd.db
```

`payd` refuses to start on a world-readable config. On Windows this check is
skipped — restrict the directory ACL instead (`icacls`).

### The `broadcasthex` broadcast path

payd broadcasts signed transactions via `POST /wallet/broadcasthex` with the full
signed-transaction protobuf envelope (one request, never retried — `WDR-014a`).
This is compatible with both mainnet TronGrid and the Nile testnet nodes. If you
front payd with a custom TRON RPC, make sure `/wallet/broadcasthex` is allowed.

---

## 3. Key management and backup

- **`internal/seed/seed.key`** (32 bytes) is compiled into the binary and
  decrypts `seed.age`. Back it up **separately** from `seed.age`, offline. Losing
  `seed.key` alone makes `seed.age` permanently undecryptable; losing both loses
  the wallet.
- **`seed.age`** — the encrypted mnemonic. Offline backup.
- **The mnemonic itself** — offline, air-gapped. The `seedtool` xpub is enough to
  audit balances without it.
- **`payd.db`** — hot backup while running (WAL mode permits it):

  ```bash
  sqlite3 payd.db ".backup '/backups/payd-$(date +%F-%H%M).db'"
  ```

  Schedule it (cron / a timer). Keep a rotation.
- **Logs MUST NOT contain** the mnemonic, private keys, API keys, TOTP codes, or
  IPN secrets (`OPS-013`) — verify your log pipeline does not echo request bodies
  or headers.

Full restore and total-loss replay procedure:
`backend/docs/operations/backup-and-recovery.md` (`OPS-011`/`OPS-012`/`OPS-014`).
Key point: after any restore, withdrawals that were in `broadcast`, `signing`, or
`needs_operator` MUST be reconciled by hand against Tronscan before a new
withdrawal is issued from those addresses.

---

## 4. Running as a service

### Linux — systemd

`/etc/systemd/system/payd.service`:

```ini
[Unit]
Description=payd TRON payment processor
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=payd
WorkingDirectory=/opt/payd
ExecStart=/opt/payd/payd --config /opt/payd/payd.yaml
Restart=on-failure
RestartSec=5
# hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/payd
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

`/etc/systemd/system/payd-web.service`:

```ini
[Unit]
Description=payd operator dashboard
After=payd.service
Requires=payd.service

[Service]
Type=simple
User=payd-web
WorkingDirectory=/opt/payd-web
EnvironmentFile=/opt/payd-web/.env.production   # 0600, owned by payd-web
ExecStart=/usr/bin/node /opt/payd-web/node_modules/next/dist/bin/next start -p 3000
Restart=on-failure
RestartSec=5
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
```

```bash
systemctl daemon-reload
systemctl enable --now payd payd-web
```

### Windows

Run both under a service supervisor (NSSM, WinSW, or a scheduled task set to run
at boot). Point the daemon at `payd.exe --config payd.yaml` and the web at
`npm run start` from `web/`. Set the environment on the service, not in a shell.

### Config reload

`payd` reloads `assets`, `ipn`, `resources`, `energy`, and `withdrawal` on
**SIGHUP** (Linux/macOS). Anything else is rejected and the old config stays
live. **Windows has no SIGHUP — restart the process.**

---

## 5. Reverse proxy

Terminate TLS at the proxy. Route:

- `/` and everything else → the **web** app on `127.0.0.1:3000`.
- The web app calls payd itself over loopback — the proxy does **not** need a
  route to `:8080`.
- **Block from the public**: `payd`'s `/openapi.yaml` and `/metrics` are
  unauthenticated at the daemon. They are only reached via the dashboard's proxy
  (which requires a session); do not add a public path to them.

nginx sketch:

```nginx
server {
  listen 443 ssl http2;
  server_name payd.example.com;
  ssl_certificate     /etc/ssl/payd/fullchain.pem;
  ssl_certificate_key /etc/ssl/payd/privkey.pem;

  add_header Strict-Transport-Security "max-age=31536000" always;

  location / {
    proxy_pass http://127.0.0.1:3000;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto https;
    proxy_read_timeout 130s;          # CSV exports stream up to 120s
  }
}
```

If you set `server.trusted_proxy: true` on the daemon, make sure only this proxy
can reach `127.0.0.1:8080` (it already binds loopback; a host firewall is belt
and braces).

---

## 6. Web — production build and env

```bash
cd web
npm ci
npm run build          # runs prebuild: regenerates the payd route allowlist from openapi.yaml
npm run start          # or: node node_modules/next/dist/bin/next start -p 3000
```

`web/.env.production` (mode 0600, owned by the web service user; **escape `$` as
`\$`** — the Argon2id hash contains them):

```dotenv
PAYD_BASE_URL=http://127.0.0.1:8080
PAYD_API_KEY=<the operator key; needs all 8 scopes>
DASH_PASSWORD_HASH=\$argon2id\$v=19\$m=65536,t=3,p=2\$<salt>\$<hash>
DASH_TOTP_SECRET=<base32; NOT payd's totp_secret — must be a different value>
SESSION_SECRET=<>= 32 random bytes, base64; must not appear in the repo>
SESSION_TTL_SECONDS=28800
TRONSCAN_BASE_URL=https://tronscan.org
```

Enforced at startup — the app refuses to boot otherwise:

- `PAYD_BASE_URL` must be loopback.
- `DASH_TOTP_SECRET` (dashboard login 2FA) must differ from every `PAYD_*` secret
  in the process env — it is **not** payd's withdrawal TOTP.
- `SESSION_SECRET` ≥ 32 bytes and absent from the repository (example files
  included).
- `TRONSCAN_BASE_URL` has **no default** and must be an https origin with no
  path. On mainnet it is `https://tronscan.org`; a wrong value here is how a
  testnet deployment gets mistaken for mainnet.

Generate the three secrets on an operator workstation with
`web/scripts/dev-auth.mjs` (`hash` / `base32` / `session-secret`). The dashboard
login code at runtime comes from the operator's authenticator app enrolled with
`DASH_TOTP_SECRET`.

Sessions are a signed, encrypted, `HttpOnly; Secure; SameSite=Strict` cookie with
a fixed absolute lifetime (`SESSION_TTL_SECONDS`, default 8h) — **no sliding
renewal**. A restart invalidates every session.

---

## 7. Monitoring and alerting

Scrape `payd`'s `/metrics` (it needs a valid `X-API-Key` — scrape it from inside
the trust boundary, e.g. Prometheus on the same host with the key in a file SD).

Ship `backend/docs/operations/payd-alerts.yml` to Prometheus and extend it. The
must-have alerts:

| Condition | Meaning |
|---|---|
| `payd_withdrawals_needs_operator > 0` | **critical** — funds in an unknown on-chain state, always a human (`OPS-006`) |
| `payd_trongrid_quota_projection_ratio >= 0.60` for 5m | approaching the daily RPC quota; at 0.90 `/readyz` goes 503 |
| `/readyz` != 200 | chain lag > 20 blocks, solidified height stalled 5m, price stale, DB unwritable, reorg > `reorg_depth`, clock skew > 30s, or energy burn ceiling would refuse a transfer (`OPS-001`) |
| `payd_ipn_dead_total{consumer}` rising | a consumer is not receiving notifications |
| `payd_payments_orphaned_unresolved > 0` | a reorg took a credited payment away |
| `payd_clock_skew_seconds > 30` | withdrawals will be rejected / expire instantly, indistinguishable from an RPC fault (`OPS-005`) — sync NTP |
| `payd_energy_cost_trx_total{source="burn"}` rising while `{source="rent"}` flat | the energy provider is silently failing (`OPS-004`) |
| worker `last_tick_at` freshness (from `GET /workers`) | a wedged worker loop |

Also alert on both services being down and on the reverse proxy's 5xx rate.

The dashboard's own **System** page (Health, Workers, Quota tabs) surfaces most
of this for a human who is already logged in; the alerts above are what page
someone who is not.

---

## 8. Upgrades and rollback

1. `sqlite3 payd.db ".backup ..."` first, always.
2. Build the new binaries on the target; keep the old ones (`payd.prev`).
3. Stop web, stop payd. Swap binaries. Start payd, wait for `/readyz` = 200 and
   the Withdrawal Engine's startup resolution to finish (`OPS-011`). Start web.
4. Smoke: log in, Overview loads, `GET /api/v1/withdrawals/limits` via the
   dashboard returns the expected cap.
5. Rollback = reverse binary swap + restore the pre-upgrade DB backup if a
   migration ran. Schema migrations are forward-only; a rollback past one needs
   the backup.

An API contract change is a coordinated deploy: `backend/internal/api/openapi.yaml`,
the web client, `web/docs/specs/17-api-coverage-matrix.md`, and the generated
allowlist (`npm run build` regenerates it) all move together.

---

## 9. Security checklist before go-live

- [ ] `payd` binds `127.0.0.1` only; host firewall confirms `:8080` is not
      externally reachable.
- [ ] TLS terminates at the proxy; HSTS set; `/openapi.yaml` and `/metrics` are
      not publicly routable.
- [ ] `payd.yaml`, `seed.age`, `seed.key`, `payd.db`, `.env.production` are mode
      `0600` (or locked-down ACLs on Windows), owned by the service user.
- [ ] `seed.key` and `seed.age` are backed up **separately** and offline; the
      mnemonic is air-gapped.
- [ ] `withdrawal.require_totp: true`; the operator's authenticator holds
      `auth.totp_secret`; `DASH_TOTP_SECRET` is a **different** value in its own
      authenticator entry.
- [ ] Each IPN consumer has a unique 32+ byte secret and its own narrow-scoped
      API key.
- [ ] Prometheus is scraping `/metrics` from inside the trust boundary;
      `PaydWithdrawalNeedsOperator` and `/readyz` alerts route to a human.
- [ ] `payd.db` hot-backup cron is running and restore has been rehearsed once.
- [ ] Log pipeline verified to contain no secrets or request bodies.
- [ ] `TRONSCAN_BASE_URL=https://tronscan.org` (mainnet) — not the Nile value.
