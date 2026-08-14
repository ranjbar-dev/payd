"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { Amount } from "@/components/data/amount";
import { ConfirmDialog } from "@/components/forms/confirm-dialog";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { Order } from "@/lib/payd/types";
import { queryKeys } from "@/lib/query-keys";

const terminal = new Set(["confirmed", "expired", "expired_funded", "cancelled", "cancelled_funded"]);
const cancelUnavailable = new Set(["expired", "expired_funded", "cancelled", "cancelled_funded"]);

function MutationError({ error }: Readonly<{ error: unknown }>) {
  if (!error) return null;
  const payd = isPaydError(error) ? error : null;
  return <p role="alert" className="text-sm text-severity-warning">{payd?.code === "order_terminal" ? "payd reports this order is already terminal." : "payd did not apply the action."} Error code: <code className="select-all font-mono">{payd?.code ?? "upstream_unreachable"}</code>{payd?.details && Object.keys(payd.details).length ? <pre className="mt-2 overflow-auto border border-border-subtle bg-inset p-2 text-xs text-ink-secondary">{JSON.stringify(payd.details, null, 2)}</pre> : null}</p>;
}

function useOrderCache() {
  const client = useQueryClient();
  return (order: Order) => {
    client.setQueriesData({ queryKey: queryKeys.orders.all }, (old: unknown) => {
      if (!old || typeof old !== "object") return old;
      const record = old as { id?: string; orders?: Order[] };
      if (record.id === order.id) return { ...record, ...order };
      if (Array.isArray(record.orders)) return { ...record, orders: record.orders.map((item) => item.id === order.id ? { ...item, ...order } : item) };
      return old;
    });
    void client.invalidateQueries({ queryKey: queryKeys.orders.all });
    void client.invalidateQueries({ queryKey: queryKeys.stats() });
  };
}

export function OrderActions({ order }: Readonly<{ order: Order }>) {
  const updateCache = useOrderCache();
  const [cancelOpen, setCancelOpen] = useState(false);
  const [forceOpen, setForceOpen] = useState(false);
  const [extendOpen, setExtendOpen] = useState(false);
  const [ttl, setTtl] = useState(3600);
  const cancel = useMutation({ mutationFn: (force: boolean) => paydRequest<Order>(["orders", order.id, "cancel"], { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(force ? { force: true } : {}) }), onSuccess: updateCache });
  const extend = useMutation({ mutationFn: (seconds: number) => paydRequest<Order>(["orders", order.id, "extend"], { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ ttl_seconds: seconds }) }), onSuccess: updateCache });
  const maxTtl = Math.max(0, order.created_at + 86_400 - order.expires_at);
  const requestedTtl = Math.min(Math.max(1, ttl), maxTtl);
  const expiry = order.expires_at + requestedTtl;

  return <section className="border border-border-subtle bg-panel p-4"><h2 className="font-semibold">Order actions</h2><p className="mt-1 text-sm text-ink-secondary">No TOTP code is required for these order actions.</p><div className="mt-3 flex flex-wrap gap-2">
    <button type="button" className="border border-severity-warning px-3 py-2 text-sm disabled:cursor-not-allowed disabled:opacity-50" disabled={cancelUnavailable.has(order.status)} onClick={() => { cancel.reset(); setCancelOpen(true); }}>Cancel order</button>
    <button type="button" className="border border-border-strong px-3 py-2 text-sm disabled:cursor-not-allowed disabled:opacity-50" disabled={terminal.has(order.status) || maxTtl === 0} onClick={() => { extend.reset(); setExtendOpen(true); }}>Extend expiry</button>
  </div>
  {terminal.has(order.status) ? <p className="mt-2 text-sm text-ink-secondary">Extend is unavailable because this order is terminal.</p> : null}
  <MutationError error={cancel.error ?? extend.error} />
  <ConfirmDialog open={cancelOpen} onClose={() => setCancelOpen(false)} title="Cancel order" confirmLabel={`Cancel ${order.id}`} onConfirm={async () => { try { await cancel.mutateAsync(false); setCancelOpen(false); } catch (error) { if (isPaydError(error) && error.status === 409 && error.code === "order_funded") { setCancelOpen(false); setForceOpen(true); } } }} error={isPaydError(cancel.error) ? cancel.error : null} apiText={<><p>payd order <code className="select-all font-mono">{order.id}</code></p><p>Status: <code className="font-mono">{order.status}</code></p><p>Received: <Amount value={order.received} asset={order.asset} /></p></>} />
  <ConfirmDialog open={forceOpen} onClose={() => setForceOpen(false)} title="Force-cancel funded order" confirmLabel={`Force-cancel ${order.id}`} onConfirm={async () => { try { await cancel.mutateAsync(true); setForceOpen(false); } catch { /* The error is rendered; no automatic resubmission. */ } }} error={isPaydError(cancel.error) ? cancel.error : null} apiText={<><p>payd rejected the first cancellation because this order received <Amount value={order.received} asset={order.asset} />.</p><p>Force-cancelling changes this order to <code className="font-mono">cancelled_funded</code>. The funds remain in the deposit address awaiting a resolution record.</p><p>The address returns to the pool after cooldown with the funds still in it. This order will appear in the funded-terminal worklist.</p></>} />
  <ConfirmDialog open={extendOpen} onClose={() => setExtendOpen(false)} title="Extend order expiry" confirmLabel={`Extend ${order.id}`} ready={requestedTtl > 0} onConfirm={async () => { try { await extend.mutateAsync(requestedTtl); setExtendOpen(false); } catch { /* The error is rendered; no automatic resubmission. */ } }} error={isPaydError(extend.error) ? extend.error : null} apiText={<><p>payd order <code className="select-all font-mono">{order.id}</code> is currently <code className="font-mono">{order.status}</code>.</p><label className="mt-3 grid gap-1 text-xs text-ink-secondary">Additional seconds (capped at 24 hours after creation)<input type="number" min="1" max={maxTtl} value={ttl} onChange={(event) => setTtl(event.currentTarget.valueAsNumber || 0)} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label><p>Resulting expiry Unix time: <code className="font-mono">{expiry}</code></p></>} />
  </section>;
}
