"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, ExternalLink, Filter, Plus, Receipt, RefreshCw } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";

import { Amount } from "@/components/data/amount";
import { CursorPager } from "@/components/data/cursor-pager";
import { DataTable, TableFilters } from "@/components/data/data-table";
import { EmptyState } from "@/components/data/empty-state";
import { ErrorState } from "@/components/data/error-state";
import { AddressLink, EntityId, TxidLink } from "@/components/data/links";
import { RefreshButton } from "@/components/data/refresh-button";
import { StatusBadge } from "@/components/data/status-badge";
import { Timestamp } from "@/components/data/timestamp";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { Order, OrderDetailResponse, OrderEventsResponse, OrderList, Payment } from "@/lib/payd/types";
import { useTronscanBaseUrl } from "@/app/providers";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";
import { ExportDialog } from "./export-dialog";
import { OrderActions } from "./order-actions";

const LIST_INTERVAL = 30_000;
const DETAIL_INTERVAL = 5_000;
const terminal = new Set(["confirmed", "expired", "expired_funded", "cancelled", "cancelled_funded"]);

const copyByCode: Record<string, string> = {
  unauthorized: "This dashboard session or its upstream scope is not authorised.",
  rate_limited: "Refresh has slowed because payd is rate limited.",
  upstream_unreachable: "payd could not be reached; showing the last available data when present.",
  upstream_timeout: "payd did not answer in time; showing the last available data when present.",
};

type Filters = Record<"status" | "asset" | "external_ref" | "consumer" | "address" | "created_from" | "created_to", string>;

function dateValue(seconds: string | null): string {
  return seconds && /^\d+$/.test(seconds) ? new Date(Number(seconds) * 1000).toISOString().slice(0, 10) : "";
}

function toSeconds(value: string, end = false): string {
  if (!value) return "";
  return String(Math.floor(Date.parse(`${value}T${end ? "23:59:59" : "00:00:00"}Z`) / 1000));
}

function orderFilters(params: URLSearchParams): Filters {
  return {
    status: params.get("status") ?? "", asset: params.get("asset") ?? "", external_ref: params.get("external_ref") ?? "",
    consumer: params.get("consumer") ?? "", address: params.get("address") ?? "", created_from: params.get("created_from") ?? "", created_to: params.get("created_to") ?? "",
  };
}

function ErrorNotice({ error, updatedAt, interval, retry }: Readonly<{ error: unknown; updatedAt: number; interval: number; retry: () => void }>) {
  if (!error) return null;
  const paydError = isPaydError(error) ? error : null;
  return <ErrorState error={{ code: paydError?.code ?? "upstream_unreachable", details: paydError?.details }} copyByCode={copyByCode} lastUpdatedAt={updatedAt || undefined} pollingIntervalMs={interval} onRetry={retry} />;
}

function remaining(expiresAt: number, status: string) {
  if (terminal.has(status)) return null;
  const seconds = expiresAt - Math.floor(Date.now() / 1000);
  if (seconds <= 0) return <span className="text-severity-warning">expired / awaiting refresh</span>;
  const minutes = Math.ceil(seconds / 60);
  return <span className={minutes <= 15 ? "text-severity-warning" : "text-ink-secondary"}>{minutes < 60 ? `${minutes}m remaining` : `${Math.ceil(minutes / 60)}h remaining`}</span>;
}

function address(address: string) {
  return <AddressLink address={address} href={`/addresses/${encodeURIComponent(address)}`} />;
}

function usd(value: string | undefined) {
  return value ? <Amount value={value} asset="USD" variant="usd-snapshot" /> : <span className="text-ink-faint" title="The backend did not return a USD value">—</span>;
}

