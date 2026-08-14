# 6. UI conventions and design system

**Part of:** payd admin dashboard specification v1.0
**ID prefixes in this file:** `UI-*`
**Related:** every page spec depends on this file. Backend `DB-001` (amounts), `DB-002a` (UTC days), `API-014` (confirmed vs pending)

---

## 6.1 Money

| ID | Requirement |
|---|---|
| UI-001 | **An amount MUST be rendered as the string the backend sent.** No `Number()`, `parseFloat`, `+`, `-`, `toFixed`, `toLocaleString`, or comparison operator MAY be applied to any amount field, anywhere, including sorting, filtering, and "is this zero" checks. TRX has 6 decimals and USDT amounts routinely exceed what a double represents exactly; one rounding produces a number that disagrees with the ledger (`INV-2`) |
| UI-002 | "Is this zero" MUST be tested against the string form (`=== "0"` or an all-zero-digits test), not by numeric coercion |
| UI-003 | Fields ending `_raw` are base units (sun for TRX, 6-decimal units for USDT) and MUST NOT be shown to the operator. Every list and detail MUST use the whole-unit decimal string the API provides alongside it |
| UI-004 | **`confirmed` and `pending` MUST always appear as two figures with two labels.** No sum, no "total balance", no single number. Pending funds are reorg-reversible and unspendable (backend `WDR-005`), and a merged figure invites a withdrawal that will be rejected (`INV-3`) |
| UI-005 | An `<Amount>` component MUST be the only way amounts are rendered. It takes the string, the asset, and a variant, and applies monospace tabular figures with the asset symbol |
| UI-006 | A USD figure MUST be labelled with its source: `amount_usd` on an order is an immutable snapshot at creation; a USD figure on a balance is a live conversion. Conflating them makes a historical order look mispriced when the market moves |
| UI-007 | A USD figure the backend omitted MUST render as "—" with a tooltip explaining that no fresh price was available. It MUST NOT be computed client-side from `/prices` (backend `API-012`: an omitted figure is deliberate) |
| UI-008 | Sorting a column of amounts MUST be done by the backend or not offered. Client-side amount sorting requires numeric comparison, which `UI-001` forbids |

## 6.2 Time

| ID | Requirement |
|---|---|
| UI-010 | **Anything scoped to a UTC day MUST be labelled UTC in the UI text itself**, not only in a tooltip: the daily withdrawal limit ("resets at 00:00 UTC"), volume report day grouping, and quota history days. Backend `DB-002a` makes these UTC boundaries; an operator in UTC+3:30 reading an unlabelled "today" is reading a different day (`INV-6`) |
| UI-011 | Every other timestamp MUST render in the browser's local zone, with the full UTC value in a `title` tooltip |
| UI-012 | Timestamps within the last hour MUST render as relative ("4m ago") with the absolute value in the tooltip. Older ones render absolute. Relative time is what makes a stalled worker or a stuck withdrawal visible at a glance |
| UI-013 | A `<Timestamp>` component MUST be the only way times are rendered. It takes Unix seconds and a variant |
| UI-014 | A null timestamp MUST render "—", never "1 Jan 1970" and never the current time |
| UI-015 | The nav footer MUST display the current UTC time, so the operator can always see what the backend means by "today" |
| UI-016 | Durations from the backend (`seconds_since_tick`, `lag_seconds`, `duration_seconds`) MUST render as humanised durations ("2m 14s"), not raw seconds |

## 6.3 Status badges

One vocabulary, used identically everywhere. Colour carries severity, never
identity — the label is always present.

| Severity | Meaning | Statuses |
|---|---|---|
| **Neutral** | Expected, no action | order `pending`, payment `seen`, address `free`/`assigned`/`cooling`, IPN `pending`, withdrawal `requested` |
| **Progress** | In flight, will resolve itself | order `partial`, withdrawal `awaiting_resources`/`awaiting_energy`/`signing`/`broadcast`, energy purchase `quoted`/`purchased` |
| **Success** | Terminal, good | order `paid`/`confirmed`, payment `confirmed`, withdrawal `confirmed`, IPN `delivered`, purchase `delegated` |
| **Muted** | Terminal, no money involved | order `expired`/`cancelled`, address `disabled`, purchase `expired` |
| **Warning** | Terminal, money unresolved — needs a human | order `expired_funded`/`cancelled_funded` with `resolution: null`, payment `unattributed`/`orphaned`, IPN `dead`, withdrawal `rejected`/`failed` |
| **Critical** | Money in an unknown state | withdrawal `needs_operator`, address `drift_detected` |

| ID | Requirement |
|---|---|
| UI-020 | Status MUST render through one `<StatusBadge>` component with the mapping above. A status string with no mapping MUST render as raw text in the neutral style rather than crashing or being hidden — a new backend status must be visible, not invisible |
| UI-021 | Colour MUST NOT be the only signal. Warning and critical MUST also carry an icon, for colour-blind operators and for greyscale screenshots in incident reports |
| UI-022 | `rejected` and `failed` MUST NOT be styled identically to each other in text. Backend `WDR-002b` distinguishes them precisely — `rejected` means nothing was attempted on chain, `failed` means it was attempted and confirmed absent — and the tooltip MUST state that difference |
| UI-023 | A funded terminal order (`expired_funded`/`cancelled_funded`) whose `resolution` is set MUST drop from Warning to Muted. The money was dealt with; keeping it loud trains the operator to ignore the colour |

