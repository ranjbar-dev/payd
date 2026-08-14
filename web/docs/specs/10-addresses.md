# 10. Addresses (`/addresses`)

**Part of:** payd admin dashboard specification v1.0
**ID prefixes in this file:** `WADR-*`
**Consumes:** `GET /wallets`, `GET /wallets/{address}`, `GET /wallets/with-balance`, `GET /wallets/needs-resources`, `POST /wallets/{address}/disable`, `POST /wallets/{address}/delegate`, `POST /wallets/{address}/clear-drift`
**Related:** backend `POOL-*`, `BAL-*`, `RES-*`, `API-010`–`API-014`

---

## 10.1 Purpose

The address pool is where all the money physically is. Sweeping is rejected
(backend `NG-003`), so funds stay in pooled deposit addresses until the
operator withdraws them, and the set of addresses holding a balance only ever
grows. This page shows what is where, and whether it can be moved.

## 10.2 Pool list

Pool states: `free → assigned → cooling → free`, plus `disabled`.

| ID | Requirement |
|---|---|
| WADR-001 | Columns: address, `hd_index`, state, per-asset confirmed and pending balances, resource sufficiency, drift flag, assigned order, `cooling_until`, last checked |
| WADR-002 | Confirmed and pending MUST be separate columns per asset, never summed (`UI-004`, backend `API-014`) |
| WADR-003 | The four states MUST be visually distinct, and `cooling` MUST show its remaining time. Cooldown exists because reuse plus late payment is the primary attribution hazard (backend §8.1); an operator seeing "cooling 4m" understands why the address is not available |
| WADR-004 | An `assigned` address MUST link to its order |
| WADR-005 | The resource wallet MUST be identifiable in the list and MUST be shown as permanently `disabled` and never poolable (backend `POOL-008`/`CFG-013`) |
| WADR-006 | Filters: state, has-balance, needs-resources, drift-detected, asset. Every one MUST be a server-side query — `state`, `asset` and `drift` as parameters on `GET /wallets`, has-balance as `GET /wallets/with-balance`, needs-resources as `GET /wallets/needs-resources`. The UI MUST NOT filter the returned page in the browser: a cursor page is not the pool, and filtering it client-side reports "3 disabled addresses" when the pool holds thirty (`DAT-020`) |
| WADR-007 | A `disabled` address MUST remain listed with its history. Disabling removes it from rotation, it does not delete it (backend `POOL-007`) |
| WADR-008 | The list MUST show pool health: total addresses, free count against `wallet.pool_min_free`, and total against `wallet.pool_max_size` from `GET /config`. Backend `LIF-003` fails order creation with 503 at the ceiling, and that ceiling should be visible before it is hit |
| WADR-008a | Both counts MUST come from `GET /stats` `addresses`, which reports the pool grouped by state. They MUST NOT be counted from the loaded page: `GET /wallets` is cursor-paginated and a page count is not a pool size. The same rule that produced `WOVW-004a` for the alarm counters applies here |
| WADR-009 | Tier B polling (30s) |

## 10.3 Balance drift

Backend `BAL-002`: `chain_raw` is written only by the 6-hour reconcile. When it
disagrees with the ledger's `confirmed_raw`, drift is flagged — and a
drift-flagged address cannot be withdrawn from (backend `WDR-002a`).

| ID | Requirement |
|---|---|
| WADR-020 | `drift_detected` MUST render at critical severity. It means the ledger and the chain disagree about how much money is at this address |
| WADR-021 | The address detail MUST show `confirmed_raw` and `chain_raw` side by side per asset, so the operator can see the size and direction of the disagreement |
| WADR-022 | The clear-drift action MUST be per asset (backend `BAL-002`), MUST require the payd TOTP code, and MUST require the operator to acknowledge the current `chain_raw` value shown in the dialog |
| WADR-023 | The dialog MUST state plainly that clearing the flag **records an acknowledgement and does not correct any balance.** It re-enables withdrawals from the address; it does not make the ledger right |
| WADR-024 | The dialog MUST recommend investigating before clearing, and MUST link to the address's payment history and its Tronscan page. A drift usually means a payment the detector missed or a transfer nothing recorded |
| WADR-025 | Clearing MUST invalidate the address, the wallet lists, and the withdrawal estimate cache for that address |

## 10.4 Address detail

