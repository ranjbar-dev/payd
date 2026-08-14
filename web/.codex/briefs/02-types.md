---
ROLE: PLATFORM
TASK-ID: 02-types
GOAL: Derive strict payd response types and Zod response schemas from the authoritative OpenAPI contract.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/02-tech-stack.md
  web/docs/specs/05-data-fetching.md
  backend/internal/api/openapi.yaml — all paths and component schemas

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/lib/payd/types.ts
  web/lib/payd/schemas.ts
Everything else belongs to another agent. If you need a change outside this list, STOP and report it instead of making it.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  WST-002, WST-007, WST-010, WST-014, DAT-030, DAT-032

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
  - every requirement ID above is satisfied and you can point to where
  - every exported response type/schema is traceable to an OpenAPI schema or operation; monetary fields remain strings and unknown backend fields remain `unknown`

YOU MUST NOT:
  - add a runtime dependency (the budget is fixed in WST-001)
  - modify anything under `backend/`
  - implement a business rule the backend owns
  - write a retry, backoff, or automatic re-send on any mutation path
  - commit, push, or change git branches
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line where it is satisfied
  - anything you could not do, and why
  - any spec ambiguity or contradiction you hit
---
