# 3. Architecture and the BFF proxy

**Part of:** payd admin dashboard specification v1.0
**ID prefixes in this file:** `BFF-*`
**Related:** [`04-auth-and-session.md`](04-auth-and-session.md) (who may use the proxy), [`05-data-fetching.md`](05-data-fetching.md) (how the client calls it), backend [`17-operations.md`](../../../backend/docs/specs/17-operations.md) `OPS-009`

---

## 3.1 Why a proxy at all

payd serves plain HTTP and authenticates with a shared secret in a header.
Backend `OPS-009` states it must not be exposed for remote access. Two
consequences follow, and together they decide the architecture:

1. If the browser called payd directly, payd would have to be reachable from
   wherever the operator's browser is — the exact exposure `OPS-009` forbids.
2. The browser would have to hold the API key. A key in `localStorage` is a key
   in every XSS, every extension, and every screen recording.

So the browser talks to Next.js, and Next.js talks to payd over loopback. The
key never leaves the server, and payd's listening socket never leaves
`127.0.0.1`.

```
  browser ──── session cookie (httpOnly, signed) ────► Next.js server
                                                            │
                                              X-API-Key + X-TOTP
                                                            ▼
                                                   payd (127.0.0.1:8080)
```

## 3.2 The proxy

| ID | Requirement |
|---|---|
| BFF-001 | Every call to payd MUST originate from the Next.js server. The browser MUST NOT be able to reach `PAYD_BASE_URL` at all — it is not a CORS decision, it is a network reachability one |
| BFF-002 | The payd API key MUST NOT appear in any response body, header, cookie, URL, error message, log line, or client-side bundle. This includes error paths: a 401 from payd MUST be re-rendered as a generic upstream error, never by echoing the request that produced it (`INV-4`) |
| BFF-003 | The proxy MUST be a single catch-all route handler at `app/api/payd/[...path]/route.ts`. One place sets the key, one place enforces the allowlist, one place normalises errors |
| BFF-004 | The proxy MUST reject any request whose path is not in an explicit allowlist of the backend's routes, with 404. A pass-through that forwards arbitrary paths turns the dashboard into an open relay to payd for anyone with a session |
| BFF-005 | The allowlist MUST be derived from `backend/internal/api/openapi.yaml` at build time, not maintained by hand. A hand-kept list drifts, and it drifts silently in the permissive direction |
| BFF-006 | The proxy MUST verify the session cookie before forwarding, and MUST return 401 without contacting payd when it is absent or invalid |
| BFF-007 | The proxy MUST forward only: the method, the allowlisted path, the query string, and — for mutations — the JSON body and the `Idempotency-Key` and `X-TOTP` headers. It MUST NOT forward arbitrary client headers, cookies, or `Authorization` |
| BFF-008 | The proxy MUST pass payd's error envelope (`{"error":{"code","message","details"}}`) through unmodified, including `details`, since the UI branches on `code` and reads `details` for `totp_consumed` and `external_ref_conflict`. It MUST NOT wrap, rename, or flatten it |
| BFF-009 | The proxy MUST pass payd's HTTP status through unchanged, with one exception: an upstream network failure (payd unreachable, DNS, timeout) MUST become 502 with code `upstream_unreachable`, which is distinguishable from payd itself returning 500 |
| BFF-010 | The proxy MUST apply a request timeout of 15 seconds for reads and 30 seconds for `POST /withdrawals`, and MUST NOT retry a timed-out mutation under any circumstance (see `BFF-020`) |
| BFF-011 | The proxy MUST stream CSV export responses rather than buffering them, preserving `Content-Disposition`. Backend `API-046` streams deliberately; buffering re-introduces the memory ceiling it was written to avoid |
| BFF-012 | The proxy MUST NOT cache. Every response MUST carry `Cache-Control: no-store`. Caching operational state produces an operator acting on a stale balance |

## 3.3 Retry policy at the proxy

This is where a well-meaning HTTP client silently breaks backend `WDR-000`.

| ID | Requirement |
|---|---|
| BFF-020 | The proxy MUST NOT retry any non-idempotent request — every `POST` — for any reason: timeout, connection reset, 5xx, or 429. `POST /withdrawals` in particular MUST be attempted at most once per user submission. The backend's `Idempotency-Key` makes a *deliberate* resubmission safe; it does not make an *automatic* one correct, because a timeout that already reached payd will have consumed the TOTP code and created the row |
| BFF-021 | The proxy MAY retry a `GET` at most once on a connection-level failure, and MUST NOT retry a `GET` that returned any HTTP status, including 429 and 5xx. Retrying a 429 is how a rate limit becomes an outage |
| BFF-022 | The fetch client MUST be configured with retries explicitly disabled rather than relying on a default. If a library's default is to retry, that default MUST be overridden at construction, not per call site |
| BFF-023 | On `POST` timeout, the proxy MUST return 504 with code `upstream_timeout` and a `details.outcome_unknown: true` flag, and the UI MUST render the ambiguous-outcome guidance in [`11-withdrawals.md`](11-withdrawals.md) `WWD-034` rather than a generic failure toast |

## 3.4 Rendering strategy

| ID | Requirement |
|---|---|
| BFF-030 | Pages MUST render on the client against the proxy, not as server components fetching payd directly. Reason: the polling model in [`05-data-fetching.md`](05-data-fetching.md) needs a client-side cache with per-query intervals, and server components would re-render whole trees on every tick |
| BFF-031 | Exception: the initial session check and the navigation shell MUST be server-rendered, so an expired session redirects to `/login` before any page content is sent |
| BFF-032 | There MUST be no Next.js `revalidate`, ISR, or `fetch` cache on any payd call. `no-store` everywhere (see `BFF-012`) |
| BFF-033 | Mutations MUST go through the proxy like reads. There MUST be no Server Action that calls payd, because a Server Action that moves funds is invoked by a POST the browser can replay |

## 3.5 Deployment

| ID | Requirement |
|---|---|
| BFF-040 | The dashboard and payd MUST be deployable on the same host, with payd bound to loopback and only the dashboard's port exposed |
| BFF-041 | The dashboard MUST be served over TLS. It carries the session cookie and, on the withdrawal path, a payd TOTP code in a request body — the same exposure `OPS-009` describes, one layer up |
| BFF-042 | Deployment documentation MUST state that `server.trusted_proxy` in payd's config is set per backend `CFG-016` only when a TLS-terminating proxy is actually in front, and that the dashboard being HTTPS does not make payd's own listener safe to expose |
| BFF-043 | The dashboard MUST NOT be exposed to the public internet without an additional network-level control (VPN, mTLS, or IP allowlist). Its login is a password and a TOTP code; behind it is the ability to move every deposit balance |
