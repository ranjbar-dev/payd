# 4. Authentication, session, and TOTP

**Part of:** payd admin dashboard specification v1.0
**ID prefixes in this file:** `AUTH-*`
**Related:** [`03-architecture-and-bff.md`](03-architecture-and-bff.md) (the proxy this protects), [`11-withdrawals.md`](11-withdrawals.md) (where payd TOTP is used), backend `API-020`/`API-022`/`API-022a`

---

## 4.1 Two different TOTPs

This is the single most confusable thing in the dashboard, so it is stated
first and repeated in the UI.

| | Session TOTP | payd TOTP |
|---|---|---|
| Secret lives in | `DASH_TOTP_SECRET` (Next.js env) | payd's config, known only to payd |
| Entered | Once, at login | Once **per fund-moving action** |
| Protects | Access to the dashboard | The specific transaction being authorised |
| Verified by | Next.js | payd, via `X-TOTP` |
| Reusable | Within its 30s step, no — but a session persists after | Never. Single-use, persisted in `used_totp` |

| ID | Requirement |
|---|---|
| AUTH-001 | The two secrets MUST be different values. Startup MUST fail if `DASH_TOTP_SECRET` equals any payd secret available to the process |
| AUTH-002 | A valid session MUST NOT be sufficient to move funds. Every payd TOTP-gated action MUST prompt for a fresh code at the moment of the action, regardless of how recently the operator logged in |
| AUTH-003 | The UI MUST label the two distinctly and permanently — "dashboard code" at login, "payd code" at every action — never just "TOTP" or "2FA code". An operator who types the wrong one gets a 401 that names neither |

## 4.2 Login

| ID | Requirement |
|---|---|
| AUTH-010 | `/login` MUST accept a password and a session TOTP code, verify the password against `DASH_PASSWORD_HASH` with Argon2id, and verify the code against `DASH_TOTP_SECRET` with a ±1 step window |
| AUTH-011 | A failed login MUST return one generic message ("invalid credentials") for a wrong password, a wrong code, or both, and MUST NOT reveal which failed — the same reasoning as backend `API-021` |
| AUTH-012 | Password comparison MUST be constant-time, and the password MUST be verified even when the TOTP is already known to be wrong, so response timing does not distinguish the two |
| AUTH-013 | Login MUST be rate limited to 5 attempts per minute per IP and 20 per hour, returning 429 without distinguishing the cause. Without this, the TOTP's 6-digit space is brute-forceable within its window |
| AUTH-014 | A session TOTP code MUST NOT be accepted twice. The used `(code, step)` MUST be held in memory for 90 seconds — unlike payd's `used_totp`, in-memory is sufficient here because a restart invalidates every session anyway |
| AUTH-015 | The login form MUST NOT be pre-filled, autocompleted for the TOTP field, or remembered. `autocomplete="one-time-code"` on the code field, `current-password` on the password |

## 4.3 Session

| ID | Requirement |
|---|---|
| AUTH-020 | The session MUST be a signed, encrypted cookie: `httpOnly`, `secure`, `sameSite=strict`, `path=/`. No session data MUST be readable by client JavaScript |
| AUTH-021 | The cookie MUST contain only an issue timestamp, an expiry, and a random session id. It MUST NOT contain the payd API key, scopes, the password hash, or any secret (`INV-4`) |
| AUTH-022 | Sessions MUST expire after `SESSION_TTL_SECONDS` (default 8h) absolute. There MUST be no sliding renewal — an idle tab open for three days must not still be able to authorise a withdrawal |
| AUTH-023 | The UI MUST warn 5 minutes before expiry and MUST NOT silently discard an in-progress form on expiry. A withdrawal wizard losing its inputs to a session timeout invites a hurried, unverified re-entry |
| AUTH-024 | Logout MUST clear the cookie and invalidate the session id server-side, so the cookie cannot be replayed from a browser's back cache |
| AUTH-025 | Every mutating proxy request MUST carry a CSRF token bound to the session (double-submit or origin check), since `sameSite=strict` alone is not a complete defence for a tool that can move money |
| AUTH-026 | Session verification MUST happen in `app/(dash)/layout.tsx` and in the proxy independently. Layout-only checks protect the render, not the endpoint |

## 4.4 Scopes and whoami

| ID | Requirement |
|---|---|
| AUTH-030 | On session creation, the server MUST call `GET /auth/whoami` and cache the key name and scopes for the session's lifetime. The ordering is fixed by `BFF-013`: create the session, call whoami in process with that session's cookie, and invalidate the session before emitting any `Set-Cookie` if the call fails. A login MUST NOT succeed with an unverified key |
| AUTH-031 | The System page MUST display the key name and its sorted scopes verbatim from `/auth/whoami` |
| AUTH-032 | A UI control whose backend route requires a scope the key lacks MUST be rendered disabled with the missing scope named in its tooltip, not hidden. A hidden control makes a misconfigured key look like a missing feature |
| AUTH-033 | If `/auth/whoami` reports missing scopes, a persistent banner MUST name each missing scope and the pages it disables (`WST-023`) |

## 4.5 payd TOTP in the UI

Backend `API-022a` requires the code in the `X-TOTP` header and rejects a code
in the body with 400 `totp_in_body`. Backend `API-022` makes each code
single-use and persists that fact.

| ID | Requirement |
|---|---|
| AUTH-040 | The payd TOTP code MUST be sent by the proxy in the `X-TOTP` header only. The client MUST send it to the proxy in a JSON field, and the proxy MUST move it to the header and strip it from the forwarded body — a body-carried code is a 400 |
| AUTH-041 | The TOTP code MUST NOT appear in any URL, query string, browser history entry, client log, or analytics event |
| AUTH-042 | The TOTP input MUST be a dedicated `TotpField` component: 6 digits, numeric input mode, `autocomplete="one-time-code"`, no autofill from a password manager's password field, cleared on submit whether the submission succeeded or failed |
| AUTH-043 | On any error response with `details.totp_consumed: true`, the UI MUST clear the field and display: *"That code has been used. Wait for the next code before correcting the request."* Backend `API-022` sets this flag exactly so the operator does not resubmit into a guaranteed 401 |
| AUTH-044 | On a 401 from a TOTP-gated route, the UI MUST NOT auto-resubmit, and MUST NOT preserve the entered code for a second attempt |
| AUTH-045 | The four TOTP-gated routes MUST be handled by one shared confirmation component, so this behaviour cannot diverge between them: `POST /withdrawals`, `POST /withdrawals/{id}/resolve`, `POST /wallets/{address}/delegate`, `POST /wallets/{address}/clear-drift` |

## 4.6 Audit

| ID | Requirement |
|---|---|
| AUTH-050 | payd writes its own `audit_log` with the API key name and source IP (backend `WDR-024`). Since every request arrives from the dashboard with one key, the recorded actor is the dashboard, not the human |
| AUTH-051 | The dashboard MUST log login success, login failure, logout, and every mutation it proxies — timestamp, action, target, outcome — to its own application log. This is the only record that ties an action to the dashboard session rather than to the shared key |
| AUTH-052 | Dashboard logs MUST NOT contain the password, either TOTP code, the API key, or a request body from a TOTP-gated route (mirrors backend `OPS-013` and `API-026`) |
