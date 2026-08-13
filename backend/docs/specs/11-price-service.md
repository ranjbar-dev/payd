# 11. Price service

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §11
**ID prefixes in this file:** `PRC-*`
**Related:** [`08-order-lifecycle-and-address-pool.md`](08-order-lifecycle-and-address-pool.md) (ORD-009 stale-price gate), [`13-withdrawal-engine.md`](13-withdrawal-engine.md) (WDR-006b stale-price gate)

---

| ID | Requirement |
|---|---|
| PRC-001 | The poller MUST fetch `GET https://api.binance.com/api/v3/ticker/price?symbols=["TRXUSDT"]` every 60 seconds and upsert into `prices` |
| PRC-002 | USDT and USDC MUST be treated as 1.00 USD without an API call unless a `*USDT` pair is explicitly configured |
| PRC-003 | The price provider MUST be an interface (`price.Provider`) with Binance as the default implementation, so an alternative source can be added without touching callers |
| PRC-004 | A failed fetch MUST NOT overwrite the last known good price; it MUST leave the existing row and increment an error counter |
| PRC-005 | Prices older than `price.stale_after` MUST be treated as unavailable, causing **both order creation and withdrawal creation** to fail with HTTP 503 rather than valuing off a frozen quote (see WDR-006b) |
| PRC-006 | `orders.price_usd` and `price_at` MUST be snapshotted at creation so the order's valuation is reproducible later |
| PRC-007 | Backoff on repeated failure MUST be exponential to 5 minutes, and MUST NOT hammer the endpoint |
