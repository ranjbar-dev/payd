# 13. Webhooks / IPN (`/webhooks`)

**Part of:** payd admin dashboard specification v1.0
**ID prefixes in this file:** `WIPN-*`
**Consumes:** `GET /ipn/consumers`, `GET /ipn/dead`, `POST /ipn/{id}/retry`, `POST /ipn/replay`, `POST /ipn/test`
**Related:** backend [`10-ipn-dispatcher.md`](../../../backend/docs/specs/10-ipn-dispatcher.md), `API-035`, `API-036`

---

## 13.1 Why retry is allowed here

Every other retry in this dashboard is forbidden. This page is the exception,
and the distinction must be visible in the UI so the exception does not read as
an inconsistency.

An IPN is an idempotent notification, not a movement of funds. Consumers are
required to treat `event_id` as an idempotency key (backend `IPN-022`), so
redelivery is safe. Backend `IPN-008` states this explicitly: *"Retry is
correct here and only here."*

| ID | Requirement |
|---|---|
| WIPN-001 | The page MUST state, where the retry and replay controls are, that redelivering an IPN is safe because consumers treat `event_id` as an idempotency key — and that this is the only retry in the system |
| WIPN-002 | Retry controls MUST NOT be styled like withdrawal actions, and withdrawal-related events MUST NOT gain a retry affordance for the underlying withdrawal from this page. Retrying a `withdrawal.confirmed` notification redelivers a message; it does nothing to the withdrawal |

## 13.2 Consumers

| ID | Requirement |
|---|---|
| WIPN-010 | The consumers table MUST render `GET /ipn/consumers`: name, enabled state, `receives_global`, and pending and dead counts |
| WIPN-011 | The target URL MUST be shown if the endpoint returns it, but **no secret MUST ever be rendered**, and the UI MUST never request one. Backend `CFG-011` and `API-043` keep credentials off the API surface entirely (`INV-4`) |
| WIPN-012 | A disabled consumer MUST show that its pending rows remain queued and resume on re-enable (backend `IPN-014`), so the operator does not conclude events were lost |
| WIPN-013 | A rising pending count MUST be flagged. Backend `IPN-012` scopes head-of-line blocking to one `(sequence_key, consumer)` pair, so a growing queue for one consumer is that consumer being slow or down, not a global fault |
| WIPN-014 | There MUST be no control to add, edit, enable, disable, or delete a consumer. Consumers are configuration (backend `CFG-006`–`CFG-010`); changing them is a YAML edit and a restart (`WNG-006`) |
| WIPN-015 | Tier B polling (30s) |

## 13.3 Test ping

| ID | Requirement |
|---|---|
| WIPN-020 | The test action MUST call `POST /ipn/test` for a named consumer and render the returned status code and latency |
| WIPN-021 | The UI MUST state what the test does and does not do: it sends one signed `test.ping` directly, using the production signature implementation, and **writes no outbox row** (backend `API-035`). It is not a business event and will not appear in any queue |
| WIPN-022 | A failure MUST show the status code and any error verbatim. This is a connectivity and signature-verification tool; a summarised failure is useless for it |
| WIPN-023 | The test MUST be available per consumer from the consumers table, not as a separate form requiring the operator to retype a name |

## 13.4 Dead letters

| ID | Requirement |
|---|---|
| WIPN-030 | The dead-letter table MUST render `GET /ipn/dead`: event id, consumer, event type, order, attempts, last status code, last error, created |
| WIPN-031 | The payload MUST be viewable as pretty-printed JSON, marked as an **immutable snapshot written at enqueue time** (backend `IPN-021a`). It describes the transition that produced it, not the entity's current state |
| WIPN-032 | Where an event is order-scoped, the row MUST link to the order and show that order's current status alongside the snapshot, fetched from `GET /orders/{id}`. This mirrors the `current_status` field the dispatcher adds at send time — that field is computed when the event is sent and is NOT part of the stored snapshot (backend `IPN-021a`), so the UI reads it from the order rather than from `payload` |
| WIPN-033 | A snapshot whose status contradicts the order's current status MUST be flagged as expected behaviour, not as corruption. Backend `IPN-025` requires consumers to handle exactly this — an `order.paid` arriving with `current_status: partial` after a reorg |
| WIPN-034 | An event dead-lettered with `last_error: 'consumer removed'` MUST be explained: the consumer could not be resolved at enqueue time, the row was written anyway so no state change happened without its event, and it was deliberately not rerouted to the default consumer (backend `IPN-002a`) |
| WIPN-035 | Single retry MUST call `POST /ipn/{id}/retry`, which resets the event to `pending` (backend `IPN-010`), and MUST state that delivery is asynchronous — a successful call means requeued, not delivered |
| WIPN-036 | After a retry the row MUST be re-fetched rather than optimistically removed. It may fail again immediately |
| WIPN-037 | The dead count MUST feed the nav alarm counter |

## 13.5 Bulk replay

| ID | Requirement |
|---|---|
| WIPN-040 | Replay MUST call `POST /ipn/replay` with a consumer filter and an inclusive Unix `from`/`to` range |
| WIPN-041 | **`dry_run` MUST default to true in the UI**, matching the backend default (backend `API-036`). The dry-run count MUST be shown and acknowledged before a live replay is possible |
| WIPN-042 | The UI MUST state the backend's per-call ceiling of 200 events and MUST show how many calls a larger range would need. Backend `API-036` caps it so bulk recovery stays inside the API rate limit |
| WIPN-043 | The UI MUST NOT loop automatically to replay more than 200 events. Each call MUST be an explicit operator action, so a large replay cannot be started and walked away from |
| WIPN-044 | The confirmation MUST restate consumer, range, and count, and MUST state that consumers will receive these events again and must be idempotent |
| WIPN-045 | Date inputs MUST be entered in local time with the resolved UTC range displayed (`UI-010`) |

## 13.6 Event reference

| ID | Requirement |
|---|---|
| WIPN-050 | The page MUST include a static reference of event types, so an operator can tell whether a missing event was ever supposed to exist |
| WIPN-051 | Order-scoped events fan out to exactly one consumer — the order's, or the default (backend `IPN-002`): `order.payment_seen`, `order.partial`, `order.paid`, `order.confirmed`, `order.expired`, `order.reverted` |
| WIPN-052 | Global events fan out to every consumer with `receives_global` (backend `IPN-003`): `payment.unattributed`, `withdrawal.broadcast`, `withdrawal.confirmed`, `withdrawal.failed`, `withdrawal.needs_operator`, `energy.purchase_failed`, `energy.balance_low`, `balance.drift_detected` |
| WIPN-053 | The reference MUST document the delivery headers consumers verify — `X-Event-Id`, `X-Timestamp`, `X-Consumer`, `X-Signature` — and that the signature is `hex(HMAC-SHA256(secret, timestamp + "." + raw_body))` computed with that consumer's own secret (backend `IPN-006`/`IPN-007`) |
| WIPN-054 | The reference MUST NOT display, and the UI MUST NOT have access to, any consumer secret (`WIPN-011`) |
