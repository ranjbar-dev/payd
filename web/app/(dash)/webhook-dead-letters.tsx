"use client";

import { useMutation, useQuery } from "@tanstack/react-query";
import { AlertTriangle, Filter, RotateCcw } from "lucide-react";
import Link from "next/link";
import { useState } from "react";

import { CursorPager } from "@/components/data/cursor-pager";
import { DataTable, TableFilters } from "@/components/data/data-table";
import { EmptyState } from "@/components/data/empty-state";
import { EntityId } from "@/components/data/links";
import { StatusBadge } from "@/components/data/status-badge";
import { Timestamp } from "@/components/data/timestamp";
import { ConfirmDialog } from "@/components/forms/confirm-dialog";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { DeadIpnPage, IpnRetryResponse, OrderDetailResponse } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

function snapshotStatus(payload: Record<string, unknown>) { return typeof payload.status === "string" ? payload.status : null; }

function OrderState({ event }: Readonly<{ event: DeadIpnPage["events"][number] }>) {
  const order = useQuery(paydQueryOptions({ queryKey: queryKeys.orders.detail(event.order_id), queryFn: () => paydRequest<OrderDetailResponse>(["orders", event.order_id]), enabled: Boolean(event.order_id), polling: { tier: "D" } }));
  if (!event.order_id) return <span className="text-ink-secondary">Not order-scoped</span>;
  const snapshot = snapshotStatus(event.payload);
  const href = `/orders/${encodeURIComponent(event.order_id)}`;
  if (order.data) return <span className="space-y-1"><span className="inline-flex items-center gap-2"><Link href={href} className="cursor-pointer text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"><EntityId value={event.order_id} /></Link><StatusBadge status={order.data.status} resolution={order.data.resolution} /></span>{snapshot ? <span className="block text-[11px] text-ink-secondary">Snapshot status: <code className="font-mono">{snapshot}</code></span> : null}{snapshot && snapshot !== order.data.status ? <span className="mt-1 flex items-center gap-1 text-[11px] text-severity-warning"><AlertTriangle aria-hidden="true" size={13} strokeWidth={1.75} />Expected state difference; not corruption.</span> : null}</span>;
  if (order.isLoading) return <span className="text-ink-secondary">Order <EntityId value={event.order_id} /> · loading current status…</span>;
  const code = isPaydError(order.error) ? order.error.code : "upstream_unreachable";
  return <span className="text-severity-warning"><Link href={href} className="cursor-pointer underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"><EntityId value={event.order_id} /></Link> · current status unavailable (<code className="font-mono">{code}</code>)</span>;
}

