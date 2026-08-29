# DESIGN.md — payd dashboard design system

Canonical brief for every redesign run. Read this **before** touching any UI file.
Derived from the `ui-ux-pro-max` skill (`web/.codex/skills/ui-ux-pro-max`).
Run the skill for any new UI task — see `CLAUDE.md` "UI work".

## Direction

**Dark-mode (OLED) minimalism** for a self-hosted TRON payment processor operator
console. Dense, flat, quiet. One accent colour. No glassmorphism, no heavy
shadows, no gradients, no decorative motion. Every screen is a worklist or a
table — optimise for scanning, not for marketing.

## Non-negotiable invariants (money app — never break these)

- **No retry of any fund-moving action.** No retry button, no auto re-send of a
  failed mutation, no retrying HTTP client. Do not add one during a redesign.
- **Amounts are decimal strings in base units.** Render via `<Amount>`. Never
  `parseFloat`, never client-side arithmetic, never client-side sort of amounts.
- **`confirmed` and `pending` balances are never merged** into one figure.
- Do **not** change data fetching, API calls, `lib/payd/*`, the BFF allowlist,
  Zod schemas, or any business logic. Redesign = markup, classes, icons, tokens.
- Keep every data hook attribute: `data-severity`, `data-financial-value`,
  `data-address`, `data-txid`, `data-entity-id`, `data-count`.
- Keep the skip-link, `:focus-visible` rings, and `prefers-reduced-motion` block.
- Status badges keep their textual `!` / `!!` prefix (survives mono screenshots).

## Tokens (`app/globals.css` — Tailwind v4 `@theme`)

Keep the existing token names. Values below; severity tokens unchanged.

| Token | Value | Use |
|---|---|---|
| `--surface-canvas` | `#050608` | app background (OLED near-black) |
| `--surface-panel` | `#0b0e12` | sidebar, cards, table header |
| `--surface-raised` | `#12171e` | row hover, inputs, secondary buttons |
| `--surface-inset` | `#05070a` | code blocks, wells |
| `--border-subtle` | `#20262f` | default borders, row separators |
| `--border-strong` | `#333c49` | header underline, secondary button border |
| `--text-primary` | `#e8edf2` | body |
| `--text-secondary` | `#a3aebc` | labels, secondary cells |
| `--text-faint` | `#68727f` | meta, column headers, disabled |
| `--focus-ring` | `#84c5ff` | keyboard focus |
| `--accent` | `#f59e0b` | primary action bg, active nav marker |
| `--accent-hover` | `#fbbf24` | primary action hover |
| `--accent-fg` | `#1a1204` | text on `--accent` |
| `--accent-bg` | `#241a06` | subtle accent tint (selected row, active nav bg) |
| severity-* | unchanged | semantic status only — never as entity colour |

Tailwind maps (keep): `bg-canvas panel raised inset`, `border-border-subtle
border-border-strong`, `text-ink ink-secondary ink-faint`, `text-severity-*`,
plus new `bg-accent text-accent-fg bg-accent-bg` and `hover:bg-accent-hover`.

## Typography

- **Body / UI / headings:** Fira Sans. **Data (IDs, amounts, addresses, txids,
  hashes, timestamps):** Fira Code. Load both with `next/font/google` in
  `app/layout.tsx` — **no external `@import`** (CSP + perf). Wire CSS vars
  `--font-sans` / `--font-mono` into the `@theme`.
- Scale (px): `11 12 13 15 18 22`. Body 13. Table cell 12.5–13. Page title 18
  semibold. Section kicker 11 uppercase `tracking-[0.14em] text-ink-faint`.
- `line-height` 1.4 body / 1.2 headings. `font-variant-numeric: tabular-nums` on
  everything numeric (already wired via `:where(.font-mono, [data-financial-value]…)`).

## Spacing

Scale (px): `2 4 6 8 12 16 24 32`. Page padding `p-4 lg:p-6`. Vertical rhythm
`space-y-4`. Card padding `p-4`. Section gap `gap-3`.

## Tables (compact — the core of this app)

- Wrapper `overflow-x-auto`. Keep the existing `lg:hidden` card fallback for mobile.
- Header: `sticky top-0 z-10 bg-panel`, cells `px-3 py-2 text-[11px] font-semibold
  uppercase tracking-wide text-ink-faint`, `border-b border-border-strong`.
