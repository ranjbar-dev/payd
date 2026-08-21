# Autopilot halted

## Reason

H8: `codex exec` could not be invoked. This is not a task failure — no model
connection was ever established, so no code was written and no attempt should be
charged against `23-reports`.

## Evidence

`web/.codex/logs/23-reports.log`, first lines:

```
2026-08-21T16:13:40.234034Z ERROR codex_models_manager::manager: failed to refresh available models: unexpected status 401 Unauthorized: ... auth error code: token_revoked
...
2026-08-21T16:13:43.590601Z ERROR codex_login::auth::manager: Failed to refresh token: 401 Unauthorized: {
  "error": {
    "message": "Your session has ended. Please log in again.",
    "type": "invalid_request_error",
    "param": null,
    "code": "refresh_token_invalidated"
  }
}
...
ERROR: Your access token could not be refreshed because your refresh token was revoked. Please log out and sign in again.
```

`git diff --stat` after the spawn showed only the orchestrator's own pre-spawn
edits (`backend/internal/api/openapi.yaml`, `web/app/api/payd/[...path]/route.ts`,
`web/.codex/LEDGER.md`) — nothing from the sub-agent. Confirmed no code was written.

## What is NOT affected

WP1, WP2, and WP3 are complete and gated (see ledger `## Gate log`). The
`23-reports` brief at `web/.codex/briefs/23-reports.md` is fully written and does
not need to be redone — it already accounts for two orchestrator contract repairs
made just before this spawn (the `/reports/volume` schema tightened in
`openapi.yaml`, and the proxy's GET timeout widened to 120s for `/export/*`
routes in `route.ts`). Both of those edits are real, validated (`tsc` clean), and
should be kept regardless of how this halt is resolved.

## Next action

A human needs to re-authenticate the Codex CLI — this is an interactive OAuth
flow the orchestrator cannot perform:

```
codex logout
codex login
```

Then verify with `codex exec --help` or a trivial `codex exec "echo ok"` before
resuming. Once auth is restored, re-run the same spawn command recorded in this
ledger's `spawn-invocation` line against the existing brief:

```
codex exec --dangerously-bypass-approvals-and-sandbox - < web/.codex/briefs/23-reports.md > web/.codex/logs/23-reports.log 2>&1
```

No ledger rows need to be rolled back; `23-reports` is HALTED with 1 attempt
recorded, but that attempt did not consume any of its remediation budget since
no work was produced (per AUTOPILOT.md's own rule for a failure a sub-agent
"cannot reach" — this is the same class as the missing-field halts earlier in
this run, just at the transport layer instead of the API contract).