| ID | Requirement |
|---|---|
| WADR-030 | Detail MUST render everything `GET /wallets/{address}` returns: `hd_index`, state, per-asset balances, energy and bandwidth figures, `needs_resources`, drift flags, the current assignment (`assigned_order_id`, `cooling_until`), and `checked_at`. Per-address assignment history is NOT retained by the backend and MUST NOT be implied: the address's orders and payments lists are the history, and the assigned order carries its own window through `created_at` and `address_released_at` |
| WADR-031 | The payment history MUST be paginated per `API-025` and MUST show both directions |
| WADR-032 | Energy and bandwidth MUST each render `available`, `limit`, `required`, and `sufficient`, not a single verdict (backend `API-013`) |
| WADR-033 | `can_withdraw` MUST render per asset, since TRX transfers need no energy (backend `API-011`). One green dot for the whole address is exactly the v1.1 bug that reported `can_withdraw: true` for an address with no bandwidth |
| WADR-034 | `blocked_by` MUST name which resource is short, in the row itself, not behind a tooltip |
| WADR-035 | `checked_at` MUST be shown with its age, and its absence MUST render as "never polled". Backend `RES-001a` polls on a tiered cadence — high-balance addresses every few minutes, the rest every 6 hours — so a stale figure on a small address is expected, not a fault, and the UI MUST say so rather than flagging it |
| WADR-036 | Detail MUST link to the withdrawal wizard pre-filled with this address as source, and to the address's orders and payments |
| WADR-037 | Tier B polling (30s); no tier A — resource state changes on the backend's polling cadence, not faster |

## 10.5 Needs-resources view

| ID | Requirement |
|---|---|
| WADR-040 | This view MUST render `GET /wallets/needs-resources` and MUST NOT paginate it (`DAT-025`) — the endpoint returns the complete set and ignores `limit`/`cursor` |
| WADR-041 | Each row MUST show `estimated_burn_trx` and `estimated_rent_trx` together, so the real choice is visible (backend `API-010`). **Known gap:** the backend does not currently compute `estimated_rent_trx` on this endpoint, so it is always absent and `WADR-043`'s "provider unavailable" is what renders. The UI MUST be built to display it the moment the field appears, and MUST NOT substitute a burn figure or a client-side estimate for it |
| WADR-042 | `energy_fee_sun` MUST be displayed alongside the burn estimate. It is a governance-controlled chain parameter that has been raised by proposal more than once; at 210 sun the same transfer costs twice what it does at 100 (backend §12.2) |
| WADR-043 | An omitted `estimated_rent_trx` MUST render as "provider unavailable", never as zero or as a stale figure. Backend `API-012` omits it deliberately when energy is disabled or the provider is unreachable |
| WADR-044 | An omitted `estimated_burn_trx` MUST render as "chain parameters not yet read", linking to the chain params card. Backend `RES-022` holds withdrawals rather than assuming a price, and this is the operator's warning that withdrawals are blocked |
| WADR-045 | The view MUST explain the structural cause, because it is unintuitive: an address that has only ever received USDT holds zero TRX, and bandwidth burn is paid in TRX from the account's own balance. Rented or delegated *energy* does not cover bandwidth (backend §12.4) |
| WADR-046 | Each row MUST offer the delegate action directly. This view is a worklist |

## 10.6 Delegate

| ID | Requirement |
|---|---|
| WADR-050 | Delegation MUST require the payd TOTP code and the `resources:write` scope |
| WADR-051 | The dialog MUST require an explicit choice of `ENERGY` or `BANDWIDTH`. It MUST NOT default to `ENERGY` — backend `RES-010` calls out that hardcoding `ENERGY` was the v1.1 bug that made bandwidth unsourceable |
| WADR-052 | The dialog MUST show the resource wallet's current available energy, bandwidth, and TRX from `GET /resources/wallet` before submission, so the operator is not delegating from an empty wallet |
| WADR-053 | The dialog MUST state that **the delegation broadcast is attempted exactly once and is never retried** (backend `RES-013`, `WDR-000`). This is a fund-moving transaction under the same no-retry rule as a withdrawal |
| WADR-054 | On any ambiguous outcome, the UI MUST NOT offer to try again. It MUST direct the operator to the resource grants list ([`12`](12-resources-and-energy.md)) to see the recorded grant and its on-chain resolution |
| WADR-055 | After submission, the UI MUST link to the grant record rather than reporting success from the HTTP response alone |
| WADR-056 | The dialog MUST state that the resource wallet's stake is not managed here: the service never stakes or unstakes automatically (backend `RES-014`), and unstaking has a 14-day period |

## 10.7 Disable

| ID | Requirement |
|---|---|
| WADR-060 | Disable MUST use `<ConfirmDialog>` and MUST NOT require TOTP (backend scope: `wallets:write`) |
| WADR-061 | The dialog MUST state what disabling does — permanent removal from rotation, history retained, no funds moved (backend `POOL-007`) |
| WADR-062 | Disabling an address holding a balance MUST warn that the funds remain there and that the address must still be withdrawn from explicitly |
| WADR-063 | Disabling an address with an active assigned order MUST warn that the order is unaffected and the customer may still pay to it |
| WADR-064 | There MUST be no re-enable control. The backend exposes none; the UI MUST NOT imply one exists |

## 10.8 With-balance view

| ID | Requirement |
|---|---|
| WADR-070 | `GET /wallets/with-balance` MUST back the withdrawal wizard's source selector — it is the list of addresses holding **confirmed** funds |
| WADR-071 | It MUST also be reachable as a filter on the pool list, so the operator can see the treasury without opening the withdrawal flow |
| WADR-072 | Each row MUST show `can_withdraw` per asset, so an address holding funds it cannot move is visible before the operator starts a withdrawal that will be blocked |
