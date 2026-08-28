---
ROLE: PLATFORM
TASK-ID: 03-auth-foundation
GOAL: Make the required native Node session self-check runnable while preserving strict TypeScript and all combined auth/BFF security requirements.

READ FIRST, FULLY:
  web/.codex/briefs/03-auth-foundation.md
  web/lib/session.test.ts
  web/tsconfig.json
  web/package.json

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
  web/tsconfig.json
Everything else belongs to another agent. If you need a change outside this list, STOP and report it instead of making it.

FAILURES:
  `npm run test:session` fails after remediation 1:
  `Error [ERR_MODULE_NOT_FOUND]: Cannot find module '.../web/lib/session' imported from .../web/lib/session.test.ts`
  The previous `.ts` import form was rejected by TypeScript with TS5097. Resolve both the native Node test runner and `npx tsc --noEmit` without adding dependencies.

REQUIREMENTS TO PRESERVE:
  WST-001, WST-010, WST-011, WST-012, WST-020..WST-023,
  BFF-001..BFF-012, BFF-020..BFF-023, BFF-030..BFF-033,
  AUTH-001..AUTH-033, AUTH-040, AUTH-041, AUTH-050..AUTH-052,
  INV-1..INV-6.

DONE WHEN:
  - `npx tsc --noEmit` clean
  - `npm run lint` clean
  - `npm run test:session` clean
  - no security requirement above is weakened

YOU MUST NOT:
  - add a runtime dependency
  - modify anything under `backend/`
  - commit, push, or change branches
  - add an unauthenticated proxy exception or set `X-API-Key` outside `app/api/payd/[...path]/route.ts`

REPORT AT THE END:
  - files changed
  - exact validation output
  - anything unresolved
---
