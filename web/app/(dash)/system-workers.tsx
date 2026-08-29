"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

import { CursorPager } from "@/components/data/cursor-pager";
import { DataTable } from "@/components/data/data-table";
import { EmptyState } from "@/components/data/empty-state";
import { Timestamp } from "@/components/data/timestamp";
import { paydRequest } from "@/lib/payd/browser-client";
import type { WorkersResponse } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

import { Duration, ErrorNotice } from "./system-shared";

type Worker = WorkersResponse["workers"][number];

// WSYS-002: name and role for every worker `GET /workers` can return, so "is 60
// seconds since the last tick a problem" is answerable without opening the
// backend spec (backend/docs/specs/03-architecture-and-workers.md §3.1, and the
// worker names as recorded by RecordWorkerTick in backend/internal/*). This is
// deliberately static TEXT only, never a tick-interval NUMBER — that comes from
// this same response's own `expected_interval_seconds` per worker below
// (WOVW-041a/INV-5: two cadences are configurable at runtime, so a client-held
// copy of the interval would be wrong the moment config changes, and wrong
// silently). An unrecognised worker name still renders below with no
// description, rather than being hidden (UI-020/WOVW-012b's fallback rule).
const WORKER_INFO: Record<string, string> = {
  follower: "Chain Follower — polls new blocks, decodes TRX/TRC-20 transfers, and matches payments to open orders.",
  confirm: "Confirmation Tracker — promotes payments from seen to confirmed as blocks solidify. Its failure mode is silent: payments stay seen and orders never reach confirmed.",
  ipn: "IPN Dispatcher — delivers signed webhook events to configured consumers and drives retry/backoff.",
  price: "Price Poller — fetches USD price quotes that feed order pricing and the withdrawal daily cap.",
  reconciler_safety_net: "Reconciler (safety net) — periodic full-pool balance verification.",
  reconciler_balances: "Reconciler (balances) — active-tier balance verification for recently used addresses.",
  withdraw: "Withdrawal Engine — resources, signs, broadcasts, and resolves the withdrawal queue.",
  lifecycle_10s: "Lifecycle Worker (fast tick) — order expiry and address cooldown-return transitions.",
  lifecycle_60s: "Lifecycle Worker (pool check) — address pool top-up.",
  chain_params: "Chain Parameter Poller — refreshes energy/bandwidth fee parameters from the chain.",
};

function WorkerRow({ worker }: Readonly<{ worker: Worker }>) {
  const neverTicked = worker.last_tick_at == null;
  // Matches the exact staleness formula overview-dashboard.tsx already uses for
  // the same field pair, so the two pages never disagree about what "stalled"
  // means for a given worker.
  const stale = !neverTicked && worker.seconds_since_tick != null && worker.expected_interval_seconds != null && worker.seconds_since_tick > worker.expected_interval_seconds * 3;
  // WSYS-004: the backend-designed distinction, rendered explicitly. A fresh tick
  // with a non-zero error_count is "failed at some point, ticking again now" —
  // recovered, not currently failing. A stale (or null) tick is "failing now".
  const recovered = !neverTicked && !stale && worker.error_count > 0;

  return (
    <>
      <td className="td">
        <p className="font-mono tabular-nums">{worker.worker}</p>
        <p className="mt-0.5 line-clamp-2 max-w-xs text-[11px] text-ink-faint">{WORKER_INFO[worker.worker] ?? "Not described by this dashboard."}</p>
      </td>
      <td className="td text-right font-mono tabular-nums"><Duration seconds={worker.expected_interval_seconds} /></td>
      <td className={neverTicked || stale ? "td text-right font-mono tabular-nums text-severity-warning" : "td text-right font-mono tabular-nums"}>
        {neverTicked ? (
          <span className="inline-flex items-center gap-1"><AlertTriangle aria-hidden="true" size={13} strokeWidth={1.75} />never ticked</span>
        ) : (
          <>
            <Timestamp seconds={worker.last_tick_at} /> · <Duration seconds={worker.seconds_since_tick} /> ago
            {stale ? <span className="mt-0.5 flex items-center justify-end gap-1 text-[11px]"><AlertTriangle aria-hidden="true" size={13} strokeWidth={1.75} />stalled — failing now (WSYS-004)</span> : null}
          </>
        )}
      </td>
      <td className="td text-right font-mono tabular-nums">{worker.error_count}</td>
      <td className="td text-right font-mono tabular-nums">{worker.restarts}</td>
      <td className="td text-ink-secondary">
        {worker.last_error || "—"}
        {recovered ? <span className="mt-0.5 block text-xs text-severity-progress">Fresh tick, non-zero error count: failed once, recovered (WSYS-004). Sticky — stays visible until a future failure (WSYS-003, backend OPS-008).</span> : null}
      </td>
    </>
  );
}

export function SystemWorkers() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const cursor = searchParams.get("workers_cursor") ?? "";
  const limit = searchParams.get("workers_limit") === "200" ? 200 : 50;
  const query = new URLSearchParams({ limit: String(limit) });
  if (cursor) query.set("cursor", cursor);
  // WSYS-006: tier B (30s) on this tab only — every other System tab is tier D.
  const workers = useQuery(paydQueryOptions({ queryKey: queryKeys.workers(Object.fromEntries(query)), queryFn: () => paydRequest<WorkersResponse>(["workers"], {}, query), polling: { tier: "B" } }));
  const rows = workers.data?.workers ?? [];
  const setParams = (next: Record<string, string>) => {
    const value = new URLSearchParams(searchParams);
    Object.entries(next).forEach(([key, item]) => (item ? value.set(key, item) : value.delete(key)));
    if (!("workers_cursor" in next)) value.delete("workers_cursor");
    router.replace(`${pathname}${value.size ? `?${value}` : ""}`);
  };

  return (
    <section className="card space-y-3">
      <h2 className="card-title">Worker health</h2>
      <p className="text-sm text-ink-secondary">
        <code className="font-mono text-xs">last_error</code> is sticky — it is not cleared on the next success, so
        a recovered fault stays visible until the next one (backend OPS-008, WSYS-003).
      </p>
      <DataTable
        columns={[
          { id: "worker", label: "Worker" },
          { id: "interval", label: "Expected interval" },
          { id: "tick", label: "Last tick / age" },
          { id: "errors", label: "Error count (sticky)" },
          { id: "restarts", label: "Restarts" },
          { id: "last-error", label: "Last error (may be resolved)" },
        ]}
        rows={rows}
        rowKey={(worker) => worker.worker}
        renderRow={(worker) => <WorkerRow worker={worker} />}
        defaultSort="Backend worker order"
        caption="Worker health"
        loading={workers.isLoading}
        emptyState={<EmptyState kind="search" title="No workers on this page" description="Workers are process-scoped and always present once payd has started; try Back to start." />}
      />
      <ErrorNotice error={workers.isError ? workers.error : null} updatedAt={workers.dataUpdatedAt} pollingIntervalMs={30_000} onReload={() => void workers.refetch()} />
      <CursorPager
        nextCursor={workers.data?.next_cursor}
        hasResults={rows.length > 0}
        limit={limit}
        onNext={(next) => setParams({ workers_cursor: next })}
        onStart={() => setParams({ workers_cursor: "" })}
        onLimitChange={(next) => setParams({ workers_limit: String(next) })}
      />
    </section>
  );
}
