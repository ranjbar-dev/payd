# 11. Withdrawals (`/withdrawals`)

**Part of:** payd admin dashboard specification v1.0
**ID prefixes in this file:** `WWD-*`
**Consumes:** `GET /withdrawals`, `GET /withdrawals/{id}`, `GET /withdrawals/limits`, `POST /withdrawals/estimate`, `POST /withdrawals`, `POST /withdrawals/{id}/resolve`
**Related:** backend [`13-withdrawal-engine.md`](../../../backend/docs/specs/13-withdrawal-engine.md) — **read its §13.0 before writing any code on this page** — plus `API-015`–`API-017`, `API-031`, `API-032`

**This is the highest-stakes file in the dashboard spec set.** §11.0 overrides
any general UI rule stated elsewhere in this document set.

---

## 11.0 No-retry policy in the UI

The backend never retries a fund-moving action. That guarantee is only as
strong as the interface in front of it: a "Retry" button, an automatic refetch
of a failed mutation, or an HTTP client with a default retry policy each
re-introduce the double-payout the backend was built to prevent.

| ID | Requirement |
|---|---|
| WWD-001 | **There MUST be no control anywhere in this dashboard that retries, resumes, re-broadcasts, or re-signs an existing withdrawal.** No button, no menu item, no keyboard shortcut, no link. The backend exposes no such endpoint (backend `API-015`/`WDR-000c`) and the UI MUST NOT invent one |
| WWD-002 | **No withdrawal mutation MAY be automatically re-sent by the client for any reason** — timeout, network error, 5xx, or 429. Mutations MUST be configured `retry: false` at the query-client level, not per call site (`DAT-034`, `BFF-020`) |
| WWD-003 | A failed, rejected, or `needs_operator` withdrawal MUST render its terminal reason and **no action that moves money**. The only permitted action is recording a decision (`WWD-040`) |
| WWD-004 | Where a new payout is genuinely required, the UI MAY offer "Create a new withdrawal", which opens the wizard **empty of an idempotency key** and requires the operator to pass through estimate and confirmation again. It MUST be visually distinct from a retry and MUST be labelled as a new, separate movement of funds |
| WWD-005 | The `Idempotency-Key` MUST be generated once per wizard completion and MUST NOT be regenerated on a failed submission or reused across submissions. Reuse with different parameters returns 409 `idempotency_key_reuse` (backend `WDR-003a`), which is a caller bug, not a retry |
| WWD-006 | On an ambiguous submission outcome (timeout, 502, 504, connection reset), the UI MUST show the ambiguous-outcome panel (`WWD-034`) and MUST NOT show a retry affordance, a "resend" link, or an auto-refreshing error toast |
| WWD-007 | Any code review touching this page MUST verify `WWD-001`–`WWD-006`. This is the one page where a well-intentioned resilience improvement is a defect |

## 11.1 Status vocabulary

```
requested → awaiting_resources → awaiting_energy → signing → broadcast → confirmed
rejected         : refused before any on-chain action was attempted
failed           : attempted, and the transaction was confirmed absent from chain
needs_operator   : outcome could not be determined automatically
```

| ID | Requirement |
|---|---|
| WWD-010 | `rejected` and `failed` MUST be rendered with distinct labels and distinct explanatory text. Backend `WDR-002b` separates them precisely: `rejected` means no funds could have moved; `failed` means a broadcast was attempted and the chain was checked. An operator who reads them as synonyms will treat a `failed` as safely repeatable |
| WWD-011 | `needs_operator` MUST render at critical severity everywhere it appears, including in lists |
| WWD-012 | The progress states (`awaiting_resources`, `awaiting_energy`, `signing`, `broadcast`) MUST show elapsed time in state. `awaiting_energy` is bounded by `energy.poll_timeout` and normally takes up to 90 seconds (backend `WDR-009g`); a withdrawal sitting there for ten minutes is a fault |
| WWD-013 | The UI MUST NOT display a progress percentage or an ETA. The engine does not provide one and a fabricated one invites an operator to intervene |

## 11.2 List

