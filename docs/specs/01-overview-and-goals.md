# 1. Overview, goals, non-goals, glossary

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §1
**ID prefixes in this file:** `G-*` (goals), `NG-*` (non-goals)
**Related:** [`03-architecture-and-workers.md`](03-architecture-and-workers.md) for how these goals map to workers; [`20-risks-and-rejected-features.md`](20-risks-and-rejected-features.md) for why NG-003/NG-007 were rejected rather than merely deferred

---

## 1. Overview

A self-hosted, single-tenant payment service that accepts TRX and TRC-20 token payments on the Tron network. It issues deposit addresses to internal services over a REST API, watches the chain for incoming transfers, notifies consumers via signed IPN callbacks, and exposes wallet balances, resource health, and withdrawal control for a web dashboard.

This is **not** a multi-merchant gateway. There is one operator, one HD wallet, one SQLite database, one server.

### 1.1 Goals

| ID | Goal |
|---|---|
| G-001 | Detect incoming TRX and TRC-20 transfers to owned addresses within ~5 seconds |
| G-002 | Attribute payments to orders correctly, including partial and over-payments |
| G-003 | Notify consumer services reliably with at-least-once, idempotent IPN callbacks |
| G-004 | Operate entirely on TronGrid's free tier without self-hosting a node |
| G-005 | Expose wallet balances and energy/bandwidth health for a dashboard |
| G-006 | Support fully automated withdrawals initiated from a web dashboard |
| G-007 | Survive restarts, chain reorganisations, and RPC outages without losing or double-counting payments — **for every supported asset, including native TRX** |
| G-008 | Minimise per-transfer resource cost by sourcing energy from a third-party market before falling back to burning TRX |
| G-009 | Route notifications to multiple independent consumer services, each with its own endpoint and secret |
| G-010 | **Never move the same funds twice.** No automatic retry exists anywhere in the withdrawal path; every ambiguous outcome is resolved against the chain, never by re-attempting |

### 1.2 Non-goals

| ID | Non-goal |
|---|---|
| NG-001 | Multi-tenant / multi-merchant support |
| NG-002 | Chains other than Tron |
| NG-003 | Automatic sweeping of deposit balances to a central or cold wallet — considered and rejected; funds remain in pooled deposit addresses until the operator withdraws |
| NG-007 | Fresh-address-per-order mode — considered and rejected; the address pool is always reused |
| NG-004 | Fiat settlement, invoicing, or accounting integration |
| NG-005 | A built-in frontend — the service is API-only; the dashboard is a separate project |
| NG-006 | Refunds (a refund is just an operator-initiated withdrawal) |
| NG-008 | **Automatic retry of any withdrawal, broadcast, or transfer.** See [`13-withdrawal-engine.md`](13-withdrawal-engine.md) §13.0. A withdrawal that does not succeed is surfaced to the operator; the service never re-attempts it |

### 1.3 Glossary

| Term | Meaning |
|---|---|
| **Deposit address** | An address derived from the operator's HD wallet, assigned to an order to receive payment |
| **Order** | A request for payment: expected asset, expected amount, deadline, assigned address |
| **Payment** | A single detected on-chain transfer into a deposit address |
| **Seen** | A payment included in a block but not yet solidified |
| **Confirmed** | A payment in a solidified (irreversible) block |
| **Solidified** | A Tron block confirmed by more than two-thirds of Super Representatives; irreversible |
| **IPN** | Instant Payment Notification — an HTTP POST to a consumer service on state change |
| **Sun** | Smallest TRX unit; 1 TRX = 1,000,000 sun |
| **Energy / Bandwidth** | Tron's two resource types, consumed by smart-contract execution and transaction size respectively |
| **Retry** | Re-attempting an action that has an external side effect. **Forbidden in the withdrawal path** |
| **Reconciliation** | Querying the chain to discover what an earlier action actually did. Always permitted, and is the *only* means of resolving an ambiguous withdrawal |
| **Assignment window** | The interval during which an address belongs to a given order; see ORD-002b in [`08-order-lifecycle-and-address-pool.md`](08-order-lifecycle-and-address-pool.md) |
