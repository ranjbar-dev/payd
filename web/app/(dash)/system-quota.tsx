"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";

import { DataTable } from "@/components/data/data-table";
import { EmptyState } from "@/components/data/empty-state";
import { Timestamp } from "@/components/data/timestamp";
import { paydRequest } from "@/lib/payd/browser-client";
import type { ChainQuotaResponse, QuotaHistoryEntry } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

import { ErrorNotice } from "./system-shared";

// WSYS-013: the /readyz threshold (backend OPS-001, RL-006), marked here as a
// fixed point of reference, never recomputed from other figures.
const READINESS_THRESHOLD = 90;

type Row = { entry: QuotaHistoryEntry; previous: QuotaHistoryEntry | undefined };

// WSYS-012: `requests`/`day_start` are plain integers (not amount fields, so
// UI-001/INV-2 do not apply), and this is a direct comparison of two
// already-rendered figures for a trend arrow — never a recomputed percentage or
// a health verdict (INV-5).
function trend({ entry, previous }: Row): string {
  if (!previous) return "—";
  if (entry.requests > previous.requests) return "↑ up from the prior day";
  if (entry.requests < previous.requests) return "↓ down from the prior day";
  return "→ unchanged from the prior day";
}

export function SystemQuota() {
  const quota = useQuery(paydQueryOptions({ queryKey: queryKeys.chain.quota(), queryFn: () => paydRequest<ChainQuotaResponse>(["chain", "quota"]), polling: { tier: "D" } }));
  const history = quota.data?.history ?? [];
  const rows: Row[] = history.map((entry, index) => ({ entry, previous: history[index - 1] }));
  // INV-2/INV-5: percent_used is a number, not a string, so a direct comparison is
  // permitted; nothing here recomputes it from requests_today/daily_request_quota.
  // The comparison only decides which threshold marker to show, not a verdict.
  const atOrAboveThreshold = quota.data != null && quota.data.percent_used >= READINESS_THRESHOLD;

  return (
    <section className="space-y-4">
      <p className="text-sm text-ink-secondary">
        TronGrid API usage, counted per key in fixed UTC-day windows. Consumption grows monotonically as the set of
        balance-holding addresses grows (backend RES-001a) — a rising trend here, or a sudden drop to near-zero, is
        usually the first symptom of a detection outage, before anything else notices (WSYS-012).
      </p>
      {quota.data ? (
        <div className="card">
          <h2 className="card-title">Quota status</h2>
          <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-sm sm:grid-cols-4">
            <div>
              <dt className="text-xs uppercase tracking-wide text-ink-faint">Requests today (UTC)</dt>
              <dd className="mt-0.5 text-right font-mono tabular-nums">{quota.data.requests_today}</dd>
            </div>
            <div>
              <dt className="text-xs uppercase tracking-wide text-ink-faint">Daily quota</dt>
              <dd className="mt-0.5 text-right font-mono tabular-nums">{quota.data.daily_request_quota}</dd>
            </div>
            <div>
              <dt className="text-xs uppercase tracking-wide text-ink-faint">Percent used</dt>
              <dd className={atOrAboveThreshold ? "mt-0.5 text-right font-mono tabular-nums text-severity-critical" : "mt-0.5 text-right font-mono tabular-nums"}>{quota.data.percent_used}%</dd>
            </div>
            <div>
              <dt className="text-xs uppercase tracking-wide text-ink-faint">Readiness threshold</dt>
              <dd className="mt-0.5 text-right font-mono tabular-nums">{READINESS_THRESHOLD}%</dd>
            </div>
          </dl>
          <p className={atOrAboveThreshold ? "mt-3 text-sm text-severity-critical" : "mt-3 text-sm text-ink-secondary"}>
            {atOrAboveThreshold ? "⚠ " : ""}At or above {READINESS_THRESHOLD}% projected usage, <code className="font-mono text-xs">/readyz</code> reports{" "}
            <code className="font-mono text-xs">trongrid_quota_projection</code> and the service is degraded (backend OPS-001, RL-006). This marks the
            threshold; it is not a verdict computed here — the figure above is exactly what payd returned.
          </p>
        </div>
      ) : (
        <div className="card animate-pulse" aria-label="Loading quota"><div className="h-3 w-28 bg-raised" /><div className="mt-4 h-12 bg-raised" /></div>
      )}
      <ErrorNotice error={quota.isError ? quota.error : null} updatedAt={quota.dataUpdatedAt} onReload={() => void quota.refetch()} />

      <div className="card">
        <h2 className="card-title">Seven-day history (UTC)</h2>
        <p className="mt-1 text-sm text-ink-secondary">
          Every day below is a UTC calendar day (UI-010), in the order payd returned it — oldest first — and this
          table is never re-sorted client-side (UI-043).
        </p>
        <div className="mt-2">
          <DataTable
            columns={[
              { id: "day", label: "Day (UTC)" },
              { id: "requests", label: "Requests" },
              { id: "trend", label: "Trend vs prior day" },
            ]}
            rows={rows}
            rowKey={(row) => String(row.entry.day_start)}
            renderRow={(row) => (
              <>
                <td className="td text-right font-mono tabular-nums"><Timestamp seconds={row.entry.day_start} variant="utc-day" /></td>
                <td className="td text-right font-mono tabular-nums">{row.entry.requests}</td>
                <td className="td">{trend(row)}</td>
              </>
            )}
            defaultSort="Backend day_start ascending order"
            caption="TronGrid quota history"
            loading={quota.isLoading}
            emptyState={<EmptyState kind="search" title="No quota history yet" description="History appears once payd has recorded at least one UTC day of TronGrid requests." />}
          />
        </div>
      </div>

      <p className="card text-sm text-ink-secondary">
        <Link href="/addresses?has_balance=1" className="cursor-pointer text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">
          Balance-holding addresses
        </Link>{" "}
        are the growth driver behind this count (backend OPS-007) — every address able to receive a payment is one
        more address this service polls.
      </p>
    </section>
  );
}
