---
ROLE: DESIGN
TASK-ID: 06-design-tokens
GOAL: Establish the dark-default, dense financial-operations visual tokens, typography, and six-level severity system.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/06-conventions.md
  web/docs/specs/02-tech-stack.md

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/globals.css
  web/app/layout.tsx
  web/tailwind.config.ts
Everything else belongs to another agent. If you need a change outside this list, STOP and report it instead of making it.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  UI-020, UI-021, UI-022, UI-023, UI-070, UI-071, UI-072, UI-073, UI-075, UI-076

THE SIX INVARIANTS — these override anything you think is a better idea:

  INV-1  NO RETRY CONTROL ANYWHERE IN THE WITHDRAWAL PATH.
  INV-2  MONEY IS A STRING, START TO FINISH. No numeric amount coercion.
  INV-3  `confirmed` AND `pending` BALANCES ARE NEVER MERGED.
  INV-4  NO PAYD API KEY, TOTP CODE, OR SECRET REACHES THE BROWSER.
  INV-5  NO BUSINESS LOGIC IN THE CLIENT.
  INV-6  ANYTHING SCOPED TO A UTC DAY IS LABELLED UTC IN VISIBLE TEXT.

DESIGN BRIEF — apply verbatim:

This is a financial operations console, not a marketing site. Target: Linear's
density and keyboard discipline, Stripe's clarity about money, a terminal's
honesty about state.

  - Dark mode is the DEFAULT (UI-075). This gets opened at 3am during an incident.
  - Density over whitespace. An operator scanning 200 payments needs rows, not cards. Cards are the <1024px fallback only (UI-073).
  - Tabular figures and monospace for every amount, address, txid, and id. Columns of numbers align on the decimal point.
  - Colour carries severity, never identity. The six-level vocabulary in UI-020 is the WHOLE palette: neutral, progress, success, muted, warning, critical. Warning and critical also carry an icon — colour is never the only signal (UI-021).
  - `needs_operator` is the single loudest thing in the entire interface. It means money is in an unknown state. Visually distinct from every other warning, everywhere it appears (UI-071, WWD-011).
  - No decorative motion. Transitions show causality — a row entering, a state changing — and nothing else. No shimmer outlasting the request, no spinner that collapses layout (UI-044).
  - Every destructive or fund-moving confirmation reads its text from the API response, not from the form inputs (UI-060).
  - Empty states are three different things and must look different: an empty worklist is SUCCESS, an empty search is NEUTRAL, a failed load is an ERROR that keeps the last good data visible (UI-050, UI-051).
  - Keyboard reachable, visible focus rings, labelled badges (UI-076).

FRONTEND-DESIGN SKILL DIRECTION:
  Use a restrained industrial/utilitarian dark aesthetic, not a generic SaaS gradient.
  Prefer precise CSS variables, sharp hierarchy, compact grids, and system/monospace
  fonts already available. Do not add fonts, images, animation libraries, or dependencies.

DONE WHEN:
  - `npx tsc --noEmit` clean
  - `npm run lint` clean
  - every requirement ID above is satisfied and you can point to where
  - the root HTML defaults to dark, global focus visibility and tabular figures exist, all six severity semantic tokens exist, and critical has a distinct high-salience treatment token

YOU MUST NOT:
  - add a runtime dependency
  - modify anything under `backend/`
  - add decorative motion or a light-default theme
  - implement page-specific UI components or business logic
  - commit, push, or change git branches
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line where it is satisfied
  - anything you could not do, and why
  - any spec ambiguity or contradiction you hit
---
