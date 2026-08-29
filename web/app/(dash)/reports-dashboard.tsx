"use client";

import { useQuery } from "@tanstack/react-query";
import { FileBarChart } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

import { Amount } from "@/components/data/amount";
import { DataTable, TableFilters } from "@/components/data/data-table";
import { EmptyState } from "@/components/data/empty-state";
import { ErrorState } from "@/components/data/error-state";
import { Timestamp } from "@/components/data/timestamp";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { FeesReportResponse, VolumeReportBucket, VolumeReportResponse } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

import { ExportDialog } from "./export-dialog";

const copyByCode: Record<string, string> = {
  unauthorized: "This dashboard session or its upstream scope is not authorised.",
  rate_limited: "Refresh has slowed because payd is rate limited.",
  upstream_unreachable: "payd could not be reached.",
  upstream_timeout: "payd did not answer in time.",
};

const groupByLabel: Record<string, string> = { day: "Day (UTC)", asset: "Asset", consumer: "Consumer" };

// EXISTING CONVENTION TO REUSE, NOT REINVENT: orders-dashboard.tsx's created_from/
// created_to filter treats an entered `<input type="date">` value as a UTC calendar
// day and labels it "(UTC)" in visible text (UI-010, WRPT-005, WRPT-006 as already
// shipped there). This report follows the exact same convention for its own from/to
// range rather than building a second, different date-input pattern. Re-implemented
// locally (not imported) because orders-dashboard.tsx does not export its helpers,
// and because this codebase's coercion detector (lib/no-coercion.test.ts, G1-2) keys
// its allowlist by exact file:line, so this file needs its own Number()-free version:
// the URL here stores the raw YYYY-MM-DD string directly rather than round-tripping
// through a seconds string, so no seconds-to-date-input conversion is ever needed.
function toUtcSeconds(value: string, end = false): number | null {
  if (!value) return null;
  return Math.floor(Date.parse(`${value}T${end ? "23:59:59" : "00:00:00"}Z`) / 1000);
}

function isoDay(date: Date): string {
  return date.toISOString().slice(0, 10);
}

// Last 30 UTC days is a starting point only; every field below still comes straight
// from the response (INV-5) and the operator can widen or narrow the range.
function defaultRange(): { from: string; to: string } {
  const now = new Date();
  return { to: isoDay(now), from: isoDay(new Date(now.getTime() - 29 * 86_400_000)) };
}

function ErrorNotice({ error, retry }: Readonly<{ error: unknown; retry: () => void }>) {
  if (!error) return null;
  const paydError = isPaydError(error) ? error : null;
  return <ErrorState error={{ code: paydError?.code ?? "upstream_unreachable", details: paydError?.details }} copyByCode={copyByCode} onRetry={retry} />;
}

function useRangeParams(searchParams: URLSearchParams, router: ReturnType<typeof useRouter>, pathname: string) {
  const defaults = defaultRange();
  const fromDate = searchParams.get("from") || defaults.from;
  const toDate = searchParams.get("to") || defaults.to;
  const setParams = (next: Record<string, string>) => {
    const value = new URLSearchParams(searchParams);
    Object.entries(next).forEach(([key, item]) => (item ? value.set(key, item) : value.delete(key)));
    router.replace(`${pathname}${value.size ? `?${value}` : ""}`);
  };
  return { fromDate, toDate, setParams, fromSeconds: toUtcSeconds(fromDate), toSeconds: toUtcSeconds(toDate, true) };
}

function ResolvedRange({ fromSeconds, toSeconds }: Readonly<{ fromSeconds: number | null; toSeconds: number | null }>) {
  return <p className="font-mono text-[11px] tabular-nums text-ink-secondary">Resolved UTC range, inclusive: <Timestamp seconds={fromSeconds} variant="utc-day" /> to <Timestamp seconds={toSeconds} variant="utc-day" />.</p>;
}

function BucketRow({ bucket, groupBy }: Readonly<{ bucket: VolumeReportBucket; groupBy: string }>) {
  const assets = Object.entries(bucket.volume);
  return <>
    <td className="td font-mono text-xs">{bucket.key}{groupBy === "day" ? <span className="ml-1 text-ink-faint">(UTC day)</span> : null}</td>
    <td className="td text-right font-mono tabular-nums">{bucket.order_count}</td>
    <td className="td text-right font-mono tabular-nums">{bucket.paid_count}</td>
    <td className="td text-right">{assets.length ? <div className="flex flex-col items-end gap-0.5">{assets.map(([asset, value]) => <Amount key={asset} value={value} asset={asset} variant="compact" />)}</div> : <span className="text-ink-faint">—</span>}</td>
    <td className="td text-right"><Amount value={bucket.usd_total} asset="USD" variant="usd-snapshot" /></td>
    {/* WRPT-003: its own first-class column, not a tooltip or a footnote. */}
    <td className="td text-right font-mono tabular-nums">{bucket.unpriced_paid_count}</td>
  </>;
}

