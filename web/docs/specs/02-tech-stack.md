# 2. Tech stack, dependencies, project layout

**Part of:** payd admin dashboard specification v1.0
**ID prefixes in this file:** `WST-*`
**Related:** [`03-architecture-and-bff.md`](03-architecture-and-bff.md) (what the server layer does), [`06-conventions.md`](06-conventions.md) (how the components are used)

---

## 2.1 Stack

| Layer | Choice | Why |
|---|---|---|
| Framework | Next.js (App Router) | Already scaffolded. Server route handlers are what makes the BFF possible without a second process |
| Language | TypeScript, `strict: true` | The API surface is 51 routes of amount-strings and nullable timestamps. Types are the cheapest defence |
| Styling | Tailwind CSS | Dense operational tables, no design system to maintain |
| Components | shadcn/ui | Copied into the repo, not a runtime dependency. Table, Dialog, Badge, Toast, Form, Tabs, Popover |
| Server state | TanStack Query | Polling intervals, cache invalidation, and request de-duplication are its entire job; [`05-data-fetching.md`](05-data-fetching.md) depends on it |
| Forms | React Hook Form + Zod | Zod schemas double as the parse boundary for API responses |
| Icons | lucide-react | Ships with shadcn/ui |
| Charts | none in v1 | `/reports/*` returns grouped totals; a table is honest and a chart is not needed. Revisit only if asked |

| ID | Requirement |
|---|---|
| WST-001 | The stack above is the whole dependency budget. Adding a runtime dependency MUST be justified against "can a few lines of the existing stack do this" and recorded in this table |
| WST-002 | TypeScript MUST run in `strict` mode. `any` on an API response type is forbidden; unknown backend fields MUST be typed as `unknown` and narrowed |
| WST-003 | There MUST be no client-side state manager (Redux, Zustand, Jotai). All meaningful state is server state, and TanStack Query owns it. UI-local state uses `useState` |
| WST-004 | There MUST be no ORM, no database driver, and no persistent store in `web/`. See `WNG-004` |
| WST-005 | There MUST be no crypto/web3 library (`tronweb`, `ethers`, `web3`). The dashboard never touches a key or builds a transaction. A dependency capable of signing is a dependency that can be made to sign |
| WST-006 | Date handling MUST use `Intl.DateTimeFormat` and native `Date`. No date library — the backend emits Unix seconds and the two formats needed are "local with UTC tooltip" and "UTC explicit" |
| WST-007 | Money MUST NOT be handled by a decimal library either. Amounts are display strings and are never operated on (`INV-2`) |

## 2.2 Project layout

```
web/
  app/
    (auth)/login/page.tsx           # unauthenticated
    (dash)/
      layout.tsx                    # nav shell, alarm counters, session guard
      page.tsx                      # Overview
      orders/                       # list, [id], funded-terminal
      payments/                     # list, unattributed, orphaned
      addresses/                    # list, [address], needs-resources
      withdrawals/                  # list, [id], new, needs-operator
      resources/
      webhooks/
      reports/
      system/
    api/
      auth/login|logout/route.ts    # session only, never proxied
      payd/[...path]/route.ts       # the proxy (BFF-003)
  lib/
    payd/
      client.ts                     # server-only fetch wrapper
      schemas.ts                    # Zod schemas per endpoint
      types.ts                      # generated/derived from openapi.yaml
    session.ts                      # cookie sign/verify, server-only
    format.ts                       # money, time, address, txid formatters
    query-keys.ts                   # TanStack Query key factory
  components/
    ui/                             # shadcn/ui primitives
    data/                           # DataTable, CursorPager, StatusBadge,
                                    # Amount, Timestamp, AddressLink, TxidLink,
                                    # EmptyState, ErrorState, AlarmCounter
    forms/                          # TotpField, IdempotencyKeyField, ConfirmDialog
  docs/                             # this spec set
  public/
```

| ID | Requirement |
|---|---|
| WST-010 | `lib/payd/` and `lib/session.ts` MUST be server-only. Every file in them MUST carry `import 'server-only'` at the top, so an accidental client import fails at build time rather than shipping the API key to the browser |
| WST-011 | `app/api/payd/[...path]/route.ts` MUST be the only place in the codebase that sets the `X-API-Key` header |
| WST-012 | The `app/(dash)/` route group MUST enforce the session in its `layout.tsx`, so a new page is protected by existing there rather than by remembering to add a guard |
| WST-013 | The scaffolded `app/transactions/` directory MUST be deleted. It maps to no backend concept: money in is `payments`, money out is `withdrawals`, and the two have different tables, lifecycles, and scopes |
| WST-014 | Response types MUST be derived from `backend/internal/api/openapi.yaml`, not hand-written from these docs. When the contract changes, the type MUST break the build |

## 2.3 Configuration

Environment variables, all server-side, none prefixed `NEXT_PUBLIC_`.

| Variable | Purpose |
|---|---|
| `PAYD_BASE_URL` | e.g. `http://127.0.0.1:8080` |
| `PAYD_API_KEY` | The single operator key. Needs all 8 scopes |
| `DASH_PASSWORD_HASH` | Argon2id hash of the dashboard password |
| `DASH_TOTP_SECRET` | Session TOTP secret — **not** payd's |
| `SESSION_SECRET` | Cookie signing key, ≥32 bytes |
| `SESSION_TTL_SECONDS` | Default 28800 (8h) |

| ID | Requirement |
|---|---|
| WST-020 | No secret MAY be exposed via a `NEXT_PUBLIC_` variable. There MUST be no `NEXT_PUBLIC_` variable that names a key, secret, hash, or URL of the backend |
| WST-021 | Startup MUST fail loudly if any variable above is missing or empty, naming the variable. A dashboard that boots without `PAYD_API_KEY` and 401s on every page is a worse failure than not booting |
| WST-022 | `SESSION_SECRET` MUST be rejected at startup if shorter than 32 bytes or equal to any value that appears in the repository, including example files |
| WST-023 | The dashboard MUST verify its key's scopes at startup or first request via `GET /auth/whoami`, and MUST render a persistent banner naming any missing scope rather than failing per-page with an opaque 401 |
