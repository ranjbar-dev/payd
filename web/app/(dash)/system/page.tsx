import { cookies } from "next/headers";

import { getEnv } from "@/lib/env";
import { verifySession } from "@/lib/session";

import { SystemDashboard } from "../system-dashboard";

export default async function SystemPage() {
  // WSYS-054: the browser never receives PAYD_BASE_URL itself — only its
  // hostname, read here in a server component the same way app/layout.tsx
  // already reads TRONSCAN_BASE_URL for Providers, and the way
  // app/(dash)/layout.tsx already reads the session cookie server-side.
  // PAYD_BASE_URL is validated loopback-only by lib/env.ts's getEnv() (127.0.0.1,
  // ::1, or localhost), so the host itself is never a secret, but the raw value
  // — which could carry a port or, in a future config, more — is still never
  // passed down; only `new URL(...).hostname` crosses into the client tree.
  const paydHost = new URL(getEnv().PAYD_BASE_URL).hostname;

  // WSYS-052: the dashboard session's issue and expiry times. app/(dash)/layout.tsx
  // already verifies this same cookie once, to build ScopesProvider and the nav
  // shell's scope banner, but its SessionExpiryProvider (app/providers.tsx) only
  // carries `exp`, not `iat` — there is no existing path for the issue time to
  // reach a client component, and adding one would mean editing layout.tsx or
  // providers.tsx, both out of scope for this task. Reading the same cookie again
  // here, independently, avoids that: by the time this page renders at all, the
  // layout above it has already required this cookie to verify, so this is a
  // second read of an already-required-valid value, not a second source of
  // truth for whether the session is valid.
  const session = verifySession((await cookies()).get("payd_session")?.value);

  return (
    <SystemDashboard
      paydHost={paydHost}
      sessionIssuedAt={session ? Math.floor(session.iat / 1000) : null}
      sessionExpiresAt={session ? Math.floor(session.exp / 1000) : null}
    />
  );
}