function OrderRow({ order }: Readonly<{ order: Order }>) {
  return <>
    <td className="td"><EntityId value={order.id} /></td>
    <td className="td font-mono text-xs">{order.external_ref || "—"}</td>
    <td className="td"><StatusBadge status={order.status} resolution={order.resolution} /></td>
    <td className="td font-mono">{order.asset}</td>
    <td className="td text-right font-mono tabular-nums"><Amount value={order.amount} asset={order.asset} /></td>
    <td className="td text-right font-mono tabular-nums"><Amount value={order.received} asset={order.asset} />{order.status === "partial" ? <span className="mt-0.5 block text-[11px] text-severity-progress">payment still due</span> : null}{order.overpaid !== "0" ? <span className="mt-0.5 block text-[11px] text-severity-warning">overpaid <Amount value={order.overpaid} asset={order.asset} variant="compact" /></span> : null}</td>
    <td className="td">{order.consumer}</td>
    <td className="td">{address(order.address)}</td>
    <td className="td text-right font-mono tabular-nums"><Timestamp seconds={order.created_at} /></td>
    <td className="td text-right font-mono tabular-nums"><Timestamp seconds={order.expires_at} /><div className="text-[11px]">{remaining(order.expires_at, order.status)}</div></td>
  </>;
}

function OrderCards({ orders }: Readonly<{ orders: Order[] }>) {
  return <div className="grid gap-3 lg:hidden">{orders.map((order) => <article key={order.id} className="card"><div className="flex items-start justify-between gap-2"><Link href={`/orders/${encodeURIComponent(order.id)}`} className="cursor-pointer text-ink hover:text-accent focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"><EntityId value={order.id} /></Link><StatusBadge status={order.status} resolution={order.resolution} /></div><p className="mt-2 font-mono text-xs text-ink-secondary">{order.external_ref || "No external reference"}</p><p className="mt-2"><Amount value={order.amount} asset={order.asset} /> expected · <Amount value={order.received} asset={order.asset} /> received</p>{order.status === "partial" ? <p className="mt-1 text-xs text-severity-progress">partial — backend reports payment still due</p> : null}{order.overpaid !== "0" ? <p className="mt-1 text-xs text-severity-warning">overpaid <Amount value={order.overpaid} asset={order.asset} variant="compact" /></p> : null}<p className="mt-2">{address(order.address)}</p><p className="mt-2 text-xs text-ink-secondary"><Timestamp seconds={order.created_at} /> · expires <Timestamp seconds={order.expires_at} /> · {remaining(order.expires_at, order.status)}</p></article>)}</div>;
}

