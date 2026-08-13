# AGENTS.md

Root guide for coding agents working in this repo.

## What this project is

A self-hosted TRON payment processor. Two parts:

- **`backend/`** — Go service (`payd`) that issues deposit addresses, watches
  the Tron chain, attributes TRX/TRC-20 payments to orders, sends signed IPN
  callbacks, and runs automated withdrawals. Single process, single SQLite DB.
  See [`backend/AGENTS.md`](backend/AGENTS.md).
- **`web/`** — Next.js dashboard for managing payments, addresses,
  transactions, and orders against the backend's REST API.
  See [`web/AGENTS.md`](web/AGENTS.md).

## Rules

- Read the relevant subproject's `AGENTS.md` before touching its code —
  don't apply backend conventions to web or vice versa.
- The backend is the source of truth for business logic and money handling.
  `web` is a client, not a second implementation of order/payment rules.
- Cross-cutting changes (e.g. an API contract change) require updating both
  sides: `backend/internal/api/openapi.yaml` and the `web` client that
  consumes it.
