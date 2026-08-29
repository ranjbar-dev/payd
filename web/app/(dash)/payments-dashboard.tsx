"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, ArrowDownToLine, Filter, Search, WalletCards } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useState, type KeyboardEvent } from "react";

import { useTronscanBaseUrl } from "@/app/providers";
import { Amount } from "@/components/data/amount";
import { CursorPager } from "@/components/data/cursor-pager";
import { DataTable, TableFilters } from "@/components/data/data-table";
import { EmptyState } from "@/components/data/empty-state";
import { ErrorState } from "@/components/data/error-state";
import { AddressLink, EntityId, TxidLink } from "@/components/data/links";
import { RefreshButton } from "@/components/data/refresh-button";
import { StatusBadge } from "@/components/data/status-badge";
import { Timestamp } from "@/components/data/timestamp";
import { PaymentDrawer } from "@/app/(dash)/payment-drawer";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { AssetsResponse, Payment, PaymentList } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

const LIST_INTERVAL = 30_000;
const tronAddress = /^T[1-9A-HJ-NP-Za-km-z]{33}$/;
const ulid = /^[0-7][0-9A-HJKMNP-TV-Z]{25}$/i;

const copyByCode: Record<string, string> = {
  unauthorized: "This dashboard session or its upstream scope is not authorised.",
  rate_limited: "Refresh has slowed because payd is rate limited.",
  upstream_unreachable: "payd could not be reached; showing the last available data when present.",
  upstream_timeout: "payd did not answer in time; showing the last available data when present.",
};

type Filters = Record<"txid" | "address" | "order_id" | "status" | "direction" | "asset" | "from" | "to", string>;

function paymentFilters(params: URLSearchParams): Filters {
  return { txid: params.get("txid") ?? "", address: params.get("address") ?? "", order_id: params.get("order_id") ?? "", status: params.get("status") ?? "", direction: params.get("direction") ?? "", asset: params.get("asset") ?? "", from: params.get("from") ?? "", to: params.get("to") ?? "" };
}

