# 14. Key management

**Part of:** Tron & TRC-20 Merchant Payment Service — Design Specification v1.2 (2026-08-07)
**Source:** original §14
**ID prefixes in this file:** `KEY-*`
**Related:** [`02-tech-stack-and-dependencies.md`](02-tech-stack-and-dependencies.md) (`hd-wallet` library), [`13-withdrawal-engine.md`](13-withdrawal-engine.md) (uses the loaded wallet to sign), [`17-operations.md`](17-operations.md) (OPS-014 recovery involves the seed)

---

| ID | Requirement |
|---|---|
| KEY-001 | A separate one-shot binary `seedtool` MUST read the BIP-39 mnemonic from **stdin** into a `memguard` buffer, never a Go string |
| KEY-002 | `seedtool` MUST encrypt the mnemonic with an AEAD (NaCl secretbox or age), using a key compiled into the binary via `go:embed` |
| KEY-003 | The embedded key MUST be a 32-byte random value generated per deployment, held in a file that is `.gitignore`d and never committed |
| KEY-004 | `seedtool` MUST write the encrypted blob to a file (default `seed.age`) with mode `0600` |
| KEY-005 | `payd` MUST decrypt `seed_file` at startup using the same embedded key and load it via `hdwallet.FromMnemonicBuffer`, which takes ownership and destroys the buffer |
| KEY-006 | `payd` MUST call `defer memguard.Purge()` at process exit and `w.Destroy()` on the wallet |
| KEY-007 | The mnemonic MUST NOT be readable from environment variables, logs, API responses, or error messages under any circumstance |
| KEY-008 | `seedtool` MUST also print the TRX account xpub (`AccountXPub(hdwallet.TRX, account)`) for operator record-keeping |

**Documented residual risk.** `go:embed` data lands in the binary's `.rodata` section and is extractable by anyone holding the binary. This design protects against the encrypted seed file leaking *alone* — a stray backup, a bad rsync, a database dump, an accidental commit. It does not protect against an attacker with access to the server, who holds both halves. The operator has assessed this as acceptable given the production network's security posture. Rotating the key means rebuilding the binary and re-running `seedtool`.
