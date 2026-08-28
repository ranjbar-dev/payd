---
ROLE: PLATFORM
TASK-ID: 05-query
GOAL: Complete the minimal shared query layer using the already-generated contract types, after the first worker exited before making a change.

READ FIRST, FULLY:
  web/.codex/briefs/05-query.md
  web/app/providers.tsx
  web/lib/payd/types.ts
  web/lib/payd/schemas.ts
  web/docs/specs/05-data-fetching.md

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/providers.tsx
  web/lib/query-keys.ts
  web/lib/query.ts
  web/lib/payd/browser-client.ts
Everything else belongs to another agent. If you need a change outside this list, STOP and report it instead of making it.

FAILURES:
  The first task invocation exited while reading the OpenAPI with no changed allowed file and no final report. Do not reprint or scan the entire OpenAPI: use the completed `lib/payd/types.ts` and `lib/payd/schemas.ts` as the contract layer for this task.

REQUIREMENTS TO PRESERVE AND SATISFY:
  DAT-001..DAT-010, DAT-020..DAT-026, DAT-030..DAT-036, DAT-040..DAT-044,
  BFF-022, BFF-030, BFF-032, BFF-033, UI-051, UI-052, INV-1..INV-6.

DONE WHEN:
  - `npx tsc --noEmit` clean
  - `npm run lint` clean
  - default query tier is D; every named tier has the specified polling rules and 429 backoff; global mutation retry remains exactly false; no money arithmetic or secret client code exists

YOU MUST NOT:
  - add a runtime dependency
  - modify anything under `backend/`
  - write a retry, backoff, or re-send on a mutation path
  - commit, push, or change branches

REPORT AT THE END:
  - files changed
  - requirement IDs with file:line
  - exact validation output
  - anything unresolved
---