function VolumeReport() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const { fromDate, toDate, setParams, fromSeconds, toSeconds } = useRangeParams(searchParams, router, pathname);
  const groupByParam = searchParams.get("group_by");
  const groupBy = groupByParam === "asset" || groupByParam === "consumer" ? groupByParam : "day";
  const query = new URLSearchParams({ from: String(fromSeconds), to: String(toSeconds), group_by: groupBy });
  const report = useQuery(paydQueryOptions({ queryKey: queryKeys.reports("volume", Object.fromEntries(query)), queryFn: () => paydRequest<VolumeReportResponse>(["reports", "volume"], {}, query), polling: { tier: "D" } }));
  const buckets = report.data?.buckets ?? [];

  return <section className="space-y-4" aria-labelledby="volume-report-heading">
    {/* WRPT-001: from, to, and group_by are all required by the backend; this UI never sends the request without them. */}
    <div className="card">
      <h2 id="volume-report-heading" className="card-title">Volume report</h2>
      <TableFilters active={false} onClear={() => { /* Nothing to clear back to: from/to/group_by are always required (WRPT-001). */ }}>
        <label className="field">Group by<select value={groupBy} onChange={(event) => setParams({ group_by: event.currentTarget.value })} className="input cursor-pointer"><option value="day">Day</option><option value="asset">Asset</option><option value="consumer">Consumer</option></select></label>
        <label className="field">From (UTC)<input type="date" value={fromDate} onChange={(event) => setParams({ from: event.currentTarget.value })} className="input" /></label>
        <label className="field">To (UTC)<input type="date" value={toDate} onChange={(event) => setParams({ to: event.currentTarget.value })} className="input" /></label>
      </TableFilters>
      <ResolvedRange fromSeconds={fromSeconds} toSeconds={toSeconds} />
      <p className="mt-3 max-w-5xl text-[13px] text-ink-secondary">
        Volume is actual received value on paid/confirmed orders only. USD total sums each order&apos;s price snapshot at creation, not a revaluation at today&apos;s price, and excludes every order counted in <strong className="text-ink">Unpriced paid</strong>. Figures are rendered exactly as returned; this page adds no totals, averages, or percentages across buckets.
      </p>
    </div>
    <DataTable
      columns={[{ id: "key", label: groupByLabel[groupBy] }, { id: "orders", label: "Order count", className: "text-right" }, { id: "paid", label: "Paid / confirmed", className: "text-right" }, { id: "volume", label: "Volume received", className: "text-right" }, { id: "usd", label: "USD total (price snapshot)", className: "text-right" }, { id: "unpriced", label: "Unpriced paid", className: "text-right" }]}
      rows={buckets}
      rowKey={(bucket) => bucket.key}
      renderRow={(bucket) => <BucketRow bucket={bucket} groupBy={groupBy} />}
      defaultSort="Backend bucket key order"
      caption="Volume report"
      loading={report.isLoading}
      emptyState={<EmptyState kind="search" title="No volume in this range" description="Buckets appear once an order in this UTC range reaches paid or confirmed status." />}
    />
    <ErrorNotice error={report.isError ? report.error : null} retry={() => void report.refetch()} />
  </section>;
}

function SourceLine({ label, source, values }: Readonly<{ label: string; source: Record<string, string>; values: readonly string[] }>) {
  // energy_by_source_trx / bandwidth_by_source_trx carry fixed keys the backend always
  // sets, including a real "0" — a present zero is a source with no spend in range,
  // not a missing figure, so every key renders unconditionally rather than being
  // filtered out when zero.
  return <div className="space-y-2"><p className="text-[11px] font-semibold uppercase tracking-wide text-ink-secondary">{label}</p><dl className="grid gap-x-6 gap-y-2 sm:grid-cols-3">{values.map((key) => <div key={key} className="flex items-baseline justify-between gap-3 sm:block"><dt className="text-[11px] uppercase text-ink-faint">{key}</dt><dd className="font-mono tabular-nums text-right sm:mt-1"><Amount value={source[key] ?? "0"} asset="TRX" /></dd></div>)}</dl></div>;
}

