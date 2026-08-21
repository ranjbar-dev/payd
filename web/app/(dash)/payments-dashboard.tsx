"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";
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
  return <div><span className="font-mono text-severity-progress">out</span><div className="mt-1 text-xs">{payment.withdrawal_id ? <Link href={`/withdrawals/${encodeURIComponent(payment.withdrawal_id)}`} className="text-severity-progress underline underline-offset-2" onClick={(event) => event.stopPropagation()}><EntityId value={payment.withdrawal_id} /></Link> : <span>not a service withdrawal</span>}</div></div>;
}

function PaymentRow({ payment, minDeposit }: Readonly<{ payment: Payment; minDeposit?: string }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  return <><td className="px-3 py-2"><TxidLink txid={payment.txid} tronscanBaseUrl={tronscanBaseUrl} /></td><td className="px-3 py-2"><Direction payment={payment} /></td><td className="px-3 py-2 font-mono">{payment.asset}</td><td className="px-3 py-2"><Amount value={payment.amount} asset={payment.asset} /></td><td className="px-3 py-2">{address(payment.from_address)}</td><td className="px-3 py-2">{address(payment.to_address)}</td><td className="px-3 py-2"><StatusBadge status={payment.status} /></td><td className="px-3 py-2">{payment.order_id ? <Link href={`/orders/${encodeURIComponent(payment.order_id)}`} className="text-severity-progress underline underline-offset-2" onClick={(event) => event.stopPropagation()}><EntityId value={payment.order_id} /></Link> : "—"}</td><td className="px-3 py-2 font-mono">{payment.block_height}</td><td className="px-3 py-2"><Timestamp seconds={payment.block_timestamp} /><div className="mt-1 text-xs text-ink-secondary">Observed <Timestamp seconds={payment.detected_at} /></div></td><td className="px-3 py-2"><DustFlag payment={payment} minDeposit={minDeposit} /></td></>;
}