export function WebhookDeadLetters({ page, loading = false, consumer, consumers, onConsumerChange, onCursor, onRetrySuccess }: Readonly<{ page: DeadIpnPage | undefined; loading?: boolean; consumer: string; consumers: readonly { name: string }[]; onConsumerChange: (consumer: string) => void; onCursor: (cursor: string) => void; onRetrySuccess: () => Promise<unknown> }>) {
  const [selected, setSelected] = useState<DeadIpnPage["events"][number] | null>(null);
  const retry = useMutation({ mutationFn: (id: string) => paydRequest<IpnRetryResponse>(["ipn", id, "retry"], { method: "POST" }), onSuccess: onRetrySuccess });
  const rows = page?.events ?? [];
  const mutationError = isPaydError(retry.error) ? retry.error : null;
  return <section className="card space-y-3" aria-labelledby="dead-letters-heading"><div><h2 id="dead-letters-heading" className="card-title">Dead letters</h2><p className="mt-1 text-[13px] text-ink-secondary">Dead notifications are newest-first as returned by payd. Redelivery only requeues a notification; delivery is asynchronous and a successful request does not mean a consumer has received it.</p></div>
    <div><div className="flex items-center gap-2"><Filter aria-hidden="true" size={14} strokeWidth={1.75} className="text-ink-faint" /><h3 className="text-[11px] font-semibold uppercase tracking-wide text-ink-faint">Filters</h3></div><TableFilters active={Boolean(consumer)} onClear={() => onConsumerChange("")}><label className="field">Dead-letter consumer<select value={consumer} onChange={(event) => onConsumerChange(event.currentTarget.value)} className="input cursor-pointer transition-colors duration-150 hover:border-ink-faint focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"><option value="">All consumers</option>{consumers.map((item) => <option key={item.name} value={item.name}>{item.name}</option>)}</select></label></TableFilters></div>
    <DataTable columns={[{ id: "id", label: "Event ID" }, { id: "consumer", label: "Consumer" }, { id: "type", label: "Event type" }, { id: "order", label: "Order / current status" }, { id: "attempts", label: "Attempts", className: "text-right" }, { id: "response", label: "Last status", className: "text-right" }, { id: "error", label: "Last error" }, { id: "created", label: "Created", className: "text-right" }, { id: "payload", label: "Snapshot" }, { id: "action", label: "Redelivery" }]} rows={rows} rowKey={(event) => event.id} defaultSort="Backend dead-letter cursor order" caption="Dead webhook notifications" loading={loading} renderRow={(event) => <><td className="td"><EntityId value={event.id} /></td><td className="td font-mono">{event.consumer}</td><td className="td font-mono">{event.event_type}</td><td className="td"><OrderState event={event} /></td><td className="td text-right font-mono tabular-nums">{event.attempts}</td><td className="td text-right font-mono tabular-nums">{event.last_status_code || "—"}</td><td className="td max-w-56 text-[11px] text-ink-secondary">{event.last_error || "—"}{event.last_error === "consumer removed" ? <p className="mt-1 text-severity-warning">Written without rerouting because no state change may occur without its event. Redelivery cannot resolve a removed consumer.</p> : null}</td><td className="td text-right font-mono tabular-nums"><Timestamp seconds={event.created_at} /></td><td className="td"><details><summary className="cursor-pointer text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">View payload</summary><p className="mt-2 text-[11px] text-ink-secondary">Immutable snapshot written at enqueue time. It describes the transition, not the entity’s current state.</p><pre className="mt-2 max-w-lg overflow-auto border border-border-subtle bg-inset p-2 text-xs">{JSON.stringify(event.payload, null, 2)}</pre></details></td><td className="td"><button type="button" className="btn btn-secondary h-7 px-2 text-xs" onClick={() => { retry.reset(); setSelected(event); }}><RotateCcw aria-hidden="true" size={14} strokeWidth={1.75} />Requeue</button></td></>} emptyState={<EmptyState kind="worklist" title="No dead notifications" description="Every configured consumer heard every queued notification." />} />
    <CursorPager nextCursor={page?.next_cursor} hasResults={rows.length > 0} onNext={onCursor} onStart={() => onCursor("")} />
    {mutationError ? <p role="alert" className="border border-severity-warning bg-[var(--severity-warning-bg)] p-3 text-sm">payd did not requeue this notification. Error code: <code className="select-all font-mono">{mutationError.code}</code>{mutationError.details && Object.keys(mutationError.details).length ? <pre className="mt-2 overflow-auto text-xs">{JSON.stringify(mutationError.details, null, 2)}</pre> : null}</p> : null}
    <ConfirmDialog open={selected != null} onClose={() => setSelected(null)} title="Requeue IPN notification" confirmLabel={selected ? `Requeue ${selected.id}` : "Requeue notification"} error={mutationError} onConfirm={async () => { if (!selected) return; try { await retry.mutateAsync(selected.id); setSelected(null); } catch { /* The operator may explicitly submit again; no mutation retries itself. */ } }} apiText={<><p><strong>IPN redelivery is safe because consumers treat <code className="font-mono">event_id</code> as an idempotency key. This is the only retry in the system.</strong></p><p className="mt-2">This redelivers the <code className="font-mono">{selected?.event_type}</code> message to <code className="font-mono">{selected?.consumer}</code>; it does nothing to an order, payment, address, or withdrawal.</p><p className="mt-2">payd will reset it to pending; the dispatcher delivers it asynchronously.</p></>} />
  </section>;
}
