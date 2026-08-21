"use client";

import { useQuery } from "@tanstack/react-query";
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
  return <p className="text-xs text-ink-secondary">Resolved range, inclusive: <Timestamp seconds={fromSeconds} variant="utc-day" /> to <Timestamp seconds={toSeconds} variant="utc-day" />.</p>;
}

function BucketRow({ bucket, groupBy }: Readonly<{ bucket: VolumeReportBucket; groupBy: string }>) {
  const assets = Object.entries(bucket.volume);
  return <>
    <td className="px-3 py-2 font-mono text-xs">{bucket.key}{groupBy === "day" ? <span className="ml-1 text-ink-faint">(UTC day)</span> : null}</td>
    <td className="px-3 py-2 font-mono tabular-nums">{bucket.order_count}</td>
    <td className="px-3 py-2 font-mono tabular-nums">{bucket.paid_count}</td>
    <td className="px-3 py-2">{assets.length ? <div className="flex flex-col gap-0.5">{assets.map(([asset, value]) => <Amount key={asset} value={value} asset={asset} variant="compact" />)}</div> : <span className="text-ink-faint">—</span>}</td>
    <td className="px-3 py-2"><Amount value={bucket.usd_total} asset="USD" variant="usd-snapshot" /></td>
    {/* WRPT-003: its own first-class column, not a tooltip or a footnote. */}
    <td className="px-3 py-2 font-mono tabular-nums">{bucket.unpriced_paid_count}</td>
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

  return <section className="space-y-3">
    {/* WRPT-001: from, to, and group_by are all required by the backend; this UI never sends the request without them. */}
    <TableFilters active={false} onClear={() => { /* Nothing to clear back to: from/to/group_by are always required (WRPT-001). */ }}>
      <label className="grid gap-1 text-xs text-ink-secondary">Group by<select value={groupBy} onChange={(event) => setParams({ group_by: event.currentTarget.value })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink"><option value="day">Day</option><option value="asset">Asset</option><option value="consumer">Consumer</option></select></label>
      <label className="grid gap-1 text-xs text-ink-secondary">From (UTC)<input type="date" value={fromDate} onChange={(event) => setParams({ from: event.currentTarget.value })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>
      <label className="grid gap-1 text-xs text-ink-secondary">To (UTC)<input type="date" value={toDate} onChange={(event) => setParams({ to: event.currentTarget.value })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>
    </TableFilters>
    <ResolvedRange fromSeconds={fromSeconds} toSeconds={toSeconds} />
    <p className="text-sm text-ink-secondary">
      Volume is actual received value on paid/confirmed orders only (WRPT-002). USD total sums each of those orders&apos;
      price snapshot taken at creation — it is not a revaluation at today&apos;s price (WRPT-004, UI-006) — and excludes
      every order counted in <strong>Unpriced paid</strong>, which the backend could not price at creation time and is
      shown here as its own column, not a footnote (WRPT-003). Figures are rendered exactly as returned; this page adds
      no totals, averages, or percentages across buckets (WRPT-008, INV-2).
    </p>
    <DataTable
      columns={[{ id: "key", label: groupByLabel[groupBy] }, { id: "orders", label: "Order count" }, { id: "paid", label: "Paid / confirmed" }, { id: "volume", label: "Volume received" }, { id: "usd", label: "USD total (price snapshot)" }, { id: "unpriced", label: "Unpriced paid" }]}
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
  return <div><dt className="text-xs uppercase tracking-wide text-ink-faint">{label}</dt><dd className="mt-1 flex flex-wrap gap-x-4 gap-y-1">{values.map((key) => <span key={key}><span className="text-xs text-ink-secondary">{key}</span> <Amount value={source[key] ?? "0"} asset="TRX" /></span>)}</dd></div>;
}

function FeeReport() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const { fromDate, toDate, setParams, fromSeconds, toSeconds } = useRangeParams(searchParams, router, pathname);
  const query = new URLSearchParams({ from: String(fromSeconds), to: String(toSeconds) });
  const report = useQuery(paydQueryOptions({ queryKey: queryKeys.reports("fees", Object.fromEntries(query)), queryFn: () => paydRequest<FeesReportResponse>(["reports", "fees"], {}, query), polling: { tier: "D" } }));

  return <section className="space-y-3">
    {/* WRPT-020: from/to over withdrawal created_at, both required. */}
    <TableFilters active={false} onClear={() => { /* Nothing to clear back to: from/to are always required (WRPT-020). */ }}>
      <label className="grid gap-1 text-xs text-ink-secondary">From (UTC)<input type="date" value={fromDate} onChange={(event) => setParams({ from: event.currentTarget.value })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>
      <label className="grid gap-1 text-xs text-ink-secondary">To (UTC)<input type="date" value={toDate} onChange={(event) => setParams({ to: event.currentTarget.value })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>
    </TableFilters>
    <ResolvedRange fromSeconds={fromSeconds} toSeconds={toSeconds} />
    <p className="text-sm text-ink-secondary">
      This report uses the same energy total calculation as the operational metrics exported to Prometheus, so a figure
      here and a figure there agree (WRPT-024).
    </p>
    {report.data ? <div className="border border-border-subtle bg-panel p-4">
      <h2 className="font-semibold">Energy fees by source</h2>
      <p className="mt-1 text-sm text-ink-secondary">The headline of this report: a rising burn cost against a falling rented cost is what a silently failing provider looks like (WRPT-022, backend OPS-004).</p>
      <dl className="mt-3 grid gap-3 sm:grid-cols-3"><SourceLine label="Energy, exact TRX" source={report.data.energy_by_source_trx} values={["rented", "burned", "self_delegated"]} /></dl>
      <h2 className="mt-4 font-semibold">Bandwidth fees by source</h2>
      <dl className="mt-3 grid gap-3 sm:grid-cols-3"><SourceLine label="Bandwidth, exact TRX" source={report.data.bandwidth_by_source_trx} values={["existing", "delegated", "topup"]} /></dl>
      <h2 className="mt-4 font-semibold">Provider rental spend</h2>
      <p className="mt-1 text-sm text-ink-secondary">Covers every rental <strong>attempt</strong> in range, including a purchase that never resulted in delegation — money spent on a failed rental is still spent (WRPT-023).</p>
      <p className="mt-2"><Amount value={report.data.rental_spend_trx} asset="TRX" /></p>
    </div> : !report.isError ? <p className="text-sm text-ink-secondary">Loading fee report…</p> : null}
    <ErrorNotice error={report.isError ? report.error : null} retry={() => void report.refetch()} />
  </section>;
}

function Exports() {
  return <section aria-labelledby="exports-heading" className="space-y-3 border border-border-subtle bg-panel p-4">
    <div><h2 id="exports-heading" className="font-semibold">CSV exports</h2><p className="mt-1 text-sm text-ink-secondary">Reachable here as well as from the orders list and the withdrawals list, where the current on-screen filters are pre-applied (WRPT-036). These exports below carry no list filters — every row up to the cap.</p></div>
    <div className="flex flex-wrap gap-3"><ExportDialog kind="orders" filters={{}} triggerLabel="Export orders CSV" /><ExportDialog kind="withdrawals" filters={{}} triggerLabel="Export withdrawals CSV" /></div>
  </section>;
}

export function ReportsDashboard({ tab }: Readonly<{ tab: "volume" | "fees" }>) {
  return <main className="mx-auto max-w-7xl space-y-4 p-4 lg:p-6">
    <header>
      <p className="font-mono text-xs uppercase tracking-[0.2em] text-ink-faint">Operations / Reports</p>
      <h1 className="mt-1 text-2xl font-semibold">Reports</h1>
      <p className="mt-1 text-sm text-ink-secondary">A report is run, not watched: figures here do not auto-refresh (WRPT-009).</p>
    </header>
    <nav className="flex gap-4 border-b border-border-subtle text-sm" aria-label="Report tabs">
      <Link href="/reports" className={tab === "volume" ? "border-b-2 border-severity-progress pb-2" : "pb-2 text-ink-secondary"}>Volume report</Link>
      <Link href="/reports/fees" className={tab === "fees" ? "border-b-2 border-severity-progress pb-2" : "pb-2 text-ink-secondary"}>Fee report</Link>
    </nav>
    {tab === "volume" ? <VolumeReport /> : <FeeReport />}
    <Exports />
  </main>;
}
