---
ROLE: PLATFORM
TASK-ID: 03-auth-foundation
GOAL: Remediate the exact TypeScript failure in the existing combined auth/session/BFF task without changing its security scope.

READ FIRST, FULLY:
  web/.codex/briefs/03-auth-foundation.md
  web/lib/session.test.ts
  web/tsconfig.json

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

FAILURES:
  `rtk proxy npx tsc --noEmit` and `rtk proxy npm run lint` both fail with:
  `lib/session.test.ts(11,86): error TS5097: An import path can only end with a '.ts' extension when 'allowImportingTsExtensions' is enabled.`

REQUIREMENTS TO PRESERVE:
  WST-001, WST-010, WST-011, WST-012, WST-020..WST-023,
  BFF-001..BFF-012, BFF-020..BFF-023, BFF-030..BFF-033,
  AUTH-001..AUTH-033, AUTH-040, AUTH-041, AUTH-050..AUTH-052,
  INV-1..INV-6.

DONE WHEN:
  - `npx tsc --noEmit` clean
  - `npm run lint` clean
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