## 6.4 Addresses, txids, ids

| ID | Requirement |
|---|---|
| UI-030 | A TRON address MUST render truncated (`TXYZab…8j9K`) with a copy button and the full value in the tooltip, via `<AddressLink>`. The full value MUST be what the copy button copies |
| UI-031 | An address MUST link to its detail page inside the dashboard, not to a block explorer. The dashboard knows more about it than Tronscan does |
| UI-032 | A txid MUST render truncated with a copy button and an explicit **external** link to Tronscan, marked with an external-link icon. Backend `WDR-026` expects the operator to check Tronscan directly for `needs_operator`, so this link is functional, not decorative |
| UI-033 | The Tronscan base URL MUST be configurable, since mainnet and Nile testnet differ. A hardcoded mainnet link on a testnet deployment sends the operator to a "not found" page and invites the conclusion that the transaction failed |
| UI-033a | It MUST come from the server-only `TRONSCAN_BASE_URL` (`WST-020a`), read in a server component and provided to the client tree through one React context. It MUST NOT be a `NEXT_PUBLIC_` variable, and no component MAY carry its own fallback literal — a per-component default is how one page keeps linking to mainnet after the deployment moves to Nile |
| UI-034 | ULIDs (order, withdrawal, IPN event ids) MUST render truncated with copy, and MUST be searchable in full |
| UI-035 | Every entity detail page MUST show the full, untruncated id somewhere selectable |

## 6.5 Tables

| ID | Requirement |
|---|---|
| UI-040 | All lists MUST use one `<DataTable>`: sticky header, dense rows, zebra-free, horizontal scroll rather than column hiding at width |
| UI-041 | Filters MUST sit above the table, reflect the URL (`DAT-026`), and show a clear-all control when any is active |
| UI-042 | Row click MUST open the entity's detail. Row-level action buttons MUST be reserved for worklists, where the action *is* the point |
| UI-043 | Every table MUST specify its default sort explicitly. Where the backend defines an order (audit is newest-first, orders are newest-first), the UI MUST NOT re-sort client-side |
| UI-044 | A loading table MUST render skeleton rows at the expected row height, not a spinner that collapses the layout and shifts every control |

## 6.6 Empty, error, and stale states

| ID | Requirement |
|---|---|
| UI-050 | An empty worklist MUST render as an explicit success state ("No unattributed payments"), visually distinct from an empty search result ("No payments match these filters") and from a failed load. Three different meanings, three different renderings |
| UI-051 | A failed load MUST keep the last good data visible with a staleness marker and a retry control, rather than blanking the page (`DAT-035`) |
| UI-052 | Any data older than 3× its polling interval MUST show a staleness marker with the age. Silent staleness on a balance screen is how an operator acts on a figure from ten minutes ago |
| UI-053 | Every empty state MUST say what would put a row there, so an operator can tell "nothing is wrong" from "this is not wired up" |

## 6.7 Destructive and money-moving actions

| ID | Requirement |
|---|---|
| UI-060 | Any action that moves funds, changes an order's terminal state, or records an irreversible decision MUST use one shared `<ConfirmDialog>` that restates the exact parameters — amounts, addresses, asset — read from the API response, not from the form inputs |
| UI-061 | The confirm button MUST name the action ("Withdraw 100.00 USDT"), never "OK" or "Confirm" |
| UI-062 | The confirm button MUST be disabled until every required field, including the payd TOTP code, is complete, and MUST disable itself on submit until the response arrives, so a double-click cannot double-submit |
| UI-063 | **There MUST be no keyboard shortcut, and no Enter-key submission, on any TOTP-gated form.** These actions are deliberate by design |
| UI-064 | After a mutation whose outcome is unknown (timeout, 502, 504), the UI MUST NOT offer "try again". It MUST direct the operator to check the entity's current state first. See [`11-withdrawals.md`](11-withdrawals.md) `WWD-034` |

## 6.8 Layout and responsive

| ID | Requirement |
|---|---|
| UI-070 | Layout is a fixed left navigation plus a content area, designed for ≥1280px |
| UI-071 | The navigation MUST show the four alarm counters at all times, on every page, with the critical one (`needs_operator`) styled distinctly from the rest |
| UI-072 | A zero counter MUST render as a quiet zero, not be hidden. An absent counter is indistinguishable from a broken query |
| UI-073 | Below 1024px, tables MUST become stacked cards for read-only surfaces: overview, alarm counters, order lookup, payment lookup, withdrawal list and detail |
| UI-074 | **The withdrawal creation wizard MUST NOT be usable below 1024px.** It MUST render an explanation instead. Entering an address, an amount, and a single-use code on a phone, under time pressure, into a form that cannot be undone, is not a flow worth supporting |
| UI-075 | Dark mode MUST be supported and MUST be the default. This is an operations tool that gets opened at 3am |
| UI-076 | Every interactive element MUST be keyboard reachable with a visible focus ring, and every status badge MUST have an accessible label |
