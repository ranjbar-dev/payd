# payd

Self-hosted, single-tenant **TRON payment processor**. Accept TRX and TRC-20
(USDT) payments on your own infrastructure — no third-party custodian, no
per-transaction fees beyond network costs.

## How it works

```
 customer ──pays──▶ deposit address (derived from your seed)
                          │
                    Tron blockchain
                          │  polled every 3s
                 ┌────────▼─────────┐        signed IPN        ┌──────────────┐
                 │   payd (Go)      │ ───────────────────────▶ │ your shop /  │
                 │   SQLite (WAL)   │                          │ backend      │
                 └────────▲─────────┘                          └──────────────┘
                          │ REST API (loopback only)
                 ┌────────┴─────────┐
                 │ web (Next.js)    │  operator dashboard
                 └──────────────────┘
```

1. **Order created** — your system (or the dashboard) creates an order via the
   REST API. payd assigns a deposit address from a pool of HD-wallet addresses
   derived from your encrypted BIP-39 seed.
2. **Chain watched** — the follower worker tracks new Tron blocks, decodes TRX
   and TRC-20 transfers, and attributes incoming payments to open orders
   (handling under/overpayment per policy, and reorgs up to 64 blocks deep).
3. **Confirmed** — after 19 confirmations a payment moves from `pending` to
   `confirmed`. The two balances are never merged.
4. **Notified** — payd sends an HMAC-signed IPN callback to your configured
   consumer URL(s).
5. **Withdrawn** — the withdrawal engine sends confirmed funds to your own
   wallet (no automatic sweeping), with TOTP,
   daily USD limits, and energy/bandwidth management. Fund-moving actions are
   **never retried automatically** — ambiguous outcomes are reconciled against
   the chain.

One process, one SQLite database, ten supervised workers (follower,
confirmation tracker, lifecycle, IPN, price feed, resources, withdrawals, …).

## Repository layout

| Path | What |
|------|------|
| [`backend/`](backend/) | Go service `payd` — source of truth for all money handling. Specs in `backend/docs/`. |
| [`web/`](web/) | Next.js operator dashboard — thin client over the backend API, no business logic. Specs in `web/docs/`. |
| [`QUICKSTART.md`](QUICKSTART.md) | Run the full stack locally against the TRON Nile testnet. |
| [`PRODUCTION.md`](PRODUCTION.md) | Production deployment runbook. Read before handling real funds. |

## Requirements

### Software

| Tool | Version | Used for |
|------|---------|----------|
| Go | 1.24+ | Building the backend (`payd`, `seedtool`, `paydev`) |
| Node.js | 24+ (needs `crypto.argon2`) | Running/building the dashboard |
| npm | ships with Node | Dashboard dependencies |
| `sqlite3` CLI | any (optional) | Inspecting the database |
| POSIX shell | Git Bash on Windows | The setup commands in the docs |

Main dependencies are installed automatically: Next.js 16, React 19, TanStack
Query, Tailwind CSS v4, Zod (web); the Go modules in `backend/go.mod`.

### Accounts and secrets

- **BIP-39 mnemonic** — a new, dedicated wallet (e.g. from TronLink). Use a
  throwaway one on testnet; never reuse a personal wallet.
- **TronGrid API key** — optional on Nile testnet, required on mainnet.
- **Price feed** — Binance public API (`data-api.binance.vision`), no key needed.
- Generated during setup (see QUICKSTART): backend API key + hash, backend TOTP
  secret (withdrawals), dashboard password hash, dashboard TOTP secret, session
  secret, and IPN signing secret(s).

### For production

- A Linux/macOS host (systemd units in PRODUCTION.md) with a reverse proxy terminating TLS (nginx, Caddy, or a cloud LB).
- Both services bound to loopback; only the proxy is public.
- Separate backups of `seed.key` and `seed.age` — losing `seed.key` makes the
  seed unrecoverable.

See [`PRODUCTION.md`](PRODUCTION.md) for the full checklist.

## Quick start

```bash
# backend
cd backend
go build -o payd.exe ./cmd/payd
go build -o seedtool.exe ./cmd/seedtool
go build -o paydev.exe ./tools/paydev
# create seed.age, API key, TOTP secret, payd.nile.yaml — see QUICKSTART.md §2
./payd.exe --config payd.nile.yaml          # http://127.0.0.1:8080

# dashboard
cd web
npm install
# create .env.local — see QUICKSTART.md §4
npm run dev                                 # http://localhost:3000
```

Health check: `curl -s http://127.0.0.1:8080/readyz` returns
`{"status":"ready"}` (allow ~60s for the first price fetch).

Full walkthrough: [`QUICKSTART.md`](QUICKSTART.md).

## Tests

```bash
cd backend && go test ./...
cd web && npm test && npm run typecheck
```

## Design rules

- All monetary amounts are **decimal strings in base units** — never floats.
- **No automatic retry** of any fund-moving action (broadcast, re-sign,
  bandwidth top-up, delegation).
- `confirmed` and `pending` balances are never merged.
- API changes update both `backend/internal/api/openapi.yaml` and the web client.
