"use client";

import { focusManager, onlineManager, QueryClientProvider } from "@tanstack/react-query";
import { createContext, useContext, useEffect, useState } from "react";

import { createPaydQueryClient, getRateLimitUntil, subscribeRateLimit } from "@/lib/query";

// The explorer origin is public but still server-only (WST-020): it reaches the
// browser by being passed down from a server component, never through a public
// browser-exposed env prefix. Held in context because every page that renders a txid
// needs it, and threading it through each page's props invites one page to
// quietly fall back to a hardcoded mainnet link (UI-033).
const TronscanContext = createContext<string | null>(null);
const SessionExpiryContext = createContext<SessionExpiry | null>(null);
// AUTH-032: which control needs disabling and why is server-side knowledge (the
// scopes cached from `GET /auth/whoami`, `lib/session.ts`'s `getSessionWhoami`).
// Same pattern as TronscanContext: a plain context fed once from a server component
// (`app/(dash)/layout.tsx`), never fetched by the client and never a `NEXT_PUBLIC_`
// variable. `null` distinguishes "no provider mounted" (a bug) from "zero scopes"
// (a real, valid key state), so `useScopes` only throws on the former.
const ScopesContext = createContext<string[] | null>(null);

export type SessionExpiry = Readonly<{
  expiresAt: number;
  remainingMs: number;
  isExpiringSoon: boolean;
  isExpired: boolean;
}>;

export function useTronscanBaseUrl(): string {
  const value = useContext(TronscanContext);
  if (!value) throw new Error("useTronscanBaseUrl requires Providers with a tronscanBaseUrl");
  return value;
}

export function useSessionExpiry(): SessionExpiry {
  const value = useContext(SessionExpiryContext);
  if (!value) throw new Error("useSessionExpiry requires SessionExpiryProvider");
  return value;
}

export function useScopes(): string[] {
  const value = useContext(ScopesContext);
  if (!value) throw new Error("useScopes requires ScopesProvider");
  return value;
}

// Mounted from `app/(dash)/layout.tsx`, the same place that already calls
// `getSessionWhoami(session.id)` once to build `ScopeBanner` — this is additive to
// that call, not a second fetch and not a restructuring of the banner.
export function ScopesProvider({ children, scopes }: Readonly<{ children: React.ReactNode; scopes: string[] }>) {
  return <ScopesContext.Provider value={scopes}>{children}</ScopesContext.Provider>;
}

export function SessionExpiryProvider({ children, expiresAt }: Readonly<{ children: React.ReactNode; expiresAt: number }>) {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const update = () => setNow(Date.now());
    const timer = window.setInterval(update, 1_000);
    document.addEventListener("visibilitychange", update);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", update);
    };
  }, []);
  const remainingMs = Math.max(0, expiresAt - now);
  return <SessionExpiryContext.Provider value={{ expiresAt, remainingMs, isExpiringSoon: remainingMs > 0 && remainingMs <= 300_000, isExpired: remainingMs === 0 }}>{children}</SessionExpiryContext.Provider>;
}

function RateLimitNotice() {
  const [until, setUntil] = useState(getRateLimitUntil);

  useEffect(() => subscribeRateLimit(() => setUntil(getRateLimitUntil())), []);
  useEffect(() => {
    if (!until) return;
    const timer = window.setTimeout(() => setUntil(getRateLimitUntil()), Math.max(0, until - Date.now()));
    return () => window.clearTimeout(timer);
  }, [until]);

  return until ? <p role="status">Refresh has slowed for two minutes because payd rate limit was reached.</p> : null;
}

export function Providers({ children, tronscanBaseUrl }: Readonly<{ children: React.ReactNode; tronscanBaseUrl: string }>) {
  const [queryClient] = useState(createPaydQueryClient);

  useEffect(() => focusManager.setEventListener((setFocused) => {
    const update = () => setFocused(document.visibilityState === "visible");
    window.addEventListener("focus", update);
    document.addEventListener("visibilitychange", update);
    update();
    return () => {
      window.removeEventListener("focus", update);
      document.removeEventListener("visibilitychange", update);
    };
  }), []);

  useEffect(() => onlineManager.setEventListener((setOnline) => {
    const update = () => setOnline(navigator.onLine);
    window.addEventListener("online", update);
    window.addEventListener("offline", update);
    update();
    return () => {
      window.removeEventListener("online", update);
      window.removeEventListener("offline", update);
    };
  }), []);

  return <TronscanContext.Provider value={tronscanBaseUrl}><QueryClientProvider client={queryClient}><RateLimitNotice />{children}</QueryClientProvider></TronscanContext.Provider>;
}