function FeeReport() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const { fromDate, toDate, setParams, fromSeconds, toSeconds } = useRangeParams(searchParams, router, pathname);
  const query = new URLSearchParams({ from: String(fromSeconds), to: String(toSeconds) });
  const report = useQuery(paydQueryOptions({ queryKey: queryKeys.reports("fees", Object.fromEntries(query)), queryFn: () => paydRequest<FeesReportResponse>(["reports", "fees"], {}, query), polling: { tier: "D" } }));

  return <section className="space-y-4" aria-labelledby="fee-report-heading">
    {/* WRPT-020: from/to over withdrawal created_at, both required. */}
    <div className="card">
      <h2 id="fee-report-heading" className="card-title">Fee report</h2>
      <TableFilters active={false} onClear={() => { /* Nothing to clear back to: from/to are always required (WRPT-020). */ }}>
        <label className="field">From (UTC)<input type="date" value={fromDate} onChange={(event) => setParams({ from: event.currentTarget.value })} className="input" /></label>
        <label className="field">To (UTC)<input type="date" value={toDate} onChange={(event) => setParams({ to: event.currentTarget.value })} className="input" /></label>
      </TableFilters>
      <ResolvedRange fromSeconds={fromSeconds} toSeconds={toSeconds} />
      <p className="mt-3 text-[13px] text-ink-secondary">This report uses the same energy total calculation as the operational metrics exported to Prometheus, so a figure here and a figure there agree.</p>
    </div>
    {report.data ? <div className="card">
      <h2 className="card-title">Energy fees by source</h2>
      <p className="mt-1 text-[13px] text-ink-secondary">The report headline: a rising burn cost against a falling rented cost is the signal of a silently failing provider.</p>
      <div className="mt-4"><SourceLine label="Energy, exact TRX" source={report.data.energy_by_source_trx} values={["rented", "burned", "self_delegated"]} /></div>
      <div className="mt-5 border-t border-border-subtle pt-4"><h3 className="card-title">Bandwidth fees by source</h3><div className="mt-3"><SourceLine label="Bandwidth, exact TRX" source={report.data.bandwidth_by_source_trx} values={["existing", "delegated", "topup"]} /></div></div>
      <div className="mt-5 border-t border-border-subtle pt-4"><h3 className="card-title">Provider rental spend</h3><p className="mt-1 text-[13px] text-ink-secondary">Covers every rental <strong className="text-ink">attempt</strong> in range, including purchases that never resulted in delegation — money spent on a failed rental is still spent.</p><dl className="mt-3"><dt className="text-[11px] uppercase text-ink-faint">Exact TRX</dt><dd className="mt-1 text-right font-mono tabular-nums"><Amount value={report.data.rental_spend_trx} asset="TRX" /></dd></dl></div>
    </div> : !report.isError ? <div className="card space-y-3" aria-label="Loading fee report"><div className="h-3 w-40 animate-pulse bg-raised" /><div className="h-3 w-full animate-pulse bg-raised" /><div className="grid gap-3 sm:grid-cols-3">{[0, 1, 2].map((item) => <div key={item} className="h-12 animate-pulse bg-raised" />)}</div></div> : null}
    <ErrorNotice error={report.isError ? report.error : null} retry={() => void report.refetch()} />
  </section>;
}

function Exports() {
  return <section aria-labelledby="exports-heading" className="card space-y-3">
    <div><h2 id="exports-heading" className="card-title">CSV exports</h2><p className="mt-1 text-[13px] text-ink-secondary">Reachable here as well as from the orders and withdrawals lists, where current filters are pre-applied. These exports carry no list filters — every row up to the cap.</p></div>
    <div className="flex flex-wrap gap-2"><ExportDialog kind="orders" filters={{}} triggerLabel="Export orders CSV" /><ExportDialog kind="withdrawals" filters={{}} triggerLabel="Export withdrawals CSV" /></div>
  </section>;
}

export function ReportsDashboard({ tab }: Readonly<{ tab: "volume" | "fees" }>) {
  return <main className="page">
    <header>
      <p className="page-kicker"><FileBarChart aria-hidden="true" size={14} strokeWidth={1.75} />Operations / Reports</p>
      <h1 className="page-title mt-1">Reports</h1>
      <p className="mt-1 text-[13px] text-ink-secondary">A report is run, not watched: figures here do not auto-refresh.</p>
    </header>
    <nav className="flex gap-4 border-b border-border-subtle text-sm" aria-label="Report tabs">
      <Link href="/reports" className={tab === "volume" ? "cursor-pointer border-b-2 border-accent pb-2 text-ink transition-colors duration-150 hover:text-accent focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]" : "cursor-pointer pb-2 text-ink-secondary transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"}>Volume report</Link>
      <Link href="/reports/fees" className={tab === "fees" ? "cursor-pointer border-b-2 border-accent pb-2 text-ink transition-colors duration-150 hover:text-accent focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]" : "cursor-pointer pb-2 text-ink-secondary transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"}>Fee report</Link>
    </nav>
    {tab === "volume" ? <VolumeReport /> : <FeeReport />}
    <Exports />
  </main>;
}
