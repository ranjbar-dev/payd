# Live end-to-end test log — payd dashboard on Nile testnet

Run started 2026-08-28. Backend `payd` on `https://nile.trongrid.io`, web `next dev`
on `localhost:3000`, driven by Playwright MCP. Backend API key + TOTP secret in
`backend/.secrets/`. Dashboard login: password `test-dashboard-pw-2026`, dashboard
TOTP secret `Q2PZ6KV4SDNESG6AUVPOCSMKAGJDRBWO` (via `web/scripts/dev-auth.mjs`).

## Environment notes / setup gotchas

- **`.env.local` + argon2 hash**: `@next/env` runs `dotenv-expand`, which treats
  `$` in a value as a variable reference. An Argon2id PHC hash
  (`$argon2id$v=19$...`) is silently truncated to garbage → login always fails
  with a generic "invalid credentials" and no hint. Fix: escape every `$` as `\$`
  in `.env.local` (or single-quote the value). Worth a note in `web/.env.example`.
  Not a dashboard code bug.

## Findings

### F1 — Overview: "Chain" card + 2 readiness links point to non-existent `/chain` (404)
- **Where:** `app/(dash)/overview-dashboard.tsx` — `<Card title="Chain" href="/chain">`
  (line ~150) and `readinessDetail()` cases `chain_lag` (~93) and `solidified_stale`
  (~94); also `reorg_depth_exceeded` (~97).
- **Observed:** `/chain` returns HTTP 404 — there is no such route. The 10 pages are
  `/`, `/orders`, `/payments`, `/addresses`, `/withdrawals`, `/resources`,
  `/webhooks`, `/reports`, `/system`.
- **Expected:** chain status/quota/params are surfaced on the System **health** tab
  (coverage matrix `WSYS-061`) and `reorg_suspected` links to the orphaned-payments
  worklist (`WOVW-022`).
- **Status:** OPEN — fix pending.
