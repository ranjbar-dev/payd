---
ROLE: DESIGN
TASK-ID: 07-components
GOAL: Build the shared dense operations UI components and a reviewable kitchen-sink route, using the existing dark semantic token system.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/06-conventions.md
  web/docs/specs/05-data-fetching.md
  web/docs/specs/04-auth-and-session.md

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/components/data/
  web/components/forms/
  web/app/(dash)/system/components/page.tsx
Everything else belongs to another agent. If you need a change outside this list, STOP and report it instead of making it.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):
  UI-001..UI-008, UI-010..UI-016, UI-020..UI-023, UI-030..UI-035,
  UI-040..UI-044, UI-050..UI-053, UI-060..UI-064, UI-070..UI-076,
  DAT-020..DAT-026, DAT-030..DAT-035, AUTH-042..AUTH-045

THE SIX INVARIANTS — these override anything you think is a better idea:
  INV-1 No retry/resume/rebroadcast/re-send control on withdrawal/grant/delegation paths.
  INV-2 Amounts are strings; never coerce, format numerically, compare, or sort them in client code.
  INV-3 Confirmed and pending balances never merge.
  INV-4 No API key, TOTP code, or secret reaches browser-visible state.
  INV-5 No business logic in the client.
  INV-6 UTC-scoped visible text says UTC.

DESIGN BRIEF:
  Financial operations console, not marketing. Dark default. Dense tables for desktop;
  <1024px cards only where the specification permits. Amounts, addresses, txids, and IDs
  use tabular monospace. Colour is severity only; warning and critical have text/icon signals.
  `needs_operator` is uniquely loud. No decorative motion. Focus rings, labels, and keyboard
  access are mandatory. Empty worklist = success; empty search = neutral; load error preserves
  last data. ConfirmDialog must present API-provided text, block double submits, and permit no
  Enter-key submission on a TOTP-gated form.

DONE WHEN:
  - `npx tsc --noEmit` clean
  - `npm run lint` clean
  - every named component exists: DataTable, CursorPager, StatusBadge, Amount, Timestamp,
    AddressLink, TxidLink, EmptyState, ErrorState, AlarmCounter, ConfirmDialog, TotpField
  - `/system/components` renders examples with no API calls

YOU MUST NOT:
  - add a dependency
  - modify backend or page routes outside the permitted kitchen-sink page
  - implement a retry/re-send control or client business/money logic
  - commit, push, or change branch
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line
  - validation results
  - unresolved issue/spec ambiguity
---
