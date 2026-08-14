---
ROLE: PLATFORM
TASK-ID: 05-query
GOAL: Provide the minimal shared TanStack Query configuration, payd proxy fetch helper, key factory, polling-tier policy, 429 backoff, and typed error mapping required by the dashboard.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/03-architecture-and-bff.md
  web/docs/specs/05-data-fetching.md
  web/docs/specs/06-conventions.md
  backend/internal/api/openapi.yaml — error response schema and read endpoints

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/providers.tsx
  web/lib/query-keys.ts
  web/lib/query.ts
  web/lib/payd/browser-client.ts
Everything else belongs to another agent. If you need a change outside this list, STOP and report it instead of making it.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  DAT-001..DAT-010, DAT-020..DAT-026, DAT-030..DAT-036, DAT-040..DAT-044,
  BFF-022, BFF-030, BFF-032, BFF-033, UI-051, UI-052

THE SIX INVARIANTS — these override anything you think is a better idea:

  INV-1  NO RETRY CONTROL ANYWHERE IN THE WITHDRAWAL PATH. No retry, resume,
         re-broadcast, or "try again" button, link, menu item, or automatic
         re-send of a failed mutation. Mutations are configured `retry: false`
         at the query-client level. The proxy never re-sends a POST for any
         reason: timeout, 5xx, 429, connection reset.
  INV-2  MONEY IS A STRING, START TO FINISH. No Number(), parseFloat, +, -,
         toFixed, toLocaleString, or comparison operator on any amount field.
  INV-3  `confirmed` AND `pending` BALANCES ARE NEVER MERGED into one figure.
  INV-4  NO PAYD API KEY, TOTP CODE, OR SECRET REACHES THE BROWSER.
  INV-5  NO BUSINESS LOGIC IN THE CLIENT.
  INV-6  ANYTHING SCOPED TO A UTC DAY IS LABELLED UTC IN VISIBLE TEXT.

DONE WHEN:
  - `npx tsc --noEmit` clean
  - `npm run lint` clean
  - every requirement ID above is satisfied and you can point to where
  - queries default to manual tier, named tiers obey the exact intervals (including 10s withdrawal detail), polling respects hidden/offline state, and only reads may perform the one connection-failure retry permitted by BFF-021; mutations never retry

YOU MUST NOT:
  - add a runtime dependency
  - modify anything under `backend/`
  - implement a business rule the backend owns
  - add client-side amount arithmetic or client sorting of amounts
  - write retry, backoff, or automatic re-send on any mutation path
  - commit, push, or change git branches
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line where it is satisfied
  - anything you could not do, and why
  - any spec ambiguity or contradiction you hit
---