| ID | Requirement |
|---|---|
| WWD-020 | Columns: id, status, asset, amount, USD, from, to, txid, energy source, bandwidth source, total cost, created, confirmed |
| WWD-021 | Filters: status, and the same filters the CSV export uses, so a list view and its export always agree (backend `API-046`) |
| WWD-022 | `needs_operator` rows MUST be pinned above all others regardless of sort |
| WWD-023 | Total cost MUST be rendered from the backend's own figure — network fee plus `energy_cost_trx` plus bandwidth cost (backend `WDR-025`). It MUST NOT be summed client-side from three fields (`UI-001`) |
| WWD-024 | List polling is tier B, but at **30s against the 10 req/min withdrawal cap** (`DAT-006`). No faster |
| WWD-025 | The daily limit meter from `GET /withdrawals/limits` MUST appear above the list: used, remaining, and cap, labelled **UTC** with its reset time (`UI-010`, backend `WDR-006`/`DB-002a`) |
| WWD-026 | The meter MUST state that the allowance counts withdrawals in `requested`, `awaiting_resources`, `awaiting_energy`, `signing`, `broadcast`, and `confirmed` — in-flight withdrawals consume the cap. Backend `API-016` computes it over the same set that gets enforced, and an operator who assumes only confirmed payouts count will be surprised by a 4xx |

## 11.3 Detail

| ID | Requirement |
|---|---|
| WWD-030 | Detail MUST render every field backend `API-017` guarantees: `status`, `failure_reason`, `txid`, `resolved_by`, and the raw `broadcast_response`. The requirement exists so any terminal outcome is explicable without a chain lookup, and the UI MUST NOT hide any of them behind a "show details" toggle |
| WWD-031 | `broadcast_response` MUST be rendered as raw, selectable, monospace text. It is the node's own words and the operator may need to quote it |
| WWD-032 | `resolved_by` MUST be rendered with its meaning: `chain_lookup` (the engine found the transaction), `expiration` (confirmed absent past expiry), `operator` (a human recorded the outcome) |
| WWD-033 | `txid` MUST always be shown when present, with a Tronscan link, **including for `failed` and `needs_operator`**. The txid is persisted before broadcast (backend `WDR-015`) precisely so an ambiguous outcome is checkable, and backend `WDR-026` expects the operator to use it |
| WWD-034 | **Ambiguous-outcome panel.** When a withdrawal is `needs_operator`, or when the submission itself timed out, the UI MUST render a fixed panel: (1) the funds may or may not have moved; (2) the txid, with a Tronscan link, as the way to find out; (3) the last lookup error; (4) that the service will not attempt anything further; (5) that recording an outcome is a decision record, not an action. It MUST contain no control that submits a transaction |
| WWD-035 | The resource breakdown MUST show `energy_source` (`existing` \| `rented` \| `self_delegated` \| `burned`), `energy_cost_trx`, `energy_used`, `bandwidth_source` (`free` \| `topup` \| `delegated` \| `burned`), and `fee_raw`, each labelled |
| WWD-036 | A `burned` energy source MUST be visually marked as the expensive path. Backend `OPS-004` exists because a silently failing provider shows up as rising burn cost, and the detail page is where that becomes visible per withdrawal |
| WWD-037 | Detail MUST link to the energy purchase record and any resource grants for this withdrawal ([`12`](12-resources-and-energy.md)) |
| WWD-038 | Detail MUST link to the outbound payment ledger row (backend `WDR-023`) and to the source address |
| WWD-039 | Tier A polling at **10s** while non-terminal, per `DAT-006`. On any terminal status, polling MUST stop entirely |

## 11.4 Resolve (`needs_operator` only)

