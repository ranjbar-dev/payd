# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## How work is split — Claude plans, Codex codes

**Claude Code owns the thinking. Codex CLI owns the typing.**

- **Claude** does all of: understanding the request, reading the code and specs,
  deciding the approach, breaking it into tasks, writing the exact task brief,
  reviewing every diff against the invariants below, running the build/tests,
  driving Playwright verification, committing, and reporting. Claude does **not**
  hand-edit source files for feature work — small mechanical fixes to unblock a
  build or a review are the only exception.
- **Codex CLI** (`codex exec -m gpt-5.6-terra --approve-for-me -C <dir>`) does
  the actual code changes, one bounded task at a time, from the brief Claude
  writes. Run it non-interactively; **pass the prompt from a file**
  (`"$(cat brief.txt)"`) — an inline heredoc hangs on stdin when backgrounded.
  `--approve-for-me` can't be combined with `-s`.

Every Codex brief must name: the files in scope, the spec/requirement IDs, what
to change, and the non-negotiable invariants it must not break (see below and
each subproject's `CLAUDE.md`). After Codex returns, Claude re-verifies with
`node_modules/.bin/tsc --noEmit` + `npm run build` (Codex's own lint report is
unreliable when runs overlap) and only then commits.

## What this is

Self-hosted, single-tenant TRON payment processor. Two subprojects:

- **`backend/`** — Go service (`payd`): issues deposit addresses, watches the
  Tron chain, attributes TRX/TRC-20 payments to orders, sends signed IPN
  callbacks, runs automated withdrawals. One process, one SQLite DB, ten
  supervised workers. **Source of truth for all money handling.**
- **`web/`** — Next.js operator dashboard. Thin client over the backend REST
  API. No business logic.

## Where to look

| Working on… | Read first |
|-------------|-----------|
| Backend code | [`backend/CLAUDE.md`](backend/CLAUDE.md) → `backend/docs/index.md` |
| Web code | [`web/CLAUDE.md`](web/CLAUDE.md) → `web/docs/index.md` |
| Web UI / design | [`web/CLAUDE.md`](web/CLAUDE.md) "UI work" → [`web/DESIGN.md`](web/DESIGN.md); invoke the `ui-ux-pro-max` skill for every UI task |
| Local setup | [`QUICKSTART.md`](QUICKSTART.md) |
| Deploy | [`PRODUCTION.md`](PRODUCTION.md) |

`AGENTS.md` in each directory is a pointer to that directory's `CLAUDE.md` —
one source, read by both Claude Code and Codex.

Both `docs/index.md` files are routing tables: topic / requirement-ID prefix →
the one spec file. Requirement IDs (`WDR-017`, `CHN-016`, `WEB-*`) are stable —
cite them in code comments and commits.

## Run the stack locally

```bash
# backend — from backend/
./payd.exe --config payd.nile.yaml            # http://127.0.0.1:8080

# web — from web/
npm run dev                                   # http://localhost:3000
```

Health: `curl -s http://127.0.0.1:8080/readyz` → `{"status":"ready"}` (allow
~60s for the first price fetch).

## Dashboard login

- URL `http://localhost:3000/login`, password **`12345678`** (local dev).
- 6-digit code — from `web/`:

  ```bash
  node scripts/dev-auth.mjs totp "$(grep '^DASH_TOTP_SECRET=' .env.local | cut -d= -f2)"
  ```

The login code uses **`DASH_TOTP_SECRET`** (`web/.env.local`), **not** the
backend's `auth.totp_secret` in `backend/payd.nile.yaml` — that one is only for
the withdrawal API. Using the wrong secret fails with "invalid credentials".

## Non-negotiable invariants (both sides)

- **No automatic retry of any fund-moving action** — withdrawal broadcast,
  re-sign, bandwidth top-up, self-delegation. At most once, ever. Ambiguous
  outcomes are reconciled against the chain. Web must not add a retry button or
  a retrying HTTP client.
- **All monetary amounts are decimal strings in base units.** Never float,
  never client-side arithmetic or sort.
- **`confirmed` and `pending` balances are never merged.**
- An API contract change updates both sides: `backend/internal/api/openapi.yaml`
  and the web client + `web/docs/specs/17-api-coverage-matrix.md`.

## Gotchas hit before

- **Backend stuck `readyz: degraded` (`chain_lag`), no follower log lines:**
  the `payd.nile.db` chain cursor fell hundreds of blocks behind Nile head and
  the follower hung in 125ms/block catch-up. Fix: stop payd,
  `mv payd.nile.db payd.nile.db.bak-$(date +%s)`, remove `-shm`/`-wal`,
  restart. Fresh DB commits at the tip. Pool addresses re-derive from
  `seed.age`; only cached test data is lost.
- **`.env.local`:** every `$` must be written `\$` or `dotenv-expand` eats it
  in the Argon2id hash and login silently fails.
- **Binance price feed:** use `https://data-api.binance.vision/...`, not
  `api.binance.com` (Go client times out on some networks). USDT orders still
  work with the feed down (stablecoin short-circuits to `1.00`).
