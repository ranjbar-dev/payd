# 15. System and audit (`/system`)

**Part of:** payd admin dashboard specification v1.0
**ID prefixes in this file:** `WSYS-*`
**Consumes:** `GET /workers`, `GET /chain/quota`, `GET /chain/status`, `GET /config`, `GET /audit`, `GET /assets`, `GET /auth/whoami`, `GET /healthz`, `GET /readyz`, `GET /metrics`
**Related:** backend `API-039`, `API-040`, `API-043`, `OPS-*`, `RL-006`

---

## 15.1 Purpose

Everything needed to diagnose the service without shell access, plus the
compliance trail. Tabs: Workers, Quota, Config, Assets, Audit, Session.

Almost all of it is tier D — manual refresh. This is a page you open when
something is wrong, not one you watch.

## 15.2 Workers

| ID | Requirement |
|---|---|
| WSYS-001 | The full worker table MUST render `GET /workers` with cursor pagination, showing `worker`, `last_tick_at`, `seconds_since_tick`, `last_error`, `error_count`, `restarts` |
| WSYS-002 | Each of the backend's workers MUST be listed by name with its expected tick interval and what it does, so "is 60 seconds since the last tick a problem" is answerable without opening the backend spec |
| WSYS-003 | `last_error` is sticky and MUST be labelled as such: it is not cleared on the next success, so a recovered fault stays visible (backend `OPS-008`) |
| WSYS-004 | The distinction the backend designed for MUST be rendered explicitly: fresh `last_tick_at` with a flat `error_count` means "failed once, recovered"; a stale `last_tick_at` means "failing now" |
| WSYS-005 | `restarts` MUST be shown. A climbing restart count with a healthy tick is a worker crash-looping back into health |
| WSYS-006 | Tier B (30s) on this tab only; tier D on the others |

## 15.3 Quota

| ID | Requirement |
|---|---|
| WSYS-010 | The quota tab MUST render `GET /chain/quota`: `requests_today`, `daily_request_quota`, `percent_used`, and the seven-day history |
| WSYS-011 | Every day bucket MUST be labelled UTC (`UI-010`) |
| WSYS-012 | The history MUST be rendered as a table with a trend indicator. Backend `RES-001a` describes consumption growing monotonically as the set of balance-holding addresses grows — the trend is the diagnosis, and its first symptom is a complete detection outage |
| WSYS-013 | The 90% readiness threshold MUST be marked, since crossing it fails `/readyz` (backend `OPS-001`, `RL-006`) |
| WSYS-014 | The tab MUST link to the addresses page filtered to balance-holding addresses, since that count is the growth driver (backend `OPS-007`) |

## 15.4 Effective config

| ID | Requirement |
|---|---|
| WSYS-020 | The config tab MUST render `GET /config` as a read-only, grouped view: assets, withdrawal settings, chain depths, order TTL, energy enabled, consumer names |
| WSYS-021 | It MUST be clearly marked read-only, with the note that configuration changes are a YAML edit and a restart (`WNG-006`) |
| WSYS-022 | The UI MUST render only what the endpoint returns. Backend `API-043` allowlists the fields precisely so no endpoint/API/TOTP/key-hash/consumer credential can appear (backend `CFG-011`); the UI MUST NOT add a field, infer one, or request more |
| WSYS-023 | Values that other pages depend on MUST link back to them: `pool_max_size` to addresses, `daily_limit_usd` to withdrawals, `max_burn_trx` to resources, consumer names to webhooks |
| WSYS-024 | The tab requires `admin:read`. Without it, the tab MUST render disabled with the scope named (`AUTH-032`) |

## 15.5 Assets

| ID | Requirement |
|---|---|
| WSYS-030 | The assets tab MUST render `GET /assets`: symbol, kind, contract, decimals, minimum deposit, verified state |
| WSYS-031 | Decimals MUST be shown, since they govern input precision everywhere in the dashboard. Backend `API-034` exists so clients stop hardcoding them |
| WSYS-032 | An unverified asset MUST be flagged |
| WSYS-033 | Contract addresses MUST link to Tronscan |
| WSYS-034 | This response MUST be cached for the session and reused by every amount input and dust indicator, rather than re-fetched per form |

## 15.6 Audit log

| ID | Requirement |
|---|---|
| WSYS-040 | The audit tab MUST render `GET /audit` newest first, with filters for actor, action, subject, and an inclusive Unix `from`/`to` range (backend `API-040`) |
| WSYS-041 | The default order MUST NOT be re-sorted client-side (`UI-043`) |
| WSYS-042 | The tab MUST state what the actor field means in this deployment: every dashboard action arrives with one API key, so the recorded actor is the dashboard, not the human (`AUTH-050`). Attribution to a person comes from the dashboard's own logs |
| WSYS-043 | Audit entries for withdrawals MUST link to the withdrawal, and entries for orders to the order |
| WSYS-044 | Withdrawal-related entries MUST be visually distinguished. Backend `WDR-024` writes every withdrawal request, approval, and outcome here, and those are the entries an incident review needs |
| WSYS-045 | The tab requires `admin:read`; without it, render disabled with the scope named |
| WSYS-046 | There MUST be no export, deletion, or edit of audit records (`WRPT-042`) |

## 15.7 Session

| ID | Requirement |
|---|---|
| WSYS-050 | The session tab MUST render `GET /auth/whoami`: the key name and its sorted scopes |
| WSYS-051 | Missing scopes MUST be listed with the pages and controls they disable (`AUTH-033`) |
| WSYS-052 | The tab MUST show the dashboard session's issue and expiry times, and offer logout |
| WSYS-053 | It MUST restate the difference between the dashboard code and the payd code (`AUTH-003`), since this is where an operator looks when a code is rejected |
| WSYS-054 | It MUST show the configured `PAYD_BASE_URL` host and the Tronscan network in use, so a testnet deployment is identifiable at a glance. A dashboard that looks identical on mainnet and Nile is a way to make a real payout while believing it is a test |

## 15.8 Health and metrics

| ID | Requirement |
|---|---|
| WSYS-060 | The tab MUST show `/healthz` and `/readyz` results side by side, with the distinction stated: `/healthz` is 200 whenever the process runs, `/readyz` reflects worker and chain state (backend `OPS-001`/`OPS-002`) |
| WSYS-061 | Every failing readiness condition MUST render as named human text (`WOVW-012`) |
| WSYS-062 | The UI MUST NOT parse or render `/metrics`. It is Prometheus text and Prometheus is the right consumer (`WNG-009`). A link and a note that the endpoint requires an API key is sufficient |
| WSYS-063 | The tab MUST link to the served `/openapi.yaml`, which is the authority when these docs and the API disagree |
