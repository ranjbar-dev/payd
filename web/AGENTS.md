# AGENTS.md (web)

This file tells a coding agent how to work in `web/`. Read before touching code.

## What this is

Next.js operator dashboard for the payd payment processor. Talks to the
`backend` service's REST API to manage orders, payments, deposit addresses,
withdrawals, resources, and webhooks. No business logic lives here — this is a
thin client over the backend's API (see `backend/internal/api/openapi.yaml`).

## Start here

**[`docs/index.md`](docs/index.md)** is the spec set: 17 numbered files with
stable `WEB-*`-style requirement IDs, one per subsystem or page. Open the file
that matches your task, not the whole set.

- Building a page → its page spec (`docs/specs/07`–`15`)
- Adding any call to payd → [`docs/specs/03-architecture-and-bff.md`](docs/specs/03-architecture-and-bff.md) and [`docs/specs/05-data-fetching.md`](docs/specs/05-data-fetching.md)
- Rendering an amount, timestamp, or status → [`docs/specs/06-conventions.md`](docs/specs/06-conventions.md)
- **Anything touching withdrawals → [`docs/specs/11-withdrawals.md`](docs/specs/11-withdrawals.md) §11.0 first, always**
- What to build next → [`docs/specs/16-implementation-phases.md`](docs/specs/16-implementation-phases.md)

## Status

Scaffold only. No code yet. The `app/transactions/` directory is to be deleted
(`WST-013`): money in is `payments`, money out is `withdrawals`.

## Rules

- **No retry on any withdrawal path.** No retry button, no automatic re-send of
  a failed mutation, no HTTP client default that re-attempts a `POST`. The
  backend never retries a fund-moving action (`WDR-000`) and the UI must not
  undo that guarantee.
- **All money amounts are decimal strings.** Never parse into floats, never do
  arithmetic on them, never sort them client-side. Display as strings.
- **`confirmed` and `pending` balances are never merged** into one figure.
- **The payd API key never reaches the browser.** Every call goes through the
  Next.js proxy at `app/api/payd/[...path]/route.ts`, which is the only place
  that sets `X-API-Key`.
- Don't duplicate validation or business rules that belong in the backend.
  This app renders and submits; the backend decides.
- An API contract change updates both sides: `backend/internal/api/openapi.yaml`
  and the client here, plus `docs/specs/17-api-coverage-matrix.md`.

<!-- BEGIN:nextjs-agent-rules -->

# This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` (resolved from this file's directory; in monorepos the `next` package may not be visible from the repo root) before writing any code. Heed deprecation notices.

This block is written and re-added by `next dev` — verify at `node_modules/next/dist/server/lib/generate-agent-files.js`. Removing it from a diff only re-creates the uncommitted change; committing it with your work keeps the tree clean.

<!-- END:nextjs-agent-rules -->