function OrdersList() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const filters = orderFilters(searchParams);
  const cursor = searchParams.get("cursor") ?? "";
  const limit = searchParams.get("limit") === "200" ? 200 : 50;
  const query = new URLSearchParams({ limit: String(limit) });
  Object.entries(filters).forEach(([key, value]) => { if (value) query.set(key, value); });
  if (cursor) query.set("cursor", cursor);
  const orders = useQuery(paydQueryOptions({ queryKey: queryKeys.orders.list(Object.fromEntries(query)), queryFn: () => paydRequest<OrderList>(["orders"], {}, query), polling: { tier: "B" } }));
  const setParams = (next: Record<string, string>) => {
    const value = new URLSearchParams(searchParams);
    Object.entries(next).forEach(([key, item]) => item ? value.set(key, item) : value.delete(key));
    if (!("cursor" in next)) value.delete("cursor");
    router.replace(`${pathname}${value.size ? `?${value}` : ""}`);
  };
  const active = Object.values(filters).some(Boolean);
  const rows = orders.data?.orders ?? [];

  return <main className="page"><header><p className="page-kicker"><Receipt aria-hidden="true" size={14} strokeWidth={1.75} />Operations / Orders</p><div className="mt-1 flex flex-wrap items-center justify-between gap-3"><div><h1 className="page-title">Orders</h1><p className="mt-1 text-sm text-ink-secondary">Newest first, as returned by payd.</p></div><div className="flex flex-wrap gap-2"><Link href="/orders/new" className="btn btn-primary"><Plus aria-hidden="true" size={14} strokeWidth={1.75} />Create order</Link><Link href="/orders/funded-terminal" className="btn btn-secondary">Funded terminal</Link>{/* WRPT-030/WRPT-036: the CURRENT list filters, straight through, not a separate export filter state. */}<ExportDialog kind="orders" filters={filters} /><button type="button" className="btn btn-secondary" onClick={() => void orders.refetch()}><RefreshCw aria-hidden="true" size={14} strokeWidth={1.75} />Refresh</button></div></div></header>
    <section className="card"><div className="mb-3 flex items-center gap-2"><Filter aria-hidden="true" size={14} strokeWidth={1.75} className="text-ink-faint" /><h2 className="card-title">Filters</h2></div><TableFilters active={active} onClear={() => setParams({ status: "", asset: "", external_ref: "", consumer: "", address: "", created_from: "", created_to: "" })}>
      <label className="grid gap-1 text-xs text-ink-secondary">Status<input value={filters.status} onChange={(event) => setParams({ status: event.currentTarget.value })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>
      <label className="grid gap-1 text-xs text-ink-secondary">Asset<input value={filters.asset} onChange={(event) => setParams({ asset: event.currentTarget.value })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>
      <label className="grid gap-1 text-xs text-ink-secondary">Created from (UTC)<input type="date" value={dateValue(filters.created_from)} onChange={(event) => setParams({ created_from: toSeconds(event.currentTarget.value) })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>
      <label className="grid gap-1 text-xs text-ink-secondary">Created to (UTC)<input type="date" value={dateValue(filters.created_to)} onChange={(event) => setParams({ created_to: toSeconds(event.currentTarget.value, true) })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>
      <label className="grid gap-1 text-xs text-ink-secondary">External reference<input value={filters.external_ref} onChange={(event) => setParams({ external_ref: event.currentTarget.value })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>
      <label className="grid gap-1 text-xs text-ink-secondary">Consumer<input value={filters.consumer} onChange={(event) => setParams({ consumer: event.currentTarget.value })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>
      <label className="grid gap-1 text-xs text-ink-secondary">Address<input value={filters.address} onChange={(event) => setParams({ address: event.currentTarget.value })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>
    </TableFilters></section>
    <section className="card p-0"><div className="border-b border-border-subtle px-4 py-3"><h2 className="card-title">Order worklist</h2></div><div className="hidden lg:block"><DataTable columns={[{ id: "id", label: "ID" }, { id: "external_ref", label: "External ref" }, { id: "status", label: "Status" }, { id: "asset", label: "Asset" }, { id: "expected", label: "Expected", className: "text-right" }, { id: "received", label: "Received", className: "text-right" }, { id: "consumer", label: "Consumer" }, { id: "address", label: "Address" }, { id: "created", label: "Created", className: "text-right" }, { id: "expires", label: "Expires", className: "text-right" }]} rows={rows} rowKey={(order) => order.id} renderRow={(order) => <OrderRow order={order} />} onRowClick={(order) => router.push(`/orders/${encodeURIComponent(order.id)}`)} defaultSort="Backend newest-first ULID order" caption="Orders" loading={orders.isLoading} emptyState={<EmptyState kind="search" title="No orders match these filters" description="Orders appear here after a consumer or operator creates one." />} /></div></section>
    {!orders.isLoading && rows.length ? <OrderCards orders={rows} /> : null}
    <ErrorNotice error={orders.isError ? orders.error : null} updatedAt={orders.dataUpdatedAt} interval={LIST_INTERVAL} retry={() => void orders.refetch()} />
    <CursorPager nextCursor={orders.data?.next_cursor} hasResults={rows.length > 0} limit={limit} onNext={(next) => setParams({ cursor: next })} onStart={() => setParams({ cursor: "" })} onLimitChange={(next) => setParams({ limit: String(next) })} />
  </main>;
}

function Field({ label, children }: Readonly<{ label: string; children: React.ReactNode }>) { return <div><dt className="text-xs uppercase tracking-wide text-ink-faint">{label}</dt><dd className="mt-1 break-words">{children}</dd></div>; }

function StateMachine({ status }: Readonly<{ status: string }>) {
  const transitions: Record<string, string> = { pending: "payment (partial) → partial; TTL expiry → expired; cancellation → cancelled", partial: "payment (sufficient) → paid; TTL expiry → expired or expired_funded", paid: "solidification → confirmed; reorganisation → partial", confirmed: "terminal", expired: "terminal", expired_funded: "terminal; customer money remains settled by backend record", cancelled: "terminal", cancelled_funded: "terminal; customer money remains settled by backend record" };
  return <section className="card"><h2 className="card-title">Backend state machine</h2><p className="mt-3 font-mono text-xs leading-6 text-ink-secondary">pending → partial → paid → confirmed<br />pending → expired / cancelled<br />partial → expired / expired_funded<br />paid → partial (reorganisation)</p><p className="mt-3">Current backend state: <StatusBadge status={status} /></p><p className="mt-2 text-sm text-ink-secondary">Possible transitions: {transitions[status] ?? "unknown backend state; no client-side inference."}</p></section>;
}

function Payments({ payments, nextCursor, onNext }: Readonly<{ payments: Payment[]; nextCursor: string; onNext: (cursor: string) => void }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  return <section className="card space-y-3"><h2 className="card-title">Payments</h2><div className="hidden lg:block"><DataTable columns={[{ id: "txid", label: "Transaction" }, { id: "payment", label: "Payment" }, { id: "from", label: "Sender" }, { id: "to", label: "Recipient" }, { id: "amount", label: "Amount", className: "text-right" }, { id: "status", label: "Status" }, { id: "height", label: "Block height", className: "text-right" }, { id: "chain", label: "Chain time", className: "text-right" }, { id: "observed", label: "Observed", className: "text-right" }, { id: "confirmed", label: "Confirmed", className: "text-right" }, { id: "dust", label: "Dust" }]} rows={payments} rowKey={(payment) => `${payment.txid}:${payment.log_index}`} defaultSort="Backend payment cursor order" caption="Order payments" renderRow={(payment) => <><td className="td"><TxidLink txid={payment.txid} tronscanBaseUrl={tronscanBaseUrl} /></td><td className="td font-mono text-xs">{payment.id} / log {payment.log_index}<br />{payment.direction} · order {payment.order_id ?? "—"}</td><td className="td">{address(payment.from_address)}</td><td className="td">{address(payment.to_address)}</td><td className="td text-right font-mono tabular-nums"><Amount value={payment.amount} asset={payment.asset} /></td><td className="td"><StatusBadge status={payment.status} /></td><td className="td text-right font-mono tabular-nums">{payment.block_height}</td><td className="td text-right font-mono tabular-nums"><Timestamp seconds={payment.block_timestamp} /></td><td className="td text-right font-mono tabular-nums text-xs text-ink-secondary"><Timestamp seconds={payment.detected_at} /></td><td className="td text-right font-mono tabular-nums"><Timestamp seconds={payment.confirmed_at} /></td><td className="td">{payment.is_dust ? <span className="inline-flex items-center gap-1 text-severity-warning"><AlertTriangle aria-hidden="true" size={14} />Dust</span> : "—"}</td></>} emptyState={<EmptyState kind="search" title="No attributed payments" description="Payments appear once payd attributes an inbound transfer to this order." />} /></div><div className="grid gap-3 lg:hidden">{payments.map((payment) => <article key={`${payment.txid}:${payment.log_index}`} className="card"><TxidLink txid={payment.txid} tronscanBaseUrl={tronscanBaseUrl} /><p className="mt-2 text-xs text-ink-secondary">Payment {payment.id} / log {payment.log_index} · {payment.direction} · order {payment.order_id ?? "—"}</p><p className="mt-2">Sender {address(payment.from_address)}<br />Recipient {address(payment.to_address)}</p><p className="mt-2"><Amount value={payment.amount} asset={payment.asset} /> · <StatusBadge status={payment.status} /></p><p className="mt-2 text-xs text-ink-secondary">block {payment.block_height} · Chain time <Timestamp seconds={payment.block_timestamp} /><br />Observed <Timestamp seconds={payment.detected_at} /> · Confirmed <Timestamp seconds={payment.confirmed_at} /></p>{payment.is_dust ? <p className="mt-2 inline-flex items-center gap-1 text-severity-warning"><AlertTriangle aria-hidden="true" size={14} />Dust payment</p> : null}</article>)}</div><CursorPager nextCursor={nextCursor} hasResults={payments.length > 0} onNext={onNext} onStart={() => onNext("")} /></section>;
}

function Events({ orderId, live, cursor, onCursor }: Readonly<{ orderId: string; live: boolean; cursor: string; onCursor: (cursor: string) => void }>) {
  const query = new URLSearchParams({ limit: "50" });
  if (cursor) query.set("cursor", cursor);
  const events = useQuery(paydQueryOptions({ queryKey: queryKeys.orders.detail(`${orderId}:events:${cursor}`), queryFn: () => paydRequest<OrderEventsResponse>(["orders", orderId, "events"], {}, query), polling: { tier: "A", entity: "detail", isLive: () => live } }));
  const rows = events.data?.events ?? [];
  return <section className="card space-y-3"><h2 className="card-title">Webhook events</h2><p className="text-sm text-ink-secondary">Delivery history only. A dead event can be redelivered from Webhooks; it does not change this order or any payment.</p><div className="hidden lg:block"><DataTable columns={[{ id: "id", label: "ID" }, { id: "consumer", label: "Consumer" }, { id: "type", label: "Event" }, { id: "status", label: "Status" }, { id: "attempts", label: "Attempts", className: "text-right" }, { id: "response", label: "Last response", className: "text-right" }, { id: "error", label: "Last error" }, { id: "created", label: "Created", className: "text-right" }, { id: "delivered", label: "Delivered", className: "text-right" }]} rows={rows} rowKey={(event) => event.id} defaultSort="Backend event cursor order" caption="Order webhook events" renderRow={(event) => <><td className="td"><EntityId value={event.id} /></td><td className="td">{event.consumer}</td><td className="td">{event.event_type}</td><td className="td"><StatusBadge status={event.status} /></td><td className="td text-right font-mono tabular-nums">{event.attempts}</td><td className="td text-right font-mono tabular-nums">{event.last_status_code || "—"}</td><td className="td text-ink-secondary">{event.last_error || "—"}</td><td className="td text-right font-mono tabular-nums"><Timestamp seconds={event.created_at} /></td><td className="td text-right font-mono tabular-nums"><Timestamp seconds={event.delivered_at} /></td></>} emptyState={<EmptyState kind="search" title="No webhook events for this order" description="Events appear after payd queues delivery to the order's configured consumer." />} /></div><div className="grid gap-3 lg:hidden">{rows.map((event) => <article key={event.id} className="card"><div className="flex justify-between gap-2"><EntityId value={event.id} /><StatusBadge status={event.status} /></div><p className="mt-2">{event.consumer} · {event.event_type} · {event.attempts} attempts</p><p className="mt-1 text-xs text-ink-secondary">Last response {event.last_status_code || "—"} · {event.last_error || "No error recorded"}</p><p className="mt-1 text-xs text-ink-secondary">Created <Timestamp seconds={event.created_at} /> · Delivered <Timestamp seconds={event.delivered_at} /></p>{event.status === "dead" ? <Link href="/webhooks" className="mt-2 inline-flex cursor-pointer items-center gap-1 text-severity-warning hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"><ExternalLink aria-hidden="true" size={14} strokeWidth={1.75} />Open Webhooks for IPN redelivery</Link> : null}</article>)}</div>{rows.some((event) => event.status === "dead") ? <Link href="/webhooks" className="inline-flex cursor-pointer items-center gap-1 text-severity-warning hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"><ExternalLink aria-hidden="true" size={14} strokeWidth={1.75} />Dead event: open Webhooks for IPN redelivery</Link> : null}<CursorPager nextCursor={events.data?.next_cursor} hasResults={rows.length > 0} onNext={onCursor} onStart={() => onCursor("")} /><ErrorNotice error={events.isError ? events.error : null} updatedAt={events.dataUpdatedAt} interval={live ? DETAIL_INTERVAL : 0} retry={() => void events.refetch()} /></section>;
}

function OrderDetail({ id, tab = "detail" }: Readonly<{ id: string; tab?: "detail" | "events" }>) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const paymentCursor = searchParams.get("payments_cursor") ?? "";
  const eventCursor = searchParams.get("events_cursor") ?? "";
  const [live, setLive] = useState(false);
  const query = new URLSearchParams({ limit: "50" });
  if (paymentCursor) query.set("cursor", paymentCursor);
  const detail = useQuery(paydQueryOptions({ queryKey: queryKeys.orders.detail(`${id}:payments:${paymentCursor}`), queryFn: () => paydRequest<OrderDetailResponse>(["orders", id], {}, query), polling: { tier: "A", entity: "detail", isLive: () => live } }));
  const order = detail.data;
  const shouldPoll = order?.status === "pending" || order?.status === "partial";
  useEffect(() => setLive(Boolean(shouldPoll)), [shouldPoll]);
  const setCursor = (key: "payments_cursor" | "events_cursor", cursor: string) => { const value = new URLSearchParams(searchParams); cursor ? value.set(key, cursor) : value.delete(key); router.replace(`${window.location.pathname}${value.size ? `?${value}` : ""}`); };
  if (!order && detail.isLoading) return <main className="page"><div className="card animate-pulse"><div className="h-4 w-40 bg-border-subtle" /><div className="mt-3 h-3 w-4/5 bg-border-subtle" /></div></main>;
  if (!order) return <main className="page"><ErrorNotice error={detail.error} updatedAt={detail.dataUpdatedAt} interval={DETAIL_INTERVAL} retry={() => void detail.refetch()} /></main>;
  const released = order.address_released_at == null ? terminal.has(order.status) ? <span>no longer recorded — attribution is settled</span> : <span>still assigned</span> : <Timestamp seconds={order.address_released_at} />;
  return <main className="page"><header><p className="page-kicker"><Receipt aria-hidden="true" size={14} strokeWidth={1.75} />Operations / Orders / Detail</p><div className="mt-1 flex flex-wrap items-center justify-between gap-3"><div className="flex flex-wrap items-center gap-3"><h1 className="page-title">Order <EntityId value={order.id} full /></h1><StatusBadge status={order.status} resolution={order.resolution} /></div><RefreshButton /></div></header><nav className="flex gap-4 border-b border-border-subtle text-sm" aria-label="Order tabs"><Link href={`/orders/${encodeURIComponent(id)}`} className={tab === "detail" ? "cursor-pointer border-b-2 border-accent pb-2 text-ink hover:text-accent focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]" : "cursor-pointer pb-2 text-ink-secondary hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"}>Detail</Link><Link href={`/orders/${encodeURIComponent(id)}/events`} className={tab === "events" ? "cursor-pointer border-b-2 border-accent pb-2 text-ink hover:text-accent focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]" : "cursor-pointer pb-2 text-ink-secondary hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"}>Events</Link></nav>
    {tab === "events" ? <Events orderId={id} live={live} cursor={eventCursor} onCursor={(cursor) => setCursor("events_cursor", cursor)} /> : <><section className="card"><h2 className="card-title">Order record</h2><dl className="mt-3 grid gap-4 sm:grid-cols-2 xl:grid-cols-3"><Field label="Full order ID"><EntityId value={order.id} full /></Field><Field label="External reference">{order.external_ref || "—"}</Field><Field label="Status"><StatusBadge status={order.status} resolution={order.resolution} /></Field><Field label="Asset">{order.asset}</Field><Field label="Expected"><Amount value={order.amount} asset={order.asset} /></Field><Field label="Received"><Amount value={order.received} asset={order.asset} /></Field><Field label="Overpaid">{order.overpaid !== "0" ? <Amount value={order.overpaid} asset={order.asset} /> : "—"}</Field><Field label="Consumer">{order.consumer}</Field><Field label="Assigned address">{address(order.address)}</Field><Field label="Created"><Timestamp seconds={order.created_at} /></Field><Field label="Expires"><Timestamp seconds={order.expires_at} /> {remaining(order.expires_at, order.status)}</Field><Field label="Updated"><Timestamp seconds={order.updated_at} /></Field><Field label="Assignment window">From <Timestamp seconds={order.created_at} /> to {released}</Field><Field label="Price at creation">{usd(order.price_usd)}</Field><Field label="Order value USD">{usd(order.amount_usd)}</Field><Field label="Resolution">{order.resolution || "—"}</Field><Field label="Resolution note">{order.resolution_note || "—"}</Field><Field label="Resolved at"><Timestamp seconds={order.resolved_at} /></Field></dl><div className="mt-4"><p className="text-xs uppercase tracking-wide text-ink-faint">Metadata</p><pre className="mt-1 max-h-80 overflow-auto border border-border-subtle bg-inset p-3 text-xs text-ink-secondary">{JSON.stringify(order.metadata, null, 2)}</pre></div></section><OrderActions order={order} /><StateMachine status={order.status} /><Payments payments={order.payments} nextCursor={order.next_cursor} onNext={(cursor) => setCursor("payments_cursor", cursor)} /><ErrorNotice error={detail.isError ? detail.error : null} updatedAt={detail.dataUpdatedAt} interval={DETAIL_INTERVAL} retry={() => void detail.refetch()} /></>}</main>;
}

export function OrdersDashboard({ orderId, tab }: Readonly<{ orderId?: string; tab?: "detail" | "events" }>) { return orderId ? <OrderDetail id={orderId} tab={tab} /> : <OrdersList />; }
