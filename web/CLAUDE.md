# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Next.js operator dashboard for the payd payment processor. Root context:
[`../CLAUDE.md`](../CLAUDE.md). Thin client over the backend REST API — **no
business logic here**; the backend decides, this app renders and submits.

## Read before coding

1. `docs/index.md` — 17 numbered specs with `WEB-*` requirement IDs, one per
   page/subsystem. Open the matching file, not the set. Cite IDs in commits.
2. Anything touching withdrawals → `docs/specs/11-withdrawals.md` §11.0, always.
3. Adding a call to payd → `docs/specs/03-architecture-and-bff.md` +
   `05-data-fetching.md`.
4. Rendering an amount / timestamp / status → `docs/specs/06-conventions.md`.
5. Building a page → its page spec (`docs/specs/07`–`15`). What to build next →
   `docs/specs/16-implementation-phases.md`.

Don't duplicate validation or business rules that belong in the backend — this
app renders and submits, the backend decides.

## UI work — mandatory

Any task that adds or changes UI (a page, component, table, form, button, modal,
layout, styling, icon, colour, spacing, hover/focus state):

1. **Invoke the `ui-ux-pro-max` skill first** (`web/.codex/skills/ui-ux-pro-max`
   — run `scripts/search.py … --design-system`, then domain/stack searches).
   Not optional, even for "small" tweaks.
2. **Follow [`DESIGN.md`](DESIGN.md)** — the canonical design system (dark-mode
   OLED minimalism, compact tables, single amber accent, Fira Sans + Fira Code,
   lucide icons, button/hover spec). Its "Non-negotiable invariants" and
   "Per-run checklist" are binding.
3. Tailwind **v4** (CSS-first `@theme` in `globals.css`, `@tailwindcss/postcss`).
4. Every button/clickable: `cursor-pointer` + a hover colour change + focus ring.
   Icons from `lucide-react` (never emoji); icon-only buttons get `aria-label`.
5. Redesign changes markup / classes / icons / tokens only — never data
   fetching, API calls, `lib/payd/*`, schemas, or business logic.

### Next.js version notice

This is Next.js 16 — APIs, conventions, and file structure differ from older
training data. Read the relevant guide in `node_modules/next/dist/docs/`
(resolved from `web/`) before writing App Router code. Heed deprecation
notices.

## Commands

```bash
npm run dev            # next dev, http://localhost:3000
npm run build          # next build (prebuild regenerates the payd allowlist)
npm run lint           # tsc --noEmit  (also: npm run typecheck)
npm test               # node --test on the guardrail suite (see below)
npm run test:session   # session-cookie tests

# dashboard TOTP for local login
node scripts/dev-auth.mjs totp "$(grep '^DASH_TOTP_SECRET=' .env.local | cut -d= -f2)"
```

Stack: Next.js 16 (App Router), React 19, TanStack Query v5, react-hook-form +
Zod, Tailwind v3. `npm test` runs a fixed set of guardrail tests
(`lib/no-coercion`, `lib/proxy-no-retry`, `lib/session-expiry`) — add new test
files to the `test` script in `package.json` explicitly.

## Architecture

- **BFF proxy** — every call to payd goes through `app/api/payd/[...path]/route.ts`,
  the only place that sets `X-API-Key`. `PAYD_API_KEY` never reaches the
  browser. Allowed paths come from `lib/payd/allowlist.ts` (regenerated at
  prebuild by `scripts/generate-payd-allowlist.mjs`).
- **API layer** — `lib/payd/`: `client.ts` (server), `browser-client.ts`
  (client, via proxy), `schemas.ts` / `types.ts` (Zod), `query-keys.ts`,
  `query.ts` (TanStack config).
- **Routes** — `app/(auth)/` login, `app/(dash)/` the dashboard. Page shells
  live in `*/page.tsx`; the many sibling `*.tsx` files in `(dash)/` are that
  page's client components (forms, drawers, dashboards).
- **Session** — `lib/session.ts`, signed cookie, `SESSION_SECRET` /
  `SESSION_TTL_SECONDS`. Expiry handled by `app/session-expiry.tsx`.
- **Shared UI** — `components/ui/` (data-table, status-badge, amount,
  error-state, empty-state, cursor-pager…), `components/forms/`.

## Invariants

- **No retry on any withdrawal path** — no retry button, no auto re-send of a
  failed mutation, no HTTP client that re-attempts a POST. Backend never
  retries a fund-moving action (`WDR-000`); the UI must not undo that.
- **All money amounts are decimal strings.** Never parse to float, never do
  arithmetic, never sort client-side. Render as strings (`components/ui/amount.tsx`).
- **`confirmed` and `pending` balances are never merged** into one figure.
- Don't duplicate backend validation. An API contract change updates
  `backend/internal/api/openapi.yaml`, the client here, and
  `docs/specs/17-api-coverage-matrix.md`.
- The `<!-- ...nextjs-agent-rules... -->` block in `AGENTS.md` is re-added by
  `next dev`; commit it with your work rather than fighting the diff.
