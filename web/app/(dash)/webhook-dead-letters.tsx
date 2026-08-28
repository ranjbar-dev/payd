"use client";

import { useMutation, useQuery } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";
import Link from "next/link";
import { useState } from "react";

import { CursorPager } from "@/components/data/cursor-pager";
import { DataTable } from "@/components/data/data-table";
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
  if (order.data) return <span className="space-y-1"><span className="inline-flex items-center gap-2"><Link href={href} className="text-severity-progress underline underline-offset-2"><EntityId value={event.order_id} /></Link><StatusBadge status={order.data.status} resolution={order.data.resolution} /></span>{snapshot ? <span className="block text-xs text-ink-secondary">Snapshot status: <code className="font-mono">{snapshot}</code></span> : null}{snapshot && snapshot !== order.data.status ? <span className="mt-1 flex items-center gap-1 text-xs text-severity-warning"><AlertTriangle aria-hidden="true" size={13} />Expected state difference: this immutable snapshot is not corruption; consumers must handle it.</span> : null}</span>;
  if (order.isLoading) return <span className="text-ink-secondary">Order <EntityId value={event.order_id} /> · loading current status…</span>;
  const code = isPaydError(order.error) ? order.error.code : "upstream_unreachable";
  return <span className="text-severity-warning"><Link href={href} className="underline underline-offset-2"><EntityId value={event.order_id} /></Link> · current status unavailable (<code className="font-mono">{code}</code>)</span>;
}

export function WebhookDeadLetters({ page, consumer, onCursor, onRetrySuccess }: Readonly<{ page: DeadIpnPage | undefined; consumer: string; onCursor: (cursor: string) => void; onRetrySuccess: () => Promise<unknown> }>) {
  const [selected, setSelected] = useState<DeadIpnPage["events"][number] | null>(null);
  const retry = useMutation({ mutationFn: (id: string) => paydRequest<IpnRetryResponse>(["ipn", id, "retry"], { method: "POST" }), onSuccess: onRetrySuccess });
  const rows = page?.events ?? [];
  const mutationError = isPaydError(retry.error) ? retry.error : null;
  return <section className="space-y-3"><div><h2 className="text-lg font-semibold">Dead letters</h2><p className="mt-1 text-sm text-ink-secondary">Dead notifications are newest-first as returned by payd. A redelivery only requeues a notification; delivery is asynchronous and a successful request does not mean a consumer has received it.</p></div>
    <DataTable columns={[{ id: "id", label: "Event ID" }, { id: "consumer", label: "Consumer" }, { id: "type", label: "Event type" }, { id: "order", label: "Order / current status" }, { id: "attempts", label: "Attempts" }, { id: "response", label: "Last status" }, { id: "error", label: "Last error" }, { id: "created", label: "Created" }, { id: "payload", label: "Snapshot" }, { id: "action", label: "Redelivery" }]} rows={rows} rowKey={(event) => event.id} defaultSort="Backend dead-letter cursor order" caption="Dead webhook notifications" renderRow={(event) => <><td className="px-3 py-2"><EntityId value={event.id} /></td><td className="px-3 py-2 font-mono">{event.consumer}</td><td className="px-3 py-2 font-mono">{event.event_type}</td><td className="px-3 py-2"><OrderState event={event} /></td><td className="px-3 py-2 font-mono">{event.attempts}</td><td className="px-3 py-2 font-mono">{event.last_status_code || "—"}</td><td className="px-3 py-2 text-xs text-ink-secondary">{event.last_error || "—"}{event.last_error === "consumer removed" ? <p className="mt-1 text-severity-warning">This row was written because no state change may occur without its event; the removed consumer was deliberately not rerouted to the default consumer. Retry will not help while it remains removed.</p> : null}</td><td className="px-3 py-2"><Timestamp seconds={event.created_at} /></td><td className="px-3 py-2"><details><summary className="cursor-pointer text-severity-progress underline underline-offset-2">View payload</summary><p className="mt-2 text-xs text-ink-secondary">Immutable snapshot written at enqueue time. It describes the transition, not the entity’s current state.</p><pre className="mt-2 max-w-lg overflow-auto border border-border-subtle bg-inset p-2 text-xs">{JSON.stringify(event.payload, null, 2)}</pre></details></td><td className="px-3 py-2"><button type="button" className="border border-border-strong px-2 py-1 text-xs hover:bg-raised" onClick={() => { retry.reset(); setSelected(event); }}>Requeue notification</button></td></>} emptyState={<EmptyState kind="worklist" title="No dead notifications" description="Every configured consumer heard every queued notification." />} />
    <CursorPager nextCursor={page?.next_cursor} hasResults={rows.length > 0} onNext={onCursor} onStart={() => onCursor("")} />
    {mutationError ? <p role="alert" className="border border-severity-warning bg-[var(--severity-warning-bg)] p-3 text-sm">payd did not requeue this notification. Error code: <code className="select-all font-mono">{mutationError.code}</code>{mutationError.details && Object.keys(mutationError.details).length ? <pre className="mt-2 overflow-auto text-xs">{JSON.stringify(mutationError.details, null, 2)}</pre> : null}</p> : null}
    <ConfirmDialog open={selected != null} onClose={() => setSelected(null)} title="Requeue IPN notification" confirmLabel={selected ? `Requeue ${selected.id}` : "Requeue notification"} error={mutationError} onConfirm={async () => { if (!selected) return; try { await retry.mutateAsync(selected.id); setSelected(null); } catch { /* The operator may explicitly submit again; no mutation retries itself. */ } }} apiText={<><p><strong>IPN redelivery is safe because consumers treat <code className="font-mono">event_id</code> as an idempotency key. This is the only retry in the system.</strong></p><p className="mt-2">This redelivers the <code className="font-mono">{selected?.event_type}</code> message to <code className="font-mono">{selected?.consumer}</code>; it does nothing to an order, payment, address, or withdrawal.</p><p className="mt-2">payd will reset it to pending; the dispatcher delivers it asynchronously.</p></>} />
  </section>;
}