function PaymentCards({ rows, minDeposit, onSelect }: Readonly<{ rows: Payment[]; minDeposit: (payment: Payment) => string | undefined; onSelect: (payment: Payment) => void }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  const activate = (payment: Payment, event: KeyboardEvent<HTMLElement>) => {
    if (event.key === "Enter" || event.key === " ") { event.preventDefault(); onSelect(payment); }
  };
  return <div className="grid gap-2 lg:hidden">{rows.map((payment) => <article key={`${payment.txid}:${payment.log_index}`} className="cursor-pointer border border-border-subtle bg-panel p-3 hover:bg-raised" role="link" tabIndex={0} onClick={() => onSelect(payment)} onKeyDown={(event) => activate(payment, event)}><div className="flex items-start justify-between gap-2"><span onClick={(event) => event.stopPropagation()}><TxidLink txid={payment.txid} tronscanBaseUrl={tronscanBaseUrl} /></span><StatusBadge status={payment.status} /></div><p className="mt-2"><Direction payment={payment} /></p><p className="mt-2 font-mono text-xs">{payment.asset}</p><p className="mt-1"><Amount value={payment.amount} asset={payment.asset} /></p><p className="mt-2">From {address(payment.from_address)}<br />To {address(payment.to_address)}</p><p className="mt-2">{payment.order_id ? <Link href={`/orders/${encodeURIComponent(payment.order_id)}`} className="text-severity-progress underline underline-offset-2" onClick={(event) => event.stopPropagation()}><EntityId value={payment.order_id} /></Link> : <span className="text-ink-faint">No order</span>}</p><p className="mt-2 font-mono text-xs text-ink-secondary">Block {payment.block_height}</p><p className="mt-1 text-xs text-ink-secondary"><Timestamp seconds={payment.block_timestamp} /> · Observed <Timestamp seconds={payment.detected_at} /></p><p className="mt-2"><DustFlag payment={payment} minDeposit={minDeposit(payment)} /></p></article>)}</div>;
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

  return <main className="mx-auto max-w-7xl space-y-4 p-4 lg:p-6"><header><p className="font-mono text-xs uppercase tracking-[0.2em] text-ink-faint">Operations / Payments</p><h1 className="mt-1 text-2xl font-semibold">Payments</h1><p className="mt-1 text-sm text-ink-secondary">Search payments as returned by payd; block timestamp is the primary time.</p><p className="mt-2 flex gap-3 text-sm"><Link href="/payments/unattributed" className="text-severity-warning underline underline-offset-2">Unattributed worklist</Link><Link href="/payments/orphaned" className="text-severity-warning underline underline-offset-2">Orphaned worklist</Link></p></header>
    <TableFilters active={active} onClear={() => { setSearch(""); setParams({ txid: "", address: "", order_id: "", status: "", direction: "", asset: "", from: "", to: "" }); }}>
      <form className="grid gap-1 text-xs text-ink-secondary" onSubmit={(event) => { event.preventDefault(); applySearch(); }}><label htmlFor="payment-search">Search txid, TRON address, or order ID</label><div className="flex"><input id="payment-search" value={search} onChange={(event) => setSearch(event.currentTarget.value)} className="min-w-72 border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /><button type="submit" className="border border-l-0 border-border-strong px-3 py-1.5 text-sm hover:bg-raised">Search</button></div></form>
      <label className="grid gap-1 text-xs text-ink-secondary">Transaction ID<input value={filters.txid} onChange={(event) => setParams({ txid: event.currentTarget.value })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>
      <label className="grid gap-1 text-xs text-ink-secondary">Address<input value={filters.address} onChange={(event) => setParams({ address: event.currentTarget.value })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>
      <label className="grid gap-1 text-xs text-ink-secondary">Order ID<input value={filters.order_id} onChange={(event) => setParams({ order_id: event.currentTarget.value })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>
      <label className="grid gap-1 text-xs text-ink-secondary">Status<input value={filters.status} onChange={(event) => setParams({ status: event.currentTarget.value })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>
      <label className="grid gap-1 text-xs text-ink-secondary">Direction<select value={filters.direction} onChange={(event) => setParams({ direction: event.currentTarget.value })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink"><option value="">Any</option><option value="in">in</option><option value="out">out</option></select></label>
      <label className="grid gap-1 text-xs text-ink-secondary">Asset<input value={filters.asset} onChange={(event) => setParams({ asset: event.currentTarget.value })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>
      <label className="grid gap-1 text-xs text-ink-secondary">Block date from (local)<input type="date" value={localDateValue(filters.from)} onChange={(event) => setParams({ from: localDateSeconds(event.currentTarget.value) })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>
      <label className="grid gap-1 text-xs text-ink-secondary">Block date to (local)<input type="date" value={localDateValue(filters.to)} onChange={(event) => setParams({ to: localDateSeconds(event.currentTarget.value, true) })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>
    </TableFilters>
    {resolvedRange}
    <div className="hidden lg:block"><DataTable columns={[{ id: "txid", label: "Transaction" }, { id: "direction", label: "Direction" }, { id: "asset", label: "Asset" }, { id: "amount", label: "Amount" }, { id: "from", label: "From" }, { id: "to", label: "To" }, { id: "status", label: "Status" }, { id: "order", label: "Order" }, { id: "height", label: "Block height" }, { id: "timestamp", label: "Block timestamp / observed" }, { id: "dust", label: "Dust" }]} rows={rows} rowKey={(payment) => `${payment.txid}:${payment.log_index}`} renderRow={(payment) => <PaymentRow payment={payment} minDeposit={minDeposit(payment)} />} onRowClick={setSelected} defaultSort="Backend payment cursor order" caption="Payments" loading={payments.isLoading} emptyState={<EmptyState kind="search" title="No payments match these filters" description="Payments appear after payd detects transfers to or from pooled addresses." />} /></div>
    {!payments.isLoading && rows.length ? <PaymentCards rows={rows} minDeposit={minDeposit} onSelect={setSelected} /> : null}
    <ErrorNotice error={payments.isError ? payments.error : null} updatedAt={payments.dataUpdatedAt} retry={() => void payments.refetch()} />
    <CursorPager nextCursor={payments.data?.next_cursor} hasResults={rows.length > 0} limit={limit} onNext={(next) => setParams({ cursor: next })} onStart={() => setParams({ cursor: "" })} onLimitChange={(next) => setParams({ limit: String(next) })} />
    {selected ? <PaymentDrawer payment={selected} minDeposit={minDeposit(selected)} onClose={() => setSelected(null)} /> : null}
  </main>;
}
