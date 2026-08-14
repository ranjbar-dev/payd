ROLE: PLATFORM
TASK-ID: 17a-session-expiry
GOAL: Implement AUTH-023 — warn the operator five minutes before the session expires, and never silently discard an in-progress form when it does.

You are working in the repository at C:\Users\root\Desktop\tron-payment-proccesor.
The web app is `web/`. Run every command from `web/`.

WHY THIS EXISTS AND WHY NOW: the WP1 gate found AUTH-023 implemented nowhere.
Today the cost is mild — a session expires, the next navigation lands on login.
The next task built is the WITHDRAWAL WIZARD, three steps ending in a payd TOTP
code, and there AUTH-023 is a money requirement: a wizard that silently loses its
inputs to a session timeout invites a hurried, unverified re-entry of a payout.
Build it before the wizard, not after.

READ FIRST, FULLY:
  web/AGENTS.md
  web/docs/specs/04-auth-and-session.md — AUTH-020..AUTH-030 especially, and
    AUTH-023 verbatim.
  web/docs/specs/03-architecture-and-bff.md — how the session cookie and the
    proxy relate.
  web/lib/session.ts — the session model. Note what the cookie contains: an
    issue timestamp, an expiry, a random id, and NOTHING ELSE (AUTH-021).
  web/app/providers.tsx — the existing client-side context provider, fed from a
    server component.
  web/app/(dash)/layout.tsx — where an invalid session already redirects.
  web/app/(dash)/nav-shell.tsx — the permanent chrome.

YOU MAY CREATE OR MODIFY ONLY THESE PATHS:
  web/app/session-expiry.tsx          (or a name you prefer, same location)
  web/app/providers.tsx
  web/app/(dash)/layout.tsx
  web/app/(dash)/nav-shell.tsx
  web/lib/session.ts                  — ONLY if the expiry must be exposed to the
                                        client tree, and ONLY additively. You MUST
                                        NOT weaken or change any existing check.
  PLUS the build configuration when required.
Everything else belongs to another agent. If you need a change outside this
list, STOP and report it instead of making it.

WHAT TO BUILD:

  1. THE WARNING. Five minutes before the session expires, the operator is told,
     in the permanent chrome where it cannot be missed, with the remaining time
     visible and counting. It must be dismissible only in the sense that it stops
     being alarming once acted on — it MUST NOT be closable in a way that hides
     an imminent expiry.

  2. THE EXPIRY ITSELF. When the session expires, the operator is told plainly
     that it has expired and that they must log in again. What they were doing is
     NOT thrown away silently: any in-progress form state stays on screen and
     stays readable. Do not clear inputs, do not navigate away automatically, do
     not unmount a form.

  3. THE MECHANISM. The expiry timestamp is already in the session and the server
     already knows it. Pass it down from a server component exactly as
     `TRONSCAN_BASE_URL` is passed today — read server-side, handed to the client
     tree through context. DO NOT add a `NEXT_PUBLIC_` variable (WST-020, and a
     grep enforces it). DO NOT put anything in the cookie that is not there
     already (AUTH-021). DO NOT poll an endpoint to ask whether the session is
     alive: the expiry is a known timestamp, and a countdown needs no network.

  4. HOW OTHER PAGES USE IT. Expose whatever a form needs to ask "is the session
     about to expire?" so the withdrawal wizard can act on it next task. Keep
     that surface small and obvious — a hook or a context value, not a framework.
     Do not build wizard-specific behaviour here.

WHAT NOT TO BUILD:
  - No auto-refresh, silent re-issue, or "keep me logged in" control. The session
    lifetime is a security property; extending it because the tab is open is
    exactly what it exists to prevent. If you believe a renewal flow is needed,
    REPORT IT — do not build it.
  - No modal that blocks the screen at expiry. An operator mid-incident needs to
    read what is on the page, and a blocking modal at minute zero is how the
    inputs get lost.
  - No countdown that fires network requests.

THE SIX INVARIANTS:
  INV-1  No retry, resume, or automatic re-send on any mutation path.
  INV-2  Money is a string, start to finish. `npm test` fails the build on any
         coercion touching a money-named identifier; the session TTL is already
         the one allowlisted numeric conversion and you MUST NOT add another
         without saying so. YOU MAY NOT EDIT ANY TEST.
  INV-3  `confirmed` and `pending` balances are never merged.
  INV-4  NO SECRET, TOTP CODE OR API KEY REACHES THE BROWSER. An expiry timestamp
         is not a secret; the session id and the session secret are. Pass the
         timestamp only.
  INV-5  No business logic in the client. Whether a request is authorised remains
         the server's answer — this warning is a courtesy, never an authority.
         The proxy must still reject an expired session on its own, and
         `lib/session-expiry.test.ts` proves it does. Keep that green.
  INV-6  Anything scoped to a UTC day is labelled UTC in visible text.

DESIGN BRIEF: dark mode default, density over whitespace, the six-level severity
palette only, icon alongside warning and critical, no decorative motion, keyboard
reachable with visible focus rings. The five-minute warning is WARNING severity;
actual expiry is CRITICAL. Neither may look like `needs_operator`, which is the
loudest thing in the interface and must stay unique to money in an unknown state.

DONE WHEN:
  - `./node_modules/.bin/tsc --noEmit` clean (NOT `npx tsc`)
  - `npm run lint` clean
  - `npm test` green — 4/4, no test edited
  - `npm run build` succeeds and `.env` is back where it was (it holds three
    required-but-empty variables that beat process env; move it aside, then MOVE
    IT BACK)
  - AUTH-023 is satisfied and you can point to where
  - `grep -rn "NEXT_PUBLIC_"` over your files finds nothing

REPORT AT THE END:
  - files changed
  - AUTH-023 → file:line
  - the exact surface you exposed for a form to ask about imminent expiry, so the
    next task can use it without guessing
  - anything you could not do, and why
