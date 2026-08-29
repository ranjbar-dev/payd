"use client";

import { useQuery } from "@tanstack/react-query";
import { Loader2, LogOut } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";

import { useTronscanBaseUrl } from "@/app/providers";
import { Timestamp } from "@/components/data/timestamp";
import { paydRequest } from "@/lib/payd/browser-client";
import type { WhoamiResponse } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

import { csrfToken, ErrorNotice } from "./system-shared";

// WSYS-051/AUTH-033: mirrors app/(dash)/scope-banner.tsx's scope -> disabled-
// pages mapping exactly. scope-banner.tsx does not export it and is out of scope
// for this task to change, so this is a documented local copy (same convention
// reports-dashboard.tsx already uses for its own re-implemented helpers) — kept
// identical so the persistent banner and this tab never disagree about what a
// missing scope disables.
const SCOPE_PAGES: Record<string, string> = {
  "orders:read": "Orders, Payments, Webhooks, Reports",
  "orders:write": "Order and payment actions",
  "wallets:read": "Addresses, Resources, Reports, System",
  "wallets:write": "Address actions",
  "withdrawals:read": "Withdrawals and reports",
  "withdrawals:write": "Withdrawal actions",
  "resources:write": "Resource actions",
  "admin:read": "System",
};

// WSYS-054: derives the network name from the Tronscan host the same way any
// other page displaying network identity would — there is no existing shared
// helper for this (useTronscanBaseUrl only supplies the origin itself). An
// unrecognised host still renders verbatim rather than being guessed at or
// hidden (UI-020/WOVW-012b's fallback rule).
function tronNetwork(tronscanBaseUrl: string): string {
  const host = new URL(tronscanBaseUrl).hostname;
  if (host === "tronscan.org" || host === "www.tronscan.org") return "Mainnet";
  if (host.startsWith("nile.")) return "Nile testnet";
  if (host.startsWith("shasta.")) return "Shasta testnet";
  return host;
}

