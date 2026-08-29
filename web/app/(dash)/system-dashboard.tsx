"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Server } from "lucide-react";

import { SystemAssets } from "./system-assets";
import { SystemAudit } from "./system-audit";
import { SystemConfig } from "./system-config";
import { SystemHealth } from "./system-health";
import { SystemQuota } from "./system-quota";
import { SystemSession } from "./system-session";
import { SystemWorkers } from "./system-workers";

// Tab navigation: a single `/system` route with `?tab=`, not sub-routes. `23-
// reports` set the sub-route precedent (`/reports`, `/reports/fees`) for its two
// tabs, but overview-dashboard.tsx already links into this page as
// `/system?tab=quota` and `/system?tab=workers` (its Quota and Workers cards) —
// those links predate this task, so `?tab=` is the pattern already load-bearing
// elsewhere in the app for this exact page, and DAT-026 (filter/view state lives
// in the URL) covers a tab selection as much as a filter. Applied consistently
// across all seven tabs below, never mixed with sub-routes.
const TABS = [
  ["workers", "Workers"],
  ["quota", "Quota"],
  ["config", "Config"],
  ["assets", "Assets"],
  ["audit", "Audit"],
  ["session", "Session"],
  ["health", "Health"],
] as const;

type Tab = (typeof TABS)[number][0];

function isTab(value: string | null): value is Tab {
  return TABS.some(([id]) => id === value);
}

export function SystemDashboard({
  paydHost,
  sessionIssuedAt,
  sessionExpiresAt,
}: Readonly<{
  paydHost: string;
  sessionIssuedAt: number | null;
  sessionExpiresAt: number | null;
}>) {
  const searchParams = useSearchParams();
  const requestedTab = searchParams.get("tab");
  const tab: Tab = isTab(requestedTab) ? requestedTab : "workers";

  return (
    <main className="page">
      <header>
        <p className="page-kicker"><Server aria-hidden="true" size={14} strokeWidth={1.75} />Operations / System</p>
        <h1 className="page-title mt-1">System and audit</h1>
        <p className="mt-1 text-sm text-ink-secondary">
          Everything needed to diagnose payd without shell access, plus the compliance trail. Almost all of it is
          tier D — manual refresh. This is a page you open when something is wrong, not one you watch.
        </p>
      </header>
      <nav className="flex flex-wrap gap-1 border-b border-border-subtle" aria-label="System tabs">
        {TABS.map(([id, label]) => (
          <Link
            key={id}
            href={`/system?tab=${id}`}
            aria-current={id === tab ? "page" : undefined}
            className={id === tab ? "cursor-pointer border-b-2 border-accent px-3 py-2 text-[13px] font-medium text-ink transition-colors duration-150 focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]" : "cursor-pointer px-3 py-2 text-[13px] text-ink-secondary transition-colors duration-150 hover:bg-raised hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"}
          >
            {label}
          </Link>
        ))}
      </nav>
      {tab === "workers" ? <SystemWorkers /> : null}
      {tab === "quota" ? <SystemQuota /> : null}
      {tab === "config" ? <SystemConfig /> : null}
      {tab === "assets" ? <SystemAssets /> : null}
      {tab === "audit" ? <SystemAudit /> : null}
      {tab === "session" ? <SystemSession paydHost={paydHost} issuedAt={sessionIssuedAt} expiresAt={sessionExpiresAt} /> : null}
      {tab === "health" ? <SystemHealth paydHost={paydHost} /> : null}
    </main>
  );
}
