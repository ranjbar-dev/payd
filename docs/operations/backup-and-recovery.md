# Backup and recovery runbook

This runbook is the operator procedure for `payd`. Keep database backups and
the encrypted seed file in separate protected locations. Never put the
mnemonic, private keys, TronGrid/API keys, TOTP codes, or IPN secrets in a
command line, ticket, terminal transcript, or log (OPS-013, KEY-007).

## Online backup (OPS-010)

SQLite WAL mode permits a consistent backup while `payd` is running. Back up
the database through SQLite; do not copy the live `.db`, `-wal`, and `-shm`
files independently.

```sh
sqlite3 /var/lib/payd/payd.db ".backup '/var/backups/payd/payd-$(date -u +%Y%m%dT%H%M%SZ).db'"
sqlite3 /var/backups/payd/payd-YYYYMMDDTHHMMSSZ.db "PRAGMA integrity_check;"
```

The integrity command must print `ok`. Periodically restore a backup on an
isolated host and start the same `payd` version against it; an unread backup is
not a backup. The automated store test also runs `sqlite3 .backup` while both
service database connections remain open.

## Restore a usable database backup (OPS-011)

1. Stop `payd` and keep the failed database files for investigation.
2. Run `PRAGMA integrity_check` against the selected backup. Abort if it does
   not return `ok`.
3. Copy the backup to the configured `database.path`, set owner-only access
   (`chmod 0600` on Unix), and do not copy an old `-wal` or `-shm` beside it.
4. Restart `payd`. The follower resumes at `crawler_state.last_height` and
   catches up from the chain.
5. Before allowing an operator or consumer to submit a new withdrawal, inspect
   `GET /api/v1/withdrawals?status=needs_operator`, the startup logs, and
   `payd_withdrawals_needs_operator`. The Withdrawal Engine performs its
   WDR-018/018a/018b/019 startup resolution before claiming new work
   (WDR-019a); do not bypass that ordering by editing withdrawal statuses.
6. Keep the service out of rotation until `/readyz` returns 200 and the chain,
   solidity, price, clock, quota, database, and burn-ceiling reasons have all
   cleared.

## Total database loss: rebuild from the mnemonic (OPS-012)

Order attribution, external references, metadata, IPN history, audit history,
and withdrawal records cannot be reconstructed from the chain. Address and
payment-ledger history can be reconstructed:

1. Stop `payd`, disable withdrawals, and block its write API at the listener or
   firewall. Do not issue any new withdrawal during reconstruction.
2. On an isolated recovery host, use `seedtool` through stdin to recreate the
   encrypted seed file. Never pass the mnemonic as an argument or environment
   variable. Use the original HD account number. Preserve the printed account
   xpub as the address-derivation cross-check (KEY-001..008).
3. Re-derive every possible deposit address at
   `m/44'/195'/<account>'/0/<index>` from index `0` through the configured
   `wallet.pool_max_size - 1`, plus the configured resource-wallet index. This
   bounded range is sufficient because `payd` never allocates a deposit index
   at or above `pool_max_size`.
4. For **every** re-derived deposit address, replay all four histories below.
   Start without `min_timestamp` and keep sending the returned `fingerprint`
   on the next request until no fingerprint remains:

   - `GET /v1/accounts/{address}/transactions/trc20?only_to=true&limit=200`
   - `GET /v1/accounts/{address}/transactions/trc20?limit=200`
   - `GET /v1/accounts/{address}/transactions?only_to=true&limit=200`
   - `GET /v1/accounts/{address}/transactions?limit=200`

   The first and third calls reconstruct inbound TRC-20 and native TRX. The
   second and fourth calls are mandatory outbound passes and reconstruct
   ledger debits. Native TRX must not be omitted. A first page is not a full
   replay; cursor iteration is mandatory (DET-010/010c/011).
5. Before inserting a discovered txid, fetch
   `POST /wallet/gettransactioninfobyid`. Use the receipt's zero-based log-array
   position for each TRC-20 `Transfer`; use index `0` for a native
   `TransferContract`. Fetch the containing block for its block ID. Feed the
   resulting records through the same decoder/store ingest used by W-007 so
   `(txid, log_index)` remains idempotent and balances are recomputed exactly
   (DET-002a, DET-010a, DET-012, DB-004).
6. Mark chain-confirmed replayed transfers confirmed. There are no recovered
   orders to match, so inbound payments remain unattributed. Compare every
   reconstructed asset balance with the on-chain balance and resolve all drift
   before re-enabling withdrawals.

If the original `pool_max_size`, HD account, or derivation path is unknown, the
mnemonic alone cannot identify a finite address scan. Restore those deployment
settings from configuration backup before proceeding; do not guess them.

## Withdrawal loss boundary (OPS-014)

Withdrawals that were `broadcast`, `signing`, or `needs_operator` when the
database was lost are in an unknown state. Reconcile every affected source
address and known txid manually in Tronscan before issuing any new withdrawal
from that address. A missing local row is not evidence that a transaction was
never broadcast. Never re-sign or automatically retry an uncertain transfer.

## Required alerts (OPS-006, RL-006)

Load [`payd-alerts.yml`](payd-alerts.yml) (or equivalent rules) in the
operator's Prometheus rule configuration:

The critical rule fires immediately whenever
`payd_withdrawals_needs_operator > 0`; the quota rule warns after the projected
ratio remains at or above `0.60` for five minutes.
