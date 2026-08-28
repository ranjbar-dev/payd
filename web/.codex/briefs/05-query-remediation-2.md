---
ROLE: PLATFORM
TASK-ID: 05-query
GOAL: Implement the four-file query platform now; repository/spec discovery has already been completed by prior attempts.

CONTEXT ALREADY VERIFIED:
  - `web/app/providers.tsx` already constructs the global QueryClient with `mutations.retry: false` and `queries.retry: false`.
  - The contract layer is complete in `web/lib/payd/types.ts` and `web/lib/payd/schemas.ts`.
  - The task requirements are DAT-001..DAT-010, DAT-020..DAT-026, DAT-030..DAT-036, DAT-040..DAT-044, BFF-022, BFF-030, BFF-032, BFF-033, UI-051, UI-052.
  - Required behavior: manual default tier; live 5s except withdrawal detail 10s; operational 30s; alarms 60s; hidden/offline polling disabled; 429 changes query interval to 60s for 2 minutes; client mutations never retry; client never carries a payd key or server URL.

DO NOT RE-READ OR PRINT THE FULL OPENAPI, AGENTS FILES, OR EXISTING AUTH IMPLEMENTATION. Inspect only the four allowed files and make the minimum complete change.

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/providers.tsx
  web/lib/query-keys.ts
  web/lib/query.ts
  web/lib/payd/browser-client.ts
Everything else belongs to another agent. If you need a change outside this list, STOP and report it instead of making it.

FAILURES:
  Two prior invocations exited during read-only repository exploration with no file changes or final report. This is the final allowed task attempt.

INVARIANTS:
  - Mutations use `retry: false`; never add a mutation retry/backoff/re-send.
  - No client money arithmetic, no balance merging, no client business logic.
  - No API key, TOTP, secret, or backend URL may enter these client files.

DONE WHEN:
  - `npx tsc --noEmit` clean
  - `npm run lint` clean
  - files above satisfy the stated context exactly

YOU MUST NOT:
  - add dependencies, modify backend, commit, push, or change branch.

REPORT AT THE END:
  - files changed
  - requirement ID to file:line
  - validation output
---
