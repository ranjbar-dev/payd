import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { getSessionWhoami, verifySession } from "@/lib/session";

import { SessionExpiryProvider } from "../providers";

import { NavShell } from "./nav-shell";
import { ScopeBanner } from "./scope-banner";

export default async function DashboardLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  const session = verifySession((await cookies()).get("payd_session")?.value);
  if (!session) redirect("/login");

  return <SessionExpiryProvider expiresAt={session.exp}><NavShell scopeBanner={<ScopeBanner whoami={getSessionWhoami(session.id)} />}>{children}</NavShell></SessionExpiryProvider>;
}
