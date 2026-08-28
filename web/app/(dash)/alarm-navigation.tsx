"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useEffect, useState } from "react";

import { AlarmCounter } from "@/components/data/alarm-counter";
import { paydRequest, isPaydError } from "@/lib/payd/browser-client";
import type { OperationalStats } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

const utcFormatter = new Intl.DateTimeFormat("en-GB", {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
  timeZone: "UTC",
});

function record(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function count(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

export function alarmCounts(stats: OperationalStats | undefined) {
  const values = stats ?? {};
  const payments = record(values.payments);
  const ipnDead = record(values.ipn_dead);
  const unattributed = count(payments?.unattributed);
  const orphaned = count(values.orphaned_unresolved);

  return {
    needsOperator: count(values.needs_operator),
    unattributed,
    orphaned,
    fundedTerminal: count(values.funded_terminal_unresolved),
    deadIpns: Object.values(ipnDead ?? {}).reduce<number>((total, value) => total + count(value), 0),
  };
}

function UtcClock() {
  const [time, setTime] = useState("");

  useEffect(() => {
    const update = () => setTime(utcFormatter.format(new Date()));
    update();
    const timer = window.setInterval(update, 1_000);
    return () => window.clearInterval(timer);
  }, []);

  return <p className="font-mono text-xs tabular-nums text-ink-secondary">UTC {time || "--:--:--"}</p>;
}

export function AlarmNavigation() {
  const stats = useQuery(paydQueryOptions({
    queryKey: queryKeys.stats(),
    queryFn: () => paydRequest<OperationalStats>(["stats"]),
    polling: { tier: "C" },
  }));
  const counts = alarmCounts(stats.data);
  const errorCode = stats.isError ? (isPaydError(stats.error) ? stats.error.code : "upstream_unreachable") : null;
  const errorMessage = errorCode === "rate_limited"
    ? "Stats refresh slowed (rate_limited)."
    : errorCode ? `Stats may be stale (${errorCode}).` : null;

  return (
    <div className="mt-auto border-t border-border-subtle p-3">
      <p className="mb-2 px-1 text-xs font-semibold uppercase tracking-wide text-ink-faint">Alarms</p>
      <div className="space-y-2">
        <Link className={`${counts.needsOperator > 0 ? "needs-operator" : ""} block rounded-sm focus-visible:outline-offset-2`} href="/withdrawals/needs-operator">
          <AlarmCounter label="needs_operator" count={counts.needsOperator} severity="critical" />
        </Link>
        <Link className="group relative block rounded-sm focus-visible:outline-offset-2" href="/payments/unattributed" aria-describedby="unattributed-breakdown">
          <AlarmCounter label="Unattributed payments" count={counts.unattributed + counts.orphaned} />
          <span id="unattributed-breakdown" className="sr-only absolute inset-x-1 bottom-full z-10 mb-1 border border-border-strong bg-raised px-2 py-1 text-xs text-ink-secondary group-hover:not-sr-only group-focus-visible:not-sr-only">
            {counts.unattributed} unattributed; {counts.orphaned} orphaned
          </span>
        </Link>
        <Link className="block rounded-sm focus-visible:outline-offset-2" href="/orders/funded-terminal">
          <AlarmCounter label="Funded terminal" count={counts.fundedTerminal} />
        </Link>
        <Link className="block rounded-sm focus-visible:outline-offset-2" href="/webhooks">
          <AlarmCounter label="Dead IPNs" count={counts.deadIpns} />
        </Link>
      </div>
      {errorMessage ? <p role="status" className="mt-2 text-xs text-severity-warning">⚠ {errorMessage}</p> : null}
      <div className="mt-4 border-t border-border-subtle px-1 pt-3"><UtcClock /></div>
    </div>
  );
}