| ID | Requirement |
|---|---|
| WWD-040 | Resolve MUST be offered **only** for withdrawals in `needs_operator`, matching backend `API-031` |
| WWD-041 | The dialog MUST require the payd TOTP code in `X-TOTP`, and MUST submit a body containing exactly `{"outcome": "confirmed"|"failed", "failure_reason": "..."}`. A TOTP in the body returns 400 `totp_in_body` (backend `API-022a`) |
| WWD-042 | The dialog MUST state, as its first line, that **this records what happened; it does not sign, broadcast, retry, or resume anything** (backend `API-031`) |
| WWD-043 | The dialog MUST require the operator to confirm they have checked the txid on Tronscan, with the link present in the dialog. Recording `failed` for a transaction that actually confirmed produces a double payout the moment the operator creates a replacement withdrawal |
| WWD-044 | `failure_reason` MUST be required and non-empty for `outcome: "failed"` |
| WWD-045 | The dialog MUST show the persisted txid and the last lookup error inline (backend `WDR-026`) |
| WWD-046 | After resolution the record MUST show `resolved_by: operator`, and the preserved txid MUST remain visible (backend `API-031` preserves it deliberately) |
| WWD-047 | Resolving MUST NOT offer, suggest, or link directly to creating a replacement withdrawal in the same flow. If a replacement is needed it is a separate, later, deliberate decision (`WWD-004`) |

## 11.5 Create wizard (`/withdrawals/new`)

Three steps, in order, with no way to skip step 1.

```
 1. Compose  →  2. Estimate (POST /withdrawals/estimate)  →  3. Confirm + payd TOTP
```

### Step 1 — compose

| ID | Requirement |
|---|---|
| WWD-050 | Source address MUST be chosen from `GET /wallets/with-balance` — only addresses holding **confirmed** funds. Free text MUST NOT be accepted for the source |
| WWD-051 | The source selector MUST show confirmed and pending separately per asset, and MUST show `can_withdraw` and `blocked_by` per asset. An address blocked on bandwidth MUST be selectable but marked, so the operator learns why rather than wondering where it went |
| WWD-052 | Destination MUST be free text with a copy-paste-friendly field, an explicit paste confirmation, and a truncation-free display of the full value before submission. The backend validates it (`WDR-004`); the UI MUST NOT pre-validate with its own address library (`WST-005`) |
| WWD-053 | The destination field MUST warn if the address matches a known pooled deposit address, since withdrawing to one's own pool is almost always a mistake |
| WWD-054 | Amount MUST be a string-preserving text input, validated for precision against the asset's decimals from `GET /assets` |
| WWD-055 | There MUST be no "max" or percentage button. Computing it requires arithmetic on money (`UI-001`), and the resulting figure would exclude the TRX needed for resources anyway |
| WWD-056 | The composer MUST show the remaining daily allowance from `GET /withdrawals/limits`, labelled UTC |
| WWD-057 | Below 1024px the wizard MUST refuse to render, per `UI-074` |

### Step 2 — estimate

Backend `API-032`: zero state writes, no TOTP, a safe preflight.

| ID | Requirement |
|---|---|
| WWD-060 | The estimate step MUST be mandatory. There MUST be no path from compose to submit that skips it |
| WWD-061 | The estimate MUST render `projected_energy_source` (`existing` \| `rented` \| `self_delegated` \| `burned` \| unknown), the projected TRX cost from live chain parameters, daily-cap status, and `can_proceed` |
| WWD-062 | **`confirmed_balance_sufficient` and `trx_for_resources_sufficient` MUST be rendered as two separate, separately labelled verdicts.** A TRC-20 transfer spends two balances on the source address and the remedies differ — deposit more of the asset, versus top the address up with TRX. Backend `API-032` splits them because collapsing them told operators the balance was short while the asset balance sat well above the request, sending them to top up the wrong one |
| WWD-063 | Every `blocked_by` entry MUST render as specific text with a specific next step: `withdrawals_disabled`, `confirmed_balance`, `trx_for_resources`, `daily_usd_cap`, `energy_unavailable`, `energy_burn_limit`, `chain_parameters_unavailable`. A raw enum value on a blocked payout screen is not an answer |
| WWD-064 | `chain_parameters_unavailable` MUST explain that the service has not read `getEnergyFee` yet and holds withdrawals rather than assuming a price (backend `RES-022`), linking to the chain params card |
| WWD-065 | `energy_burn_limit` MUST show the configured `energy.max_burn_trx` against the live computed burn cost, since backend `ENR-017` describes a misconfigured ceiling silently disabling the fallback of last resort |
| WWD-066 | `can_proceed: false` MUST disable step 3 entirely. The operator MUST NOT be able to submit a withdrawal the backend has already said is blocked |
| WWD-067 | The estimate MUST be re-run automatically if the operator returns to step 1 and changes anything. A stale estimate against changed parameters is worse than none |
| WWD-068 | The estimate MUST state that it is a projection, that no state was written, and that conditions may change before signing |

