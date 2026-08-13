# 10. IPN dispatcher

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §10
**ID prefixes in this file:** `IPN-*`
**Related:** [`04-configuration.md`](04-configuration.md) (consumer config, CFG-006..010), [`05-data-model.md`](05-data-model.md) (`ipn_outbox` schema), [`13-withdrawal-engine.md`](13-withdrawal-engine.md) and [`08-order-lifecycle-and-address-pool.md`](08-order-lifecycle-and-address-pool.md) (event producers)

---

## 10.1 Delivery

| ID | Requirement |
|---|---|
| IPN-001 | Every state change MUST write its outbox rows **in the same database transaction** as the state change itself (transactional outbox pattern). There MUST be no path where state changes without events being enqueued |
| IPN-002 | An order-scoped event MUST fan out to exactly one consumer: the order's `consumer` field, or `ipn.default_consumer` when it is NULL |
| IPN-002a | If an order's `consumer` cannot be resolved at enqueue time, the outbox row MUST **still be written**, with `status = 'dead'` and `last_error = 'consumer removed'`, so IPN-001's invariant holds and the event is visible at `GET /ipn/dead`. It MUST NOT be silently dropped and MUST NOT be rerouted to `default_consumer` — IPN-015 forbids that substitution at creation time and the same reasoning applies here. Because enqueue is part of the state-change transaction, a panic on this path would block order state transitions entirely; CFG-006 prevents the situation arising, and this requirement makes it survivable if it somehow does |
| IPN-003 | A global event (`withdrawal.*`, `payment.unattributed`, `energy.*`, `balance.*`) MUST fan out to one row per consumer with `receives_global: true` |
| IPN-004 | `target_url` MUST be snapshotted into the outbox row at enqueue time, so a config change mid-retry does not redirect an in-flight event |
| IPN-005 | The dispatcher MUST POST with `Content-Type: application/json` |
| IPN-006 | Every request MUST carry `X-Event-Id` (the outbox ULID), `X-Timestamp` (Unix seconds), `X-Consumer` (the consumer name), and `X-Signature` |
| IPN-007 | `X-Signature` MUST be `hex(HMAC-SHA256(consumer.secret, timestamp + "." + raw_body))`, computed with **that consumer's own secret** |
| IPN-008 | HTTP 2xx MUST be treated as delivered. Any other response or a network error MUST schedule a retry per `ipn.backoff`. (Retry is correct here and only here: an IPN is an idempotent notification, not a movement of funds — contrast the withdrawal-engine spec §13.0) |
| IPN-009 | After `ipn.max_attempts`, the event MUST be marked `dead` and exposed at `GET /api/v1/ipn/dead` |
| IPN-010 | `POST /api/v1/ipn/{id}/retry` MUST reset a dead event to `pending` for manual redelivery |
| IPN-011 | Events MUST be delivered in creation order **per `(sequence_key, consumer)` pair**, and `sequence_key` MUST NOT be NULL. The dispatcher MUST NOT send event N+1 for a pair while event N is unresolved. v1.1 keyed ordering on `(order_id, consumer)`, and `order_id` is NULL for every global event — in SQL `NULL = NULL` is not true, so an equality-predicate dispatcher grouped nothing and delivered `withdrawal.confirmed` before `withdrawal.broadcast`, with no way for the consumer to detect the inversion |
| IPN-012 | A consumer that is slow or down MUST NOT block delivery to any other consumer. Head-of-line blocking is scoped to one `(sequence_key, consumer)` pair only — so a stalled withdrawal's events do not hold up unrelated global traffic for that consumer |
| IPN-013 | Delivery MUST be concurrent across pairs, with `ipn.workers` (default 4) dispatchers |
| IPN-014 | A disabled consumer MUST stop receiving new events; its pending rows MUST remain queued and resume when re-enabled |
| IPN-015 | An order naming a consumer that does not exist MUST be rejected at creation with HTTP 400, not silently routed to the default |

## 10.2 Event types and payload

Order-scoped: `order.payment_seen`, `order.partial`, `order.paid`, `order.confirmed`, `order.expired`, `order.reverted`.

Global (fan out to `receives_global` consumers): `payment.unattributed`, `withdrawal.broadcast`, `withdrawal.confirmed`, `withdrawal.failed`, `withdrawal.needs_operator`, `energy.purchase_failed`, `energy.balance_low`, `balance.drift_detected`.

```json
{
  "event_id": "01J8XQZ2M4K3N5P7R9T1V3W5Y7",
  "event_type": "order.paid",
  "occurred_at": 1754499463,
  "consumer": "shop-backend",
  "current_status": "paid",
  "snapshot_age_seconds": 0,
  "order": {
    "id": "01J8XQZ0A1B2C3D4E5F6G7H8J9",
    "external_ref": "invoice-2291",
    "address": "TXYZ...",
    "asset": "USDT",
    "expected": "25.000000",
    "received": "25.500000",
    "overpaid": "0.500000",
    "status": "paid",
    "confirmations": 3,
    "metadata": {"user_id": 4471}
  },
  "payments": [
    {
      "txid": "a1b2c3...",
      "from": "TABC...",
      "amount": "25.500000",
      "block_height": 68412330,
      "status": "seen"
    }
  ]
}
```

| ID | Requirement |
|---|---|
| IPN-020 | Amounts in IPN payloads MUST be human-readable decimal strings formatted with the asset's decimals via `hdwallet.FormatUnits`, never raw base units and never floats |
| IPN-021a | **`payload` MUST be an immutable snapshot written at enqueue time and MUST NOT be rebuilt at send time.** A signed event describes the transition that produced it. The dispatcher MUST add exactly two fields at send time, outside the snapshot: `current_status` (the order's status now) and `snapshot_age_seconds`. *(This replaces v1.1's IPN-021, which required the order object to reflect current state at send time — directly contradicting the snapshotted `payload` column, and producing bodies where `event_type: "order.paid"` carried `"status": "partial"` after a reorg during a consumer outage.)* |
| IPN-022 | Consumers MUST be documented as required to treat `event_id` as an idempotency key, since a two-stage flow plus retries guarantees repeat deliveries |
| IPN-023 | The payload MUST include a top-level `"consumer"` field naming the recipient, so a service receiving events for several reasons can tell which subscription produced them |
| IPN-024 | Consumers MUST be documented as required to verify `X-Signature` against **their own** secret and to reject unsigned or mis-signed requests |
| IPN-025 | Consumers MUST be documented as required to act on `event_type` **together with** `current_status`, and to ignore any event whose `current_status` contradicts the action the event would trigger — for example an `order.paid` arriving with `current_status: "partial"` after a reorg |
