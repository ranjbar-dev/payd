# Autopilot ledger

spawn-invocation: not started (halted before first spawn)
current-phase: WP1

| task-id | role | status | attempts | notes |
|---|---|---|---|---|
| 01-scaffold | SCAFFOLD | HALTED | 0 | H9: pre-existing working-tree changes detected before first spawn. |
| 02-types | PLATFORM | PENDING | 0 | |
| 03-proxy | PLATFORM | PENDING | 0 | |
| 04-session | PLATFORM | PENDING | 0 | |
| 05-query | PLATFORM | PENDING | 0 | |
| 06-design-tokens | DESIGN | PENDING | 0 | |
| 07-components | DESIGN | PENDING | 0 | |
| 08-shell | PAGE | PENDING | 0 | |
| 09-overview | PAGE | PENDING | 0 | |
| 10-orders-read | PAGE | PENDING | 0 | |
| 11-payments-read | PAGE | PENDING | 0 | |
| 12-addresses-read | PAGE | PENDING | 0 | |
| 13-withdrawals-read | PAGE | PENDING | 0 | |
| 14-orders-mut | PAGE | PENDING | 0 | |
| 15-payments-work | PAGE | PENDING | 0 | |
| 16-addresses-dis | PAGE | PENDING | 0 | |
| 17-webhooks | PAGE | PENDING | 0 | |
| 18-wd-wizard | PAGE | PENDING | 0 | |
| 19-wd-resolve | PAGE | PENDING | 0 | |
| 20-addr-totp | PAGE | PENDING | 0 | |
| 21-resources | PAGE | PENDING | 0 | |
| 22-noretry-audit | AUDITOR | PENDING | 0 | |
| 23-reports | PAGE | PENDING | 0 | |
| 24-system | PAGE | PENDING | 0 | |
| 25-polish | PAGE | PENDING | 0 | |
| 26-coverage | AUDITOR | PENDING | 0 | |
| WP1-GATE | GATE | PENDING | 0 | |
| WP2-GATE | GATE | PENDING | 0 | |
| WP3-GATE | GATE | PENDING | 0 | |
| WP4-GATE | GATE | PENDING | 0 | |
| FINAL | REPORT | PENDING | 0 | |

## Gate log

No gates run.

## Blocked / halted

H9 — `git status --short` was non-empty before task 01. It includes a large pre-existing rename set under `backend/`, an untracked root `AGENTS.md`, an untracked `backend/internal/seed/seed.key`, and untracked `web/` content. No sub-agent was spawned and no application code was changed by this run.