### Step 3 — confirm and submit

| ID | Requirement |
|---|---|
| WWD-070 | The confirmation MUST restate source, destination, asset, amount, projected energy source, and projected cost, read from the estimate response rather than from the form inputs (`UI-060`) |
| WWD-071 | The full destination address MUST be shown untruncated, in monospace, for visual verification |
| WWD-072 | The payd TOTP code MUST be entered here, at the moment of submission, in a `TotpField` (`AUTH-042`) |
| WWD-073 | The submit button MUST be labelled with the action and amount, e.g. "Withdraw 100.00 USDT", and MUST disable on click until the response arrives (`UI-061`, `UI-062`) |
| WWD-074 | There MUST be no Enter-key submission and no keyboard shortcut (`UI-063`) |
| WWD-075 | The `Idempotency-Key` MUST be generated client-side once, when step 3 is first reached, and sent as a header via the proxy (`WWD-005`) |
| WWD-076 | The dialog MUST state that only **confirmed** funds are spendable and that pending deposits are not (backend `WDR-005`) |

### Submission outcomes

| ID | Requirement |
|---|---|
| WWD-080 | **200** means an existing withdrawal was returned for a repeated `Idempotency-Key` and **no TOTP was checked** (backend `WDR-001a`). The UI MUST say so explicitly and link to the existing record. It MUST NOT be reported as a new withdrawal |
| WWD-081 | **201** MUST navigate to the detail page and start tier-A polling immediately (`DAT-044`) |
| WWD-082 | **409 `idempotency_key_reuse`** MUST be explained as a client bug — the same key was presented with different parameters — and MUST require starting a new wizard, not editing and resubmitting |
| WWD-083 | **409 with `details.totp_consumed: true`** MUST render the `AUTH-043` copy: the code has been consumed, the request was not created, wait for a fresh code before correcting the request. This is exactly why backend `API-022` sets the flag |
| WWD-084 | **4xx validation errors** MUST be mapped to the field that caused them. Backend `WDR-002a` moved this validation to be synchronous specifically so the dashboard could show a reason, rather than a withdrawal failing out of band where the dashboard could not see it |
| WWD-085 | **503** MUST distinguish stale prices (backend `WDR-006b`) from other causes, and MUST state that the withdrawal was not created |
| WWD-086 | **Timeout / 502 / 504** MUST render the ambiguous-outcome panel (`WWD-034`) with an additional instruction: check the withdrawal list for a row created in the last minute before doing anything else. The request may have reached payd, consumed the TOTP code, and created the row |
| WWD-087 | After **any** error, the wizard MUST NOT retain the entered TOTP code, and MUST NOT resubmit on its own under any circumstances |

## 11.6 `needs_operator` worklist (`/withdrawals/needs-operator`)

| ID | Requirement |
|---|---|
| WWD-090 | The worklist MUST render `GET /withdrawals?status=needs_operator`, oldest first |
| WWD-091 | Each row MUST show the persisted txid, the last lookup error, and the amount, with a direct Tronscan link (backend `WDR-026`) |
| WWD-092 | The count MUST feed the nav alarm counter at critical severity, and MUST be the one counter that is styled distinctly from the rest (`UI-071`, backend `OPS-006`) |
| WWD-093 | The page MUST open with the explanation that each row is money in an unknown state, that the service will attempt nothing further, and that resolution is a human decision recorded after checking the chain |
| WWD-094 | The only action on this worklist MUST be resolve (`WWD-040`) |