function localDateValue(seconds: string): string {
  if (!/^\d+$/.test(seconds)) return "";
  const date = new Date(Number(seconds) * 1000);
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

function localDateSeconds(value: string, end = false): string {
  if (!value) return "";
  const [year, month, day] = value.split("-").map(Number);
  return String(Math.floor(new Date(year, month - 1, day, end ? 23 : 0, end ? 59 : 0, end ? 59 : 0).getTime() / 1000));
}

function address(value: string) {
  return <AddressLink address={value} href={`/addresses/${encodeURIComponent(value)}`} />;
}

function DustFlag({ payment, minDeposit }: Readonly<{ payment: Payment; minDeposit?: string }>) {
  if (!payment.is_dust) return <>—</>;
  const title = minDeposit ? `Dust: below the configured minimum deposit of ${minDeposit} ${payment.asset}.` : "Dust: loading the configured minimum deposit.";
  return <span className="inline-flex items-center gap-1 text-severity-warning" title={title}><AlertTriangle aria-hidden="true" size={14} />Dust</span>;
}

function Direction({ payment }: Readonly<{ payment: Payment }>) {
  if (payment.direction !== "out") return <span className="font-mono text-severity-neutral">in</span>;
  return <div><span className="font-mono text-severity-progress">out</span><div className="text-[11px] text-ink-faint">{payment.withdrawal_id ? <Link href={`/withdrawals/${encodeURIComponent(payment.withdrawal_id)}`} className="cursor-pointer text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-offset-2" onClick={(event) => event.stopPropagation()}><EntityId value={payment.withdrawal_id} /></Link> : <span>not a service withdrawal</span>}</div></div>;
}

function PaymentRow({ payment, minDeposit }: Readonly<{ payment: Payment; minDeposit?: string }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  return <><td className="td"><TxidLink txid={payment.txid} tronscanBaseUrl={tronscanBaseUrl} /></td><td className="td"><Direction payment={payment} /></td><td className="td font-mono">{payment.asset}</td><td className="td text-right font-mono tabular-nums"><Amount value={payment.amount} asset={payment.asset} /></td><td className="td">{address(payment.from_address)}</td><td className="td">{address(payment.to_address)}</td><td className="td"><StatusBadge status={payment.status} /></td><td className="td">{payment.order_id ? <Link href={`/orders/${encodeURIComponent(payment.order_id)}`} className="cursor-pointer text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-offset-2" onClick={(event) => event.stopPropagation()}><EntityId value={payment.order_id} /></Link> : "—"}</td><td className="td text-right font-mono tabular-nums">{payment.block_height}</td><td className="td text-right font-mono tabular-nums"><Timestamp seconds={payment.block_timestamp} /><div className="text-[11px] text-ink-faint">Observed <Timestamp seconds={payment.detected_at} /></div></td><td className="td"><DustFlag payment={payment} minDeposit={minDeposit} /></td></>;
}

function PaymentCards({ rows, minDeposit, onSelect }: Readonly<{ rows: Payment[]; minDeposit: (payment: Payment) => string | undefined; onSelect: (payment: Payment) => void }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  const activate = (payment: Payment, event: KeyboardEvent<HTMLElement>) => {
    if (event.key === "Enter" || event.key === " ") { event.preventDefault(); onSelect(payment); }
  };
  return <div className="grid gap-2 lg:hidden">{rows.map((payment) => <article key={`${payment.txid}:${payment.log_index}`} className="card cursor-pointer transition-colors duration-150 hover:bg-raised focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]" role="link" tabIndex={0} onClick={() => onSelect(payment)} onKeyDown={(event) => activate(payment, event)}><div className="flex items-start justify-between gap-2"><span onClick={(event) => event.stopPropagation()}><TxidLink txid={payment.txid} tronscanBaseUrl={tronscanBaseUrl} /></span><StatusBadge status={payment.status} /></div><p className="mt-2"><Direction payment={payment} /></p><p className="mt-2 font-mono text-xs">{payment.asset}</p><p className="mt-1 font-mono tabular-nums"><Amount value={payment.amount} asset={payment.asset} /></p><p className="mt-2">From {address(payment.from_address)}<br />To {address(payment.to_address)}</p><p className="mt-2">{payment.order_id ? <Link href={`/orders/${encodeURIComponent(payment.order_id)}`} className="cursor-pointer text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-offset-2" onClick={(event) => event.stopPropagation()}><EntityId value={payment.order_id} /></Link> : <span className="text-ink-faint">No order</span>}</p><p className="mt-2 font-mono text-xs text-ink-secondary">Block {payment.block_height}</p><p className="mt-1 text-xs text-ink-secondary"><Timestamp seconds={payment.block_timestamp} /> · Observed <Timestamp seconds={payment.detected_at} /></p><p className="mt-2"><DustFlag payment={payment} minDeposit={minDeposit(payment)} /></p></article>)}</div>;
}

function ErrorNotice({ error, updatedAt, retry }: Readonly<{ error: unknown; updatedAt: number; retry: () => void }>) {
  if (!error) return null;
  const paydError = isPaydError(error) ? error : null;
  return <ErrorState error={{ code: paydError?.code ?? "upstream_unreachable", details: paydError?.details }} copyByCode={copyByCode} lastUpdatedAt={updatedAt || undefined} pollingIntervalMs={LIST_INTERVAL} onRetry={retry} />;
}

export function PaymentsDashboard() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const filters = paymentFilters(searchParams);
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<Payment | null>(null);
  const cursor = searchParams.get("cursor") ?? "";
  const limit = searchParams.get("limit") === "200" ? 200 : 50;
  const query = new URLSearchParams({ limit: String(limit) });
  Object.entries(filters).forEach(([key, value]) => { if (value) query.set(key, value); });
  if (cursor) query.set("cursor", cursor);
  const payments = useQuery(paydQueryOptions({ queryKey: queryKeys.payments.list(Object.fromEntries(query)), queryFn: () => paydRequest<PaymentList>(["payments"], {}, query), polling: { tier: "B" } }));
  const assets = useQuery(paydQueryOptions({ queryKey: queryKeys.assets(), queryFn: () => paydRequest<AssetsResponse>(["assets"]), polling: { tier: "D" } }));
  const setParams = (next: Record<string, string>) => {
    const value = new URLSearchParams(searchParams);
    Object.entries(next).forEach(([key, item]) => item ? value.set(key, item) : value.delete(key));
    if (!("cursor" in next)) value.delete("cursor");
    router.replace(`${pathname}${value.size ? `?${value}` : ""}`);
  };
  const applySearch = () => {
    const value = search.trim();
    if (!value) return;
    const key = tronAddress.test(value) ? "address" : ulid.test(value) ? "order_id" : "txid";
    setParams({ txid: "", address: "", order_id: "", [key]: value });
  };
  const active = Object.values(filters).some(Boolean);
  const rows = payments.data?.payments ?? [];
  const minDeposit = (payment: Payment) => assets.data?.assets.find((asset) => asset.symbol === payment.asset)?.min_deposit;
  const resolvedRange = filters.from || filters.to ? <p className="text-xs text-ink-secondary">Block range (UTC): {filters.from ? <Timestamp seconds={Number(filters.from)} variant="utc-day" /> : "unbounded"} to {filters.to ? <Timestamp seconds={Number(filters.to)} variant="utc-day" /> : "unbounded"}, inclusive.</p> : null;

  return <main className="page"><header><p className="page-kicker"><ArrowDownToLine aria-hidden="true" size={14} strokeWidth={1.75} />Operations / Payments</p><div className="mt-1 flex flex-wrap items-end justify-between gap-3"><div><h1 className="page-title">Payments</h1><p className="mt-1 text-[13px] text-ink-secondary">Search payments as returned by payd; block timestamp is the primary time.</p></div><div className="flex flex-wrap gap-2 text-[13px]"><Link href="/payments/unattributed" className="btn btn-ghost text-severity-warning hover:text-ink">Unattributed worklist</Link><Link href="/payments/orphaned" className="btn btn-ghost text-severity-warning hover:text-ink">Orphaned worklist</Link><RefreshButton /></div></div></header>
    <section className="card" aria-labelledby="payment-filters-title"><div className="flex items-center gap-2"><Filter aria-hidden="true" size={14} strokeWidth={1.75} className="text-ink-faint" /><h2 id="payment-filters-title" className="card-title">Filters</h2></div><TableFilters active={active} onClear={() => { setSearch(""); setParams({ txid: "", address: "", order_id: "", status: "", direction: "", asset: "", from: "", to: "" }); }}>
      <form className="field min-w-72" onSubmit={(event) => { event.preventDefault(); applySearch(); }}><label htmlFor="payment-search">Search txid, TRON address, or order ID</label><div className="flex gap-2"><input id="payment-search" value={search} onChange={(event) => setSearch(event.currentTarget.value)} className="input min-w-0" /><button type="submit" className="btn btn-secondary"><Search aria-hidden="true" size={14} strokeWidth={1.75} />Search</button></div></form>
      <label className="field">Transaction ID<input value={filters.txid} onChange={(event) => setParams({ txid: event.currentTarget.value })} className="input" /></label>
      <label className="field">Address<input value={filters.address} onChange={(event) => setParams({ address: event.currentTarget.value })} className="input" /></label>
      <label className="field">Order ID<input value={filters.order_id} onChange={(event) => setParams({ order_id: event.currentTarget.value })} className="input" /></label>
      <label className="field">Status<input value={filters.status} onChange={(event) => setParams({ status: event.currentTarget.value })} className="input" /></label>
      <label className="field">Direction<select value={filters.direction} onChange={(event) => setParams({ direction: event.currentTarget.value })} className="input"><option value="">Any</option><option value="in">in</option><option value="out">out</option></select></label>
      <label className="field">Asset<input value={filters.asset} onChange={(event) => setParams({ asset: event.currentTarget.value })} className="input" /></label>
      <label className="field">Block date from (local)<input type="date" value={localDateValue(filters.from)} onChange={(event) => setParams({ from: localDateSeconds(event.currentTarget.value) })} className="input" /></label>
      <label className="field">Block date to (local)<input type="date" value={localDateValue(filters.to)} onChange={(event) => setParams({ to: localDateSeconds(event.currentTarget.value, true) })} className="input" /></label>
    </TableFilters></section>
    {resolvedRange}
    <section aria-labelledby="payment-ledger-title"><div className="mb-2 flex items-center justify-between gap-3"><h2 id="payment-ledger-title" className="card-title">Payment ledger</h2><span className="font-mono text-[11px] text-ink-faint" data-count={rows.length}>{rows.length} loaded</span></div><div className="hidden lg:block"><DataTable columns={[{ id: "txid", label: "Transaction" }, { id: "direction", label: "Direction" }, { id: "asset", label: "Asset" }, { id: "amount", label: "Amount", className: "text-right" }, { id: "from", label: "From" }, { id: "to", label: "To" }, { id: "status", label: "Status" }, { id: "order", label: "Order" }, { id: "height", label: "Block height", className: "text-right" }, { id: "timestamp", label: "Block timestamp / observed", className: "text-right" }, { id: "dust", label: "Dust" }]} rows={rows} rowKey={(payment) => `${payment.txid}:${payment.log_index}`} renderRow={(payment) => <PaymentRow payment={payment} minDeposit={minDeposit(payment)} />} onRowClick={setSelected} isRowActive={(payment) => selected?.id === payment.id} defaultSort="Backend payment cursor order" caption="Payments" loading={payments.isLoading} emptyState={<EmptyState kind="search" title="No payments match these filters" description="Payments appear after payd detects transfers to or from pooled addresses." icon={<WalletCards aria-hidden="true" size={20} strokeWidth={1.75} />} />} /></div>
    {!payments.isLoading && rows.length ? <PaymentCards rows={rows} minDeposit={minDeposit} onSelect={setSelected} /> : null}
    </section>
    <ErrorNotice error={payments.isError ? payments.error : null} updatedAt={payments.dataUpdatedAt} retry={() => void payments.refetch()} />
    <CursorPager nextCursor={payments.data?.next_cursor} hasResults={rows.length > 0} limit={limit} onNext={(next) => setParams({ cursor: next })} onStart={() => setParams({ cursor: "" })} onLimitChange={(next) => setParams({ limit: String(next) })} />
    {selected ? <PaymentDrawer payment={selected} minDeposit={minDeposit(selected)} onClose={() => setSelected(null)} /> : null}
  </main>;
}
