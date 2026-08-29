# AGENTS.md (web)

Single source of truth for every coding agent (Codex, Claude Code, others):
**[`CLAUDE.md`](CLAUDE.md)** in this directory. Read it before touching code.

It covers dev / build / lint / test commands, the BFF-proxy architecture, the
`lib/payd` API layer, route layout, and the no-retry / decimal-string
invariants. Root context: [`../CLAUDE.md`](../CLAUDE.md).

<!-- BEGIN:nextjs-agent-rules -->

# This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` (resolved from this file's directory; in monorepos the `next` package may not be visible from the repo root) before writing any code. Heed deprecation notices.

This block is written and re-added by `next dev` — verify at `node_modules/next/dist/server/lib/generate-agent-files.js`. Removing it from a diff only re-creates the uncommitted change; committing it with your work keeps the tree clean.

<!-- END:nextjs-agent-rules -->
