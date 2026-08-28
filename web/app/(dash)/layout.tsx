import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { getSessionWhoami, verifySession } from "@/lib/session";

import { ScopesProvider, SessionExpiryProvider } from "../providers";

import { NavShell } from "./nav-shell";
import { ScopeBanner } from "./scope-banner";

export default async function DashboardLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  const session = verifySession((await cookies()).get("payd_session")?.value);
  if (!session) redirect("/login");

  // Single whoami read for the session, same as before this task — ScopeBanner keeps
  // its existing coarse page-level warning, and ScopesProvider (AUTH-032) adds the
  // per-control scope data next to it without a second call or a restructuring of
  // either. An unverified key (whoami missing) is treated as zero scopes, never as
  // "every control is fine": a control that cannot prove it has the scope stays
  // disabled, it does not fail open.
  const whoami = getSessionWhoami(session.id);
  return <SessionExpiryProvider expiresAt={session.exp}><ScopesProvider scopes={whoami?.scopes ?? []}><NavShell scopeBanner={<ScopeBanner whoami={whoami} />}>{children}</NavShell></ScopesProvider></SessionExpiryProvider>;
}
