---
ROLE: PLATFORM
TASK-ID: 03-auth-foundation
GOAL: Implement the signed session, secure login/logout, and authenticated OpenAPI-derived BFF as one foundation with no proxy/session bootstrap gap.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/02-tech-stack.md
  web/docs/specs/03-architecture-and-bff.md
  web/docs/specs/04-auth-and-session.md
  web/docs/specs/05-data-fetching.md
  backend/internal/api/openapi.yaml — all paths and component schemas

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/(auth)/login/page.tsx
  web/app/(dash)/layout.tsx
  web/app/(dash)/scope-banner.tsx
  web/app/api/auth/login/route.ts
  web/app/api/auth/logout/route.ts
  web/app/api/payd/[...path]/route.ts
  web/lib/env.ts
  web/lib/session.ts
  web/lib/session.test.ts
  web/lib/payd/client.ts
  web/lib/payd/allowlist.ts
  web/scripts/generate-payd-allowlist.mjs
  web/package.json
Everything else belongs to another agent. If you need a change outside this list, STOP and report it instead of making it.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  WST-001, WST-010, WST-011, WST-012, WST-020, WST-021, WST-022, WST-023,
  BFF-001..BFF-012, BFF-020..BFF-023, BFF-030..BFF-033,
  AUTH-001..AUTH-033, AUTH-040, AUTH-041, AUTH-050..AUTH-052

THE SIX INVARIANTS — these override anything you think is a better idea:

  INV-1  NO RETRY CONTROL ANYWHERE IN THE WITHDRAWAL PATH. No retry, resume,
         re-broadcast, or "try again" button, link, menu item, or automatic
         re-send of a failed mutation. Mutations are configured `retry: false`
         at the query-client level. The proxy never re-sends a POST for any
         reason: timeout, 5xx, 429, connection reset. The backend never retries
         a fund-moving action; client-side retry silently undoes that guarantee
         and pays out twice.
  INV-2  MONEY IS A STRING, START TO FINISH. No Number(), parseFloat, +, -,
         toFixed, toLocaleString, or comparison operator on any amount field —
         including sorting, filtering, and zero-checks. Ever.
  INV-3  `confirmed` AND `pending` BALANCES ARE NEVER MERGED into one figure.
  INV-4  NO PAYD API KEY, TOTP CODE, OR SECRET REACHES THE BROWSER — not in a
         response body, a JS-readable cookie, a URL, localStorage, an error
         message, or the built bundle.
  INV-5  NO BUSINESS LOGIC IN THE CLIENT. Never compute whether an order is
         paid, a withdrawal is permitted, or a balance suffices. Render what
         the API said.
  INV-6  ANYTHING SCOPED TO A UTC DAY IS LABELLED UTC IN VISIBLE TEXT.

DONE WHEN:
  - `npx tsc --noEmit` clean
  - `npm run lint` clean
  - `npm run build` clean
  - every requirement ID above is satisfied and you can point to where
  - the session test proves signature/encryption verification, expiry, invalidation, and CSRF rejection; the proxy allowlist is generated from OpenAPI at build time; every POST is attempted once only

YOU MUST NOT:
  - add a runtime dependency outside WST-001; use Node 24's native Argon2id and crypto APIs
  - modify anything under `backend/`
  - implement a business rule the backend owns
  - write a retry, backoff, or automatic re-send on any mutation path
  - commit, push, or change git branches
  - add an unauthenticated proxy exception or set `X-API-Key` outside `app/api/payd/[...path]/route.ts`
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line where it is satisfied
  - anything you could not do, and why
  - any spec ambiguity or contradiction you hit
---