export function SystemSession({
  paydHost,
  issuedAt,
  expiresAt,
}: Readonly<{
  paydHost: string;
  issuedAt: number | null;
  expiresAt: number | null;
}>) {
  const router = useRouter();
  const tronscanBaseUrl = useTronscanBaseUrl();
  const [loggingOut, setLoggingOut] = useState(false);
  const [logoutError, setLogoutError] = useState<string | null>(null);
  const whoami = useQuery(paydQueryOptions({ queryKey: queryKeys.whoami(), queryFn: () => paydRequest<WhoamiResponse>(["auth", "whoami"]), polling: { tier: "D" } }));
  // WSYS-050: rendered in the order /auth/whoami returned it. The backend already
  // sorts scopes before responding (backend/internal/api/identity.go), so this
  // does not re-sort — it just does not undo the ordering the response arrived
  // in, the same "never re-sort a backend-ordered list" principle as UI-043.
  const scopes = whoami.data?.scopes ?? [];
  const missing = Object.keys(SCOPE_PAGES).filter((scope) => !scopes.includes(scope));

  // INV-1: a deliberate human click that ends the session — not a fund-moving
  // action, not auto-retried on failure (AUTH-024).
  const logout = async () => {
    setLoggingOut(true);
    setLogoutError(null);
    try {
      const csrf = csrfToken();
      const response = await fetch("/api/auth/logout", {
        method: "POST",
        headers: csrf ? { "x-csrf-token": csrf } : {},
        credentials: "same-origin",
        cache: "no-store",
      });
      if (!response.ok) {
        setLogoutError(`Logout was not confirmed (status ${response.status}). Close this tab or clear cookies to end the session another way.`);
        setLoggingOut(false);
        return;
      }
      router.push("/login");
    } catch {
      setLogoutError("The logout request failed to reach the dashboard server. Close this tab or clear cookies to end the session another way.");
      setLoggingOut(false);
    }
  };

  return (
    <section className="space-y-4">
      <div className="card">
        <h2 className="card-title">payd key</h2>
        <p className="mt-1 text-xs text-ink-faint">GET /auth/whoami</p>
        {whoami.data ? (
          <dl className="mt-3 grid gap-3 sm:grid-cols-2">
            <div>
              <dt className="text-ink-faint text-[11px] uppercase tracking-wide">Key name</dt>
              <dd className="mt-0.5 font-mono">{whoami.data.key_name}</dd>
            </div>
            <div>
              <dt className="text-ink-faint text-[11px] uppercase tracking-wide">Scopes</dt>
              <dd className="mt-0.5">
                {scopes.length ? (
                  <ul className="flex flex-wrap gap-1.5">{scopes.map((scope) => <li key={scope} className="border border-border-strong px-1.5 py-0.5 font-mono text-xs">{scope}</li>)}</ul>
                ) : (
                  <span className="text-severity-warning">None</span>
                )}
              </dd>
            </div>
          </dl>
        ) : (
          !whoami.isError ? <div className="mt-3 h-8 animate-pulse bg-raised" aria-label="Loading key identity" /> : null
        )}
        <ErrorNotice error={whoami.isError ? whoami.error : null} updatedAt={whoami.dataUpdatedAt} onReload={() => void whoami.refetch()} />
      </div>

      {missing.length ? (
        <div className="card border-severity-warning bg-[var(--severity-warning-bg)] text-sm" role="alert">
          <h2 className="card-title text-severity-warning">Missing scopes</h2>
          <ul className="mt-2 space-y-1">
            {missing.map((scope) => (
              <li key={scope}>
                <code className="font-mono text-ink">{scope}</code> — disables {SCOPE_PAGES[scope]}
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      <div className="card">
        <h2 className="card-title">Dashboard session</h2>
        <dl className="mt-3 grid gap-3 sm:grid-cols-2">
          <div>
            <dt className="text-ink-faint text-[11px] uppercase tracking-wide">Issued</dt>
            <dd className="mt-0.5 text-right font-mono tabular-nums"><Timestamp seconds={issuedAt} /></dd>
          </div>
          <div>
            <dt className="text-ink-faint text-[11px] uppercase tracking-wide">Expires</dt>
            <dd className="mt-0.5 text-right font-mono tabular-nums"><Timestamp seconds={expiresAt} /></dd>
          </div>
        </dl>
        <p className="mt-3 text-sm text-ink-secondary">
          Sessions expire after a fixed absolute lifetime — there is no sliding renewal, so an idle tab left open does
          not extend it (AUTH-022).
        </p>
        <button
          type="button"
          disabled={loggingOut}
          onClick={() => void logout()}
          className="btn btn-secondary mt-3"
        >
          {loggingOut ? <Loader2 aria-hidden="true" size={14} strokeWidth={1.75} className="animate-spin" /> : <LogOut aria-hidden="true" size={14} strokeWidth={1.75} />}{loggingOut ? "Logging out…" : "Log out"}
        </button>
        {logoutError ? <p className="mt-2 text-severity-warning" role="alert">{logoutError}</p> : null}
      </div>

      <div className="card text-sm">
        <h2 className="card-title">Two different codes</h2>
        <p className="mt-2 text-ink-secondary">
          The <strong>dashboard code</strong> is entered once, at login, and verified by this Next.js server against{" "}
          <code className="font-mono text-xs">DASH_TOTP_SECRET</code> — it protects access to the dashboard itself. The{" "}
          <strong>payd code</strong> is a separate, single-use code entered fresh at the moment of every fund-moving
          action, verified by payd against its own secret — it authorises that one specific transaction. A valid
          dashboard session is never sufficient to move funds on its own (AUTH-002): the two codes are never the same
          value, and a 401 that names neither usually means the wrong code was typed into the wrong prompt (AUTH-003).
        </p>
      </div>

      <div className="card text-sm">
        <h2 className="card-title">Deployment identity</h2>
        <dl className="mt-3 grid gap-3 sm:grid-cols-2">
          <div>
            <dt className="text-ink-faint text-[11px] uppercase tracking-wide">payd host</dt>
            <dd className="mt-0.5 font-mono">{paydHost}</dd>
          </div>
          <div>
            <dt className="text-ink-faint text-[11px] uppercase tracking-wide">Tronscan network</dt>
            <dd className="mt-0.5">
              {tronNetwork(tronscanBaseUrl)} <span className="text-ink-faint">({new URL(tronscanBaseUrl).hostname})</span>
            </dd>
          </div>
        </dl>
        <p className="mt-2 text-ink-secondary">
          A dashboard that looks identical on mainnet and Nile is how a real payout gets made while believing it is a
          test — check both figures above before authorising anything (WSYS-054).
        </p>
      </div>
    </section>
  );
}