- Body cell: `px-3 py-1.5` (row ~32px), `border-b border-border-subtle`.
- Row hover: `hover:bg-raised transition-colors duration-150`.
- Selected / active row: `bg-accent-bg` + `box-shadow: inset 2px 0 0 var(--accent)`.
- Numeric / amount / date columns: `text-right font-mono tabular-nums`.
- No row over 2 visual lines. Secondary info: `text-[11px] text-ink-faint` beneath.
- No zebra striping — borders only. No inner card-in-card.

## Buttons & interactive (cursor + colour change on hover, always)

Base: `inline-flex items-center gap-1.5 h-8 px-3 rounded text-[13px] font-medium
transition-colors duration-150 cursor-pointer disabled:cursor-not-allowed
disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]`.

| Variant | Classes |
|---|---|
| Primary | `bg-accent text-accent-fg hover:bg-accent-hover` |
| Secondary | `border border-border-strong bg-raised text-ink hover:border-ink-faint hover:bg-panel` |
| Ghost | `text-ink-secondary hover:bg-raised hover:text-ink` |
| Danger | `border border-severity-critical text-severity-critical hover:bg-severity-critical-bg` |

- The global `button:not(:disabled), [role="button"], summary { cursor: pointer }`
  rule stays. Every clickable row, link, `<summary>`, tab: `cursor-pointer` + a
  hover colour change (bg / border / text). No `scale` transforms on hover.
- Icon-only button → `aria-label`. Async button → disabled + spinner while pending.

## Icons — `lucide-react` (installed), never emoji

- Sizes: `14` inline in buttons/badges, `16` nav, `20` empty/error states.
  `strokeWidth={1.75}`, `aria-hidden` when decorative.
- Nav items (one each): Overview `LayoutDashboard`, Orders `Receipt`, Payments
  `ArrowDownToLine`, Addresses `Wallet`, Withdrawals `Banknote`, Resources `Zap`,
  Webhooks `Webhook`, Reports `FileBarChart`, System `Server`.
- Action verbs: new `Plus`, export `Download`, refresh `RefreshCw`, resolve
  `Check`, cancel/close `X`, external link `ExternalLink`, copy `Copy`,
  attribute `Link2`, delegate `Share2`, replay `RotateCcw` (UI action, **not** a
  fund retry), filter `Filter`, search `Search`.
- Empty state: a relevant noun icon. Error state: `AlertTriangle` (severity colour).

## Layout

- Sidebar: `w-60` (240px), `bg-panel`, `border-r border-border-subtle`. Nav item:
  `flex items-center gap-2 rounded px-3 py-1.5 text-[13px]`. Idle
  `text-ink-secondary hover:bg-raised hover:text-ink`. Active `bg-accent-bg
  text-ink` + `inset 2px 0 0 var(--accent)` (`aria-current="page"`).
- Main: `pl-60`. Content `mx-auto max-w-7xl p-4 lg:p-6 space-y-4`.
- Page header: kicker (icon + uppercased label) → title row with right-aligned
  action buttons → optional filter bar. Consistent across all 23 pages.
- `z-index` scale only: `10` sticky header, `20` dropdown/popover, `30` drawer,
  `50` modal / toast. No `z-[9999]`.

## Motion

`transition-colors duration-150` on interactive elements. `animate-pulse`
skeletons for loading (never a blank panel). No infinite decorative animation.
`prefers-reduced-motion` already neutralises transitions globally — keep it.

## Per-run checklist

- [ ] Ran / consulted `ui-ux-pro-max` for anything new.
- [ ] Invariants above intact — diff touches no `lib/`, no fetch, no schema.
- [ ] Amounts still `<Amount>`; no sort/arith added.
- [ ] Every button: `cursor-pointer` + hover colour change + focus ring.
- [ ] Icons from lucide, sized per spec, icon-only has `aria-label`.
- [ ] Table rows ≤ 2 lines, header sticky, numerics right + mono.
- [ ] Loading = skeleton; empty = icon + copy; error = `AlertTriangle`.
- [ ] `npm run lint` (tsc) clean; page renders; no console error in Playwright.
