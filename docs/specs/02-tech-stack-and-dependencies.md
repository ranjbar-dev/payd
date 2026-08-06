# 2. Technology decisions

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §2
**ID prefixes in this file:** `TD-*` (technology decisions), `GAP-*` (hd-wallet library gaps)
**Related:** [`14-key-management.md`](14-key-management.md) for how `hd-wallet` secrets are loaded; [`12-resource-management.md`](12-resource-management.md) and [`13-withdrawal-engine.md`](13-withdrawal-engine.md) for where GAP-002/GAP-004 raw_json workarounds are used

---

| ID | Decision | Rationale |
|---|---|---|
| TD-001 | Go 1.23+ | Required by `hd-wallet`; matches the operator's stack |
| TD-002 | `github.com/ranjbar-dev/hd-wallet` for key management and signing | Trust Wallet–compatible derivation, memguard-protected secrets, Tron TRC-20 signing built in |
| TD-003 | SQLite with WAL mode, single process | Zero-ops persistence; single-writer constraint is why this is one binary. Two connections are opened: a `NORMAL` connection for general work and a `FULL` connection reserved for irreversible-side-effect writes (ARC-006a) |
| TD-004 | One binary, multiple internal workers | SQLite cannot tolerate multi-process write contention; workers are separated at the package level so they remain independently testable and extractable |
| TD-005 | HTTP polling of TronGrid, not WebSocket | TronGrid exposes FullNode HTTP, SolidityNode HTTP, Event Server, and gRPC. There is **no public block-subscription WebSocket**. Real-time push requires java-tron's ZeroMQ message queue or a local event plugin, both of which require running a node — explicitly out of scope |
| TD-006 | Chain follower polls at 3s with gap detection | Matches Tron's 3s block time; TronGrid documentation explicitly discourages faster polling |
| TD-007 | `chi` or `net/http` + `ServeMux` for routing | Minimal dependency surface |
| TD-008 | `zerolog` or `log/slog` for structured logging | Standard |

## 2.1 What `hd-wallet` provides, and what it does not

**Provides:**

- `AddressIndex(hdwallet.TRX, n)` — deposit address derivation at `m/44'/195'/0'/0/n`
- `SignTransaction(hdwallet.TRX, index, *tronpb.SigningInput)` — TRX transfer, TRC-10 transfer, TRC-20 transfer, generic `TriggerSmartContract`
- **raw_json mode** — signs a node-provided pre-built transaction from `raw_data_hex` with a txID guard
- `BroadcastPayload(hdwallet.TRX, msg)` — converts signed output to the TronGrid JSON object the broadcast endpoint expects
- `TransactionID(msg)` — canonical txid extraction
- `IsValidAddress` / `ValidateAddress` for destination validation
- `FromMnemonicBuffer` / memguard-protected secret storage

**Does not provide:**

| ID | Gap | Consequence |
|---|---|---|
| GAP-001 | **No network I/O whatsoever** | The service must supply `ref_block_bytes`, `ref_block_hash`, `expiration`, `timestamp`, and `fee_limit` on every `SigningInput` |
| GAP-002 | **No Stake 2.0 contracts** — only legacy Stake 1.0 `FreezeBalanceContract` / `UnfreezeBalanceContract`. `DelegateResourceContract` is absent | Energy and bandwidth delegation must go through raw_json mode (see [`12-resource-management.md`](12-resource-management.md)) |
| GAP-003 | **No token registry** — TRC-20 decimals are the caller's responsibility | Decimals live in the config file per token |
| GAP-004 | **No broadcast** | The service posts the `BroadcastPayload` output to `/wallet/broadcasttransaction` itself |
