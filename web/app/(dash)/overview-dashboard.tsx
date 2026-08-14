"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";

import { alarmCounts } from "./alarm-navigation";
import { AlarmCounter } from "@/components/data/alarm-counter";
import { Amount } from "@/components/data/amount";
import { DataTable } from "@/components/data/data-table";
import { ErrorState } from "@/components/data/error-state";
import { StatusBadge } from "@/components/data/status-badge";
import { Timestamp } from "@/components/data/timestamp";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type {
  ChainParameters,
  ChainQuotaResponse,
  ChainStatusResponse,
  ConfigResponse,
  OperationalStats,
  PricePage,
  Readiness,
  VolumeReportResponse,
  WorkersResponse,
} from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

const POLL_INTERVAL = 30_000;
const listQuery = new URLSearchParams({ limit: "200" });

function duration(seconds: number | null | undefined) {
  return seconds == null ? <span className="text-ink-faint">—</span> : <Timestamp seconds={seconds} variant="duration" />;
}

function record(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function count(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function volumeBucket(value: unknown) {
  const bucket = record(value);
  const volume = record(bucket?.volume);
  return {
    key: typeof bucket?.key === "string" ? bucket.key : "UTC day",
    orders: count(bucket?.order_count),
    paid: count(bucket?.paid_count),
    volume: Object.entries(volume ?? {}).filter(([, amount]) => typeof amount === "string") as [string, string][],
    usdTotal: typeof bucket?.usd_total === "string" ? bucket.usd_total : null,
    unpriced: count(bucket?.unpriced_paid_count),
  };
}

function ErrorNotice({ error, updatedAt, onRetry }: Readonly<{ error: unknown; updatedAt: number; onRetry: () => void }>) {
  if (!error) return null;
  const paydError = isPaydError(error) ? error : null;
  return (
    <ErrorState
      error={{ code: paydError?.code ?? "upstream_unreachable", details: paydError?.details }}
      copyByCode={{
        rate_limited: "Refresh has slowed because payd is rate limited.",
        unauthorized: "The configured dashboard session or upstream scope is not authorised.",
        upstream_unreachable: "payd could not be reached; showing the last available data when present.",
      }}
      lastUpdatedAt={updatedAt || undefined}
      pollingIntervalMs={POLL_INTERVAL}
      onRetry={onRetry}
    />
  );
}

function Card({ title, href, children }: Readonly<{ title: string; href?: string; children: React.ReactNode }>) {
  const heading = href ? <Link className="focus-visible:outline-offset-2 hover:underline" href={href}>{title}</Link> : title;
  return <section className="border border-border-subtle bg-panel p-4"><h2 className="mb-3 font-semibold">{heading}</h2>{children}</section>;
}

function readinessDetail(
  reason: string,
  chain: ChainStatusResponse | undefined,
  quota: ChainQuotaResponse | undefined,
  prices: PricePage | undefined,
  params: ChainParameters | undefined,
  config: ConfigResponse | undefined,
) {
  const oldestPrice = prices?.prices.length ? Math.min(...prices.prices.map((price) => price.fetched_at)) : null;
  switch (reason) {
    case "database_unavailable": return { href: "/system", text: "Database is unavailable; payment processing cannot read its operational state." };
    case "database_unwritable": return { href: "/system", text: "Database is unwritable; payment processing cannot record state." };
    case "chain_lag": return { href: "/chain", text: <>Chain follower lag: {chain?.lag_blocks ?? "—"} blocks / 20.</> };
    case "solidified_stale": return { href: "/chain", text: <>Solidified height: {chain?.solidified_height ?? "—"}; latest block <Timestamp seconds={chain?.last_block_timestamp} />.</> };
    case "price_stale": return { href: "/resources", text: <>Oldest cached price: <Timestamp seconds={oldestPrice} />.</> };
    case "trongrid_quota_projection": return { href: "/system", text: <>TronGrid quota projection: {quota?.percent_used ?? "—"}% / 90%.</> };
    case "reorg_depth_exceeded": return { href: "/chain", text: <>Chain reorganisation suspected: {chain?.reorg_suspected ? "yes" : "reported by readiness"}.</> };
    case "energy_burn_ceiling": return { href: "/resources", text: <>Energy fee: {params?.getEnergyFee ?? "—"} SUN/unit; burn ceiling: {config ? <Amount value={config.energy.max_burn_trx} asset="TRX" /> : "—"}.</> };
    case "clock_skew": return { href: "/system", text: "Clock skew can cause withdrawals to be rejected or expire immediately, indistinguishable from an RPC fault." };
    case "clock_unavailable": return { href: "/system", text: "Clock availability failure can cause withdrawals to be rejected or expire immediately, indistinguishable from an RPC fault." };
    default: return { href: "/system", text: reason };
  }
}

export function OverviewDashboard() {
  const [volumeWindow] = useState(() => {
    const now = new Date();
    const dayStart = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()));
    return { dayStart, query: new URLSearchParams({ from: String(Math.floor(dayStart.getTime() / 1000)), to: String(Math.floor(now.getTime() / 1000)), group_by: "day" }) };
  });
  const { dayStart, query: volumeQuery } = volumeWindow;
  const volumeKey = Object.fromEntries(volumeQuery);
  const stats = useQuery(paydQueryOptions({ queryKey: queryKeys.stats(), queryFn: () => paydRequest<OperationalStats>(["stats"]), polling: { tier: "B" } }));
  const chain = useQuery(paydQueryOptions({ queryKey: queryKeys.chain.status(), queryFn: () => paydRequest<ChainStatusResponse>(["chain", "status"]), polling: { tier: "B" } }));
  const quota = useQuery(paydQueryOptions({ queryKey: queryKeys.chain.quota(), queryFn: () => paydRequest<ChainQuotaResponse>(["chain", "quota"]), polling: { tier: "B" } }));
  const workers = useQuery(paydQueryOptions({ queryKey: queryKeys.workers({ limit: 200 }), queryFn: () => paydRequest<WorkersResponse>(["workers"], {}, listQuery), polling: { tier: "B" } }));
  const prices = useQuery(paydQueryOptions({ queryKey: queryKeys.prices({ limit: 200 }), queryFn: () => paydRequest<PricePage>(["prices"], {}, listQuery), polling: { tier: "B" } }));
  const readiness = useQuery(paydQueryOptions({ queryKey: queryKeys.readiness(), queryFn: () => paydRequest<Readiness>(["readyz"], {}, undefined, [503]), polling: { tier: "B" } }));
  const params = useQuery(paydQueryOptions({ queryKey: queryKeys.chain.params(), queryFn: () => paydRequest<ChainParameters>(["chain", "params"]), polling: { tier: "B" } }));
  const config = useQuery(paydQueryOptions({ queryKey: queryKeys.config(), queryFn: () => paydRequest<ConfigResponse>(["config"]), polling: { tier: "D" }, staleTime: Infinity, refetchOnWindowFocus: false, refetchOnReconnect: false }));
  const volume = useQuery(paydQueryOptions({ queryKey: queryKeys.reports("volume", volumeKey), queryFn: () => paydRequest<VolumeReportResponse>(["reports", "volume"], {}, volumeQuery), polling: { tier: "B" } }));
  const alarms = alarmCounts(stats.data);
  const workerRows = workers.data?.workers ?? [];
  const volumeBuckets = volume.data?.buckets.map(volumeBucket) ?? [];
  const quotaSeverity = quota.data && quota.data.percent_used > 90 ? "critical" : quota.data && quota.data.percent_used > 75 ? "warning" : null;

  return (
    <main className="mx-auto max-w-7xl space-y-4 p-4 lg:p-6">
      <header>
        <p className="font-mono text-xs uppercase tracking-[0.2em] text-ink-faint">Operations / Overview</p>
        <h1 className="mt-1 text-2xl font-semibold">Operational overview</h1>
      </header>

      <section className="border border-border-subtle bg-panel p-3">
        <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-ink-secondary">Alarms</h2>
        <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
          <Link className="needs-operator block rounded-sm focus-visible:outline-offset-2" href="/withdrawals/needs-operator"><AlarmCounter label="needs_operator" count={alarms.needsOperator} severity="critical" /></Link>
          <Link className="group relative block rounded-sm focus-visible:outline-offset-2" href="/payments/unattributed" aria-describedby="overview-unattributed"><AlarmCounter label="Unattributed payments" count={alarms.unattributed + alarms.orphaned} /><span id="overview-unattributed" className="sr-only absolute inset-x-1 bottom-full z-10 mb-1 border border-border-strong bg-raised px-2 py-1 text-xs text-ink-secondary group-hover:not-sr-only group-focus-visible:not-sr-only">{alarms.unattributed} unattributed; {alarms.orphaned} orphaned</span></Link>
          <Link className="block rounded-sm focus-visible:outline-offset-2" href="/orders/funded-terminal"><AlarmCounter label="Funded terminal" count={alarms.fundedTerminal} /></Link>
          <Link className="block rounded-sm focus-visible:outline-offset-2" href="/webhooks"><AlarmCounter label="Dead IPNs" count={alarms.deadIpns} /></Link>
        </div>
        <ErrorNotice error={stats.isError ? stats.error : null} updatedAt={stats.dataUpdatedAt} onRetry={() => void stats.refetch()} />
      </section>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card title="Readiness">
          {readiness.data ? <div className="space-y-3"><StatusBadge status={readiness.data.status} />{readiness.data.reasons?.length ? <ul className="space-y-2">{readiness.data.reasons.map((reason) => { const detail = readinessDetail(reason, chain.data, quota.data, prices.data, params.data, config.data); return <li key={reason} className="border-l-2 border-severity-warning pl-2 text-sm"><Link className="text-severity-warning underline underline-offset-2" href={detail.href}>{detail.text}</Link></li>; })}</ul> : <p className="text-ink-secondary">All readiness checks are passing.</p>}</div> : <p className="text-ink-faint">Readiness has not loaded.</p>}
          <ErrorNotice error={readiness.isError ? readiness.error : null} updatedAt={readiness.dataUpdatedAt} onRetry={() => void readiness.refetch()} />
        </Card>
        <Card title="Chain" href="/chain">
          {chain.data ? <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm"><dt className="text-ink-secondary">Height</dt><dd className="font-mono tabular-nums">{chain.data.last_height}</dd><dt className="text-ink-secondary">Solidified</dt><dd className="font-mono tabular-nums">{chain.data.solidified_height}</dd><dt className="text-ink-secondary">Lag</dt><dd className="font-mono tabular-nums">{chain.data.lag_blocks} / 20 blocks · {duration(chain.data.lag_seconds)}</dd><dt className="text-ink-secondary">Last block</dt><dd><Timestamp seconds={chain.data.last_block_timestamp} /></dd><dt className="text-ink-secondary">Reorg</dt><dd>{chain.data.reorg_suspected ? <Link className="text-severity-warning underline underline-offset-2" href="/payments/orphaned">⚠ Reorg suspected — orphaned payments</Link> : "No reorg suspected"}</dd></dl> : <p className="text-ink-faint">Chain state has not loaded.</p>}
          <ErrorNotice error={chain.isError ? chain.error : null} updatedAt={chain.dataUpdatedAt} onRetry={() => void chain.refetch()} />
        </Card>
        <Card title="Quota (UTC)" href="/system?tab=quota">
          {quota.data ? <div className="space-y-2"><p className="font-mono tabular-nums">{quota.data.requests_today} / {quota.data.daily_request_quota} requests</p><p className={quotaSeverity === "critical" ? "text-severity-critical" : quotaSeverity === "warning" ? "text-severity-warning" : "text-ink-secondary"}>{quotaSeverity === "critical" ? "⚠ " : quotaSeverity === "warning" ? "⚠ " : ""}{quota.data.percent_used}% used today (UTC)</p><p className="text-xs text-ink-faint">Counter resets at 00:00 UTC.</p></div> : <p className="text-ink-faint">Quota has not loaded.</p>}
          <ErrorNotice error={quota.isError ? quota.error : null} updatedAt={quota.dataUpdatedAt} onRetry={() => void quota.refetch()} />
        </Card>
        <Card title="Prices" href="/resources">
          {prices.data ? <div className="space-y-2">{prices.data.prices.map((price) => { const stale = config.data ? (Date.now() / 1000) - price.fetched_at > config.data.price.stale_after_seconds : price.stale; return <div key={price.symbol} className={stale ? "border-l-2 border-severity-warning pl-2" : ""}><Amount value={price.price_usd} asset="USD" variant="usd-live" /> <span className="text-ink-secondary">{price.symbol} · <Timestamp seconds={price.fetched_at} /></span>{stale ? <p className="mt-1 text-xs text-severity-warning">⚠ Stale price: order and withdrawal creation return 503.</p> : null}</div>; })}</div> : <p className="text-ink-faint">Prices have not loaded.</p>}
          <ErrorNotice error={prices.isError ? prices.error : null} updatedAt={prices.dataUpdatedAt} onRetry={() => void prices.refetch()} />
          <ErrorNotice error={config.isError ? config.error : null} updatedAt={config.dataUpdatedAt} onRetry={() => void config.refetch()} />
        </Card>
      </div>

      <Card title="Workers" href="/system?tab=workers">
        <div className="hidden lg:block"><DataTable columns={[{ id: "worker", label: "Worker" }, { id: "tick", label: "Last tick / age" }, { id: "errors", label: "Errors" }, { id: "restarts", label: "Restarts" }, { id: "last-error", label: "Last error (may be resolved)" }]} rows={workerRows} rowKey={(worker) => worker.worker} defaultSort="API worker order" caption="Worker health" loading={workers.isLoading} renderRow={(worker) => { const stalled = worker.seconds_since_tick != null && worker.expected_interval_seconds != null && worker.seconds_since_tick > worker.expected_interval_seconds * 3; return <><td className="px-3 py-2 font-mono tabular-nums">{worker.worker}</td><td className={stalled ? "px-3 py-2 text-severity-warning" : "px-3 py-2"}>{worker.last_tick_at == null ? <span className="text-severity-warning">⚠ never ticked</span> : <><Timestamp seconds={worker.last_tick_at} /> · {duration(worker.seconds_since_tick)}{stalled && worker.worker === "confirm" ? " · ⚠ Confirmation Tracker stalled" : stalled ? " · ⚠ stalled" : ""}</>}</td><td className="px-3 py-2 font-mono tabular-nums">{worker.error_count}</td><td className="px-3 py-2 font-mono tabular-nums">{worker.restarts}</td><td className="px-3 py-2 text-ink-secondary">{worker.last_error || "—"}</td></>; }} emptyState={<p className="text-ink-secondary">No workers were returned.</p>} /></div>
        <div className="grid gap-2 lg:hidden">{workerRows.map((worker) => { const stalled = worker.seconds_since_tick != null && worker.expected_interval_seconds != null && worker.seconds_since_tick > worker.expected_interval_seconds * 3; return <article key={worker.worker} className="border border-border-subtle p-3"><p className="font-mono font-semibold">{worker.worker}</p><p className={stalled || worker.last_tick_at == null ? "mt-1 text-severity-warning" : "mt-1 text-ink-secondary"}>{worker.last_tick_at == null ? "⚠ never ticked" : <><Timestamp seconds={worker.last_tick_at} /> · {duration(worker.seconds_since_tick)}{stalled && worker.worker === "confirm" ? " · ⚠ Confirmation Tracker stalled" : stalled ? " · ⚠ stalled" : ""}</>}</p><p className="mt-1 font-mono text-xs text-ink-secondary">errors {worker.error_count} · restarts {worker.restarts}</p><p className="mt-2 text-xs text-ink-secondary">last error (may be resolved): {worker.last_error || "—"}</p></article>; })}</div>
        <ErrorNotice error={workers.isError ? workers.error : null} updatedAt={workers.dataUpdatedAt} onRetry={() => void workers.refetch()} />
      </Card>

      <Card title="Volume today (UTC)" href="/reports?tab=volume">
        {volumeBuckets.length ? <div className="space-y-3">{volumeBuckets.map((bucket) => <div key={bucket.key} className="border-l-2 border-border-strong pl-3"><p className="font-mono text-xs text-ink-secondary">{bucket.key} UTC</p><p>{bucket.orders} orders · {bucket.paid} paid or confirmed</p><div className="mt-1 flex flex-wrap gap-x-3 gap-y-1">{bucket.volume.map(([asset, amount]) => <Amount key={asset} value={amount} asset={asset} />)}{bucket.usdTotal ? <Amount value={bucket.usdTotal} asset="USD" variant="usd-snapshot" /> : null}</div>{bucket.unpriced !== 0 ? <p className="mt-1 text-severity-warning">⚠ {bucket.unpriced} paid orders are unpriced; USD total is incomplete.</p> : null}</div>)}</div> : <p className="text-ink-faint">No UTC-day volume has been returned.</p>}
        <p className="mt-2 text-xs text-ink-faint">Current UTC day: {dayStart.toISOString().slice(0, 10)} UTC.</p>
        <ErrorNotice error={volume.isError ? volume.error : null} updatedAt={volume.dataUpdatedAt} onRetry={() => void volume.refetch()} />
      </Card>
    </main>
  );
}
