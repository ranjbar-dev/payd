ROLE: PAGE
TASK-ID: 25-polish
GOAL: Close the remaining UI-050..053 (empty/stale states) and UI-073/UI-076 (responsive cards, keyboard/focus) gaps across the already-built pages. Verify, don't rebuild, UI-075 (dark default).

You are working in the repository at C:\Users\root\Desktop\Projects\github\tron-payment-proccesor, on branch web-autopilot.

This is a pass over EXISTING pages, not a new route. Every page below already works;
your job is closing specific, named gaps in it — not redesigning it.

READ FIRST, FULLY:
  web/docs/specs/06-conventions.md §6.6 (UI-050..053, empty/error/stale states),
    §6.8 (UI-070..076, layout/density/dark-mode/keyboard)
  components/data/empty-state.tsx — `kind: "worklist" | "search"` already exists.
    "worklist" renders success styling (green, checkmark) for a genuinely empty
    operational worklist (UI-050's first case); "search" renders neutral styling for
    a filtered list with no matches (UI-050's second case). UI-050's THIRD case — a
    failed load — is a SEPARATE component, `components/data/error-state.tsx`, not a
    third `EmptyState` kind; do not add one.
  components/data/error-state.tsx — already computes a staleness marker
    (`Date.now() - lastUpdatedAt > pollingIntervalMs * 3`, UI-052) and a "Retry read"
    button (UI-051) whenever it is given `lastUpdatedAt`/`pollingIntervalMs` props.
    The gap, where one exists, is a CALL SITE omitting those props or using the wrong
    `EmptyState` kind — not a missing capability in these two components.

YOU MAY MODIFY ONLY THESE PATHS (existing files only; this task creates no new route):
  web/app/(dash)/overview-dashboard.tsx
  web/app/(dash)/alarm-navigation.tsx
  web/app/(dash)/orders-dashboard.tsx
  web/app/(dash)/payments-dashboard.tsx
  web/app/(dash)/payment-worklists.tsx
  web/app/(dash)/payment-attribute.tsx
  web/app/(dash)/addresses-dashboard.tsx
  web/app/(dash)/address-detail.tsx
  web/app/(dash)/withdrawals-dashboard.tsx
  web/app/(dash)/withdrawal-detail.tsx
  web/app/(dash)/withdrawal-resolve.tsx
  web/app/(dash)/withdrawal-needs-operator.tsx
  web/app/(dash)/resources-dashboard.tsx
  web/app/(dash)/resource-purchases.tsx
  web/app/(dash)/resource-grants.tsx
  web/app/(dash)/webhooks-dashboard.tsx
  web/app/(dash)/webhook-dead-letters.tsx
  web/app/(dash)/webhook-consumers.tsx
  web/app/(dash)/reports-dashboard.tsx
  web/app/(dash)/system-*.tsx
  web/components/data/empty-state.tsx, error-state.tsx, data-table.tsx
    (ONLY if you find a genuine capability gap in the shared component itself,
    not to fix a single page's misuse of it — a call-site fix belongs in the page)
Everything else belongs to another agent, including anything under `backend/` or
any withdrawal-wizard/address-delegate/address-clear-drift/export-dialog file — WP3's
audited surface (`22-noretry-audit`) is closed; do not reopen it for a polish pass.
If you need a change outside this list, STOP and report it instead of making it.

REQUIREMENTS TO SATISFY (cite each in your report with file:line):

  UI-050  Three visually distinct renderings for: empty worklist (success),
          empty search/filtered result (neutral), failed load (error, last-good-data
          kept visible). AUDIT every `<EmptyState kind=...>` call site across the
          paths above: a call on a genuinely operational worklist (payments
          unattributed/orphaned, withdrawals needs-operator, webhooks dead-letters,
          orders funded-terminal, addresses needs-resources) that currently passes
          `kind="search"` while empty is WRONG and must become `kind="worklist"`.
          A call on an ordinary filtered list (orders, payments, addresses, audit)
          is correctly `kind="search"` already — do not change those.
  UI-051  Every list/detail page's failed-load path renders `ErrorState`/its
          `ErrorNotice` wrapper with the last good data still visible underneath,
          not a blank page. Audit for any page that returns early on `isError` and
          discards the previously-rendered rows.
  UI-052  Every `ErrorState`/`ErrorNotice` call site passes real
          `lastUpdatedAt`/`pollingIntervalMs` (or the wrapper's equivalent prop
          names — check each page's own `ErrorNotice` signature, several pages
          define a slightly different one) so the staleness marker can actually
          fire. A call site missing either prop silently disables UI-052 for that
          page — find and fix those.
  UI-053  Every `EmptyState`'s `description` says what WOULD put a row there
          (already true almost everywhere per the pages already built — audit for
          any description that only restates the title without saying what
          populates the list).
  UI-073  Below 1024px, tables become stacked cards on: overview, alarm counters,
          order lookup, payment lookup, withdrawal list and detail. TWO CONFIRMED
          GAPS to close, found by the orchestrator before this brief was written:
            - `withdrawals-dashboard.tsx`'s list table has `hidden lg:block` with NO
              matching `lg:hidden` card view — the list is simply invisible below
              1024px today. Add cards following the exact same pattern
              `orders-dashboard.tsx`'s `OrderCards` or `payment-worklists.tsx`'s
              `UnattributedCards`/`OrphanedCards` already use (a `lg:hidden` sibling
              `<div>` of `article` elements, not a new abstraction).
            - `withdrawal-detail.tsx` has NO responsive card treatment of any kind —
              audit it for any table-shaped surface and give it the same treatment.
          Audit `overview-dashboard.tsx`'s worker table and `alarm-navigation.tsx`
          for the same gap — do not assume they are already covered.
          `orders-dashboard.tsx`'s orders list, its payments tab, its webhook-events
          tab, and `payment-worklists.tsx` ALREADY have correct card fallbacks —
          verify only, do not rebuild them.
  UI-075  Dark mode is default and already built (`06-design-tokens`). VERIFY, do
          not rebuild: grep the paths above for any hardcoded Tailwind color utility
          that bypasses the design-token classes already in use everywhere else
          (e.g. a literal `text-slate-950`/`bg-amber-300`/`text-red-500` instead of
          `text-severity-*`/`text-ink*`/`bg-panel` etc.). `scope-banner.tsx` is a
          KNOWN pre-existing offender (`bg-amber-300 text-slate-950`, hardcoded,
          not theme-aware) but is NOT in your allowed paths — report it, do not fix
          it, since it belongs to no page in your list and touching it risks
          colliding with nothing currently in flight, but is still outside your
          scope as written.
  UI-076  Every interactive element (button, link acting as a control, input) is
          keyboard reachable with a visible focus ring, and every `StatusBadge`
          instance has an accessible label. Spot-check `components/data/status-badge`
          usage for a missing `aria-label`/accessible name, and spot-check for any
          `onClick` handler on a non-interactive element (a `<div>` or `<td>`) that
          has no keyboard equivalent — `DataTable`'s `onRowClick` pattern already
          used throughout should already handle this correctly; verify it does
          (check for a `tabIndex`/`role="button"`/`onKeyDown` on the row, or that
          the row click is redundant with an in-row link that IS reachable).

THE SIX INVARIANTS — these override anything you think is a better idea, even though
this task touches no fund-moving control directly:

  INV-1  Do not add a retry control anywhere. A "Retry read" button on a GET
         (already the established pattern via `ErrorState`'s `onRetry`) is fine and
         pre-existing; do not add a NEW automatic or one-click "try again" anywhere,
         and do not touch anything under withdrawal-wizard/address-delegate/
         address-clear-drift/export-dialog (out of scope, see above).
  INV-2  MONEY IS A STRING. If a card view you add renders an amount, use `<Amount>`
         exactly as the table row it mirrors already does — copy the row's own
         rendering, do not reformat it.
  INV-3  `confirmed`/`pending` balances stay separate in any card view exactly as
         they are in the table it mirrors.
  INV-4  No secret reaches the browser. Not directly at stake in this task, but a
         card view must not surface any field the table version doesn't already show.
  INV-5  NO BUSINESS LOGIC IN THE CLIENT. Deciding `kind="worklist"` vs
         `kind="search"` for an EmptyState is not business logic — it's rendering
         which of two already-known, static facts applies (is this route an
         operational worklist or a filtered search) and requires no new server call
         or new client-side computation over data the API returned.
  INV-6  UTC labeling in any card view matches its table row exactly.

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean
  - `npm run build` clean, every route from before this task still present, none removed
  - `npm test` still 4/4
  - every requirement ID above is satisfied and you can point to where, OR explicitly
    marked "already correct, no change needed" with the file:line you checked
  - the mechanical scans below return nothing unexpected:
      grep -rniE "retry|resume|re-?broadcast|try ?again|resend|re-?send" on the files
        you touched — every hit must be the established manual read-refetch pattern
      grep -rnE "Number\(|parseFloat|parseInt|toFixed|toLocaleString" on the files you
        touched — no hit on a money field
      grep -rn "NEXT_PUBLIC_" on the files you touched — none

YOU MUST NOT:
  - add a runtime dependency (WST-001's budget is fixed; this task needs none)
  - modify anything under `backend/`
  - touch withdrawal-wizard.tsx, withdrawal-wizard-steps.tsx, address-delegate.tsx,
    address-clear-drift.tsx, export-dialog.tsx, confirm-dialog.tsx, totp-field.tsx —
    WP3's audited surface, closed and out of scope for a polish pass
  - touch `scope-banner.tsx` (report its hardcoded-color issue, do not fix it — it
    is not in your allowed-paths list)
  - invent a fourth `EmptyState` kind or a new shared component when the existing
    two (`EmptyState`, `ErrorState`) already cover the requirement
  - commit, push, or change git branches
  - resolve a spec ambiguity yourself — report it instead

REPORT AT THE END:
  - files changed
  - each requirement ID → file:line where it is satisfied, OR "already correct" with
    the file:line you verified
  - the two confirmed UI-073 gaps: what you found and fixed
  - anything you could not do, and why
  - any spec ambiguity or contradiction you hit
