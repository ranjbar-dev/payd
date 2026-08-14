"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { Amount } from "@/components/data/amount";
import { CursorPager } from "@/components/data/cursor-pager";
import { ErrorState } from "@/components/data/error-state";
import { EntityId } from "@/components/data/links";
import { StatusBadge } from "@/components/data/status-badge";
import { ConfirmDialog } from "@/components/forms/confirm-dialog";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { Order, OrderList, Payment, PaymentAttributeResponse } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

const terminalOrders = new Set(["confirmed", "expired", "expired_funded", "cancelled", "cancelled_funded"]);

const readCopy = {
  unauthorized: "This dashboard session or its upstream scope is not authorised.",
  rate_limited: "Refresh has slowed because payd is rate limited.",
  upstream_unreachable: "payd could not be reached.",
  upstream_timeout: "payd did not answer in time.",
};

function mutationCopy(code: string) {
  switch (code) {
    case "unauthorized": return "This dashboard session or its upstream scope is not authorised.";
    case "rate_limited": return "payd is rate limited; wait for the next refresh before deciding whether to submit again.";
    case "not_found": return "payd no longer finds this payment or order. Refresh the worklist before considering another submission.";
    case "invalid_payment": return "payd rejected the payment identifier.";
    case "invalid_request": return "payd rejected the attribution request.";
    default: return "payd could not attribute this payment.";
  }
}

export function PaymentAttribute({ payment }: Readonly<{ payment: Payment }>) {
  const client = useQueryClient();
  const [choosing, setChoosing] = useState(false);
  const [cursor, setCursor] = useState("");
  const [order, setOrder] = useState<Order | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [confirmingMismatch, setConfirmingMismatch] = useState(false);
  const query = new URLSearchParams({ address: payment.to_address, asset: payment.asset, limit: "50" });
  if (cursor) query.set("cursor", cursor);
  const candidates = useQuery(paydQueryOptions({
    queryKey: queryKeys.orders.list(Object.fromEntries(query)),
    queryFn: () => paydRequest<OrderList>(["orders"], {}, query),
    enabled: choosing,
    polling: { tier: "D" },
  }));
  const mutation = useMutation({
    mutationFn: () => paydRequest<PaymentAttributeResponse>(["payments", String(payment.id), "attribute"], { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ order_id: order?.id }) }),
    onSuccess: () => {
      if (!order) return;
      void client.invalidateQueries({ queryKey: queryKeys.orders.detail(order.id) });
      void client.invalidateQueries({ queryKey: queryKeys.orders.all });
      void client.invalidateQueries({ queryKey: queryKeys.orders.fundedTerminalAll() });
      void client.invalidateQueries({ queryKey: queryKeys.wallets.all });
      void client.invalidateQueries({ queryKey: queryKeys.stats() });
      void client.invalidateQueries({ queryKey: queryKeys.payments.unattributedAll() });
      setConfirming(false);
      setChoosing(false);
      setOrder(null);
    },
  });
  const candidateError = isPaydError(candidates.error) ? candidates.error : null;
  const mutationError = isPaydError(mutation.error) ? mutation.error : null;
  const pickOrder = (next: Order) => {
    setOrder(next);
    mutation.reset();
    if (next.asset !== payment.asset) setConfirmingMismatch(true);
    else setConfirming(true);
  };
  const close = () => {
    setConfirming(false);
    setConfirmingMismatch(false);
  };

  return <><button type="button" className="border border-severity-warning px-2 py-1 text-xs hover:bg-[var(--severity-warning-bg)]" onClick={() => { setChoosing((value) => !value); setCursor(""); }}>Attribute</button>
    {choosing ? <div className="mt-2 min-w-80 space-y-2 border border-border-subtle bg-inset p-2 text-xs"><p className="text-ink-secondary">Choose an order returned by payd for this payment’s address and asset: <code className="font-mono">{payment.to_address}</code> · <code className="font-mono">{payment.asset}</code>.</p>{candidates.data?.orders.map((candidate) => <div key={candidate.id} className="border border-border-subtle bg-panel p-2"><div className="flex items-start justify-between gap-2"><EntityId value={candidate.id} /><StatusBadge status={candidate.status} resolution={candidate.resolution} /></div><p className="mt-1"><Amount value={candidate.amount} asset={candidate.asset} /> expected · <Amount value={candidate.received} asset={candidate.asset} /> received</p><button type="button" className="mt-2 border border-border-strong px-2 py-1 hover:bg-raised" onClick={() => pickOrder(candidate)}>Attribute to this order</button></div>)}{candidates.data && !candidates.data.orders.length ? <p className="text-ink-secondary">No orders were returned for this address and asset.</p> : null}{candidateError ? <ErrorState error={candidateError} copyByCode={readCopy} onRetry={() => void candidates.refetch()} /> : null}<CursorPager nextCursor={candidates.data?.next_cursor} hasResults={(candidates.data?.orders.length ?? 0) > 0} onNext={setCursor} onStart={() => setCursor("")} /></div> : null}
    {order ? <ConfirmDialog open={confirmingMismatch} onClose={close} title="Asset mismatch warning" confirmLabel="Continue to extra confirmation" onConfirm={async () => { setConfirmingMismatch(false); setConfirming(true); }} apiText={<p className="text-severity-warning">Asset mismatch: this payment is <Amount value={payment.amount} asset={payment.asset} />, but the selected order expects <Amount value={order.amount} asset={order.asset} />. Attributing it can credit the wrong asset and lose the same value.</p>} /> : null}
    {order ? <ConfirmDialog open={confirming} onClose={close} title="Attribute payment to order" confirmLabel={`Attribute ${payment.amount} ${payment.asset}`} onConfirm={async () => { try { await mutation.mutateAsync(); } catch { /* The error is rendered; no automatic resubmission. */ } }} error={mutationError} apiText={<><p>Attribute payment <code className="select-all font-mono">{payment.id}</code>: <Amount value={payment.amount} asset={payment.asset} /> sent to <code className="select-all font-mono">{payment.to_address}</code>.</p><p className="mt-2">Target order <code className="select-all font-mono">{order.id}</code>: <Amount value={order.amount} asset={order.asset} /> expected · <Amount value={order.received} asset={order.asset} /> received.</p>{terminalOrders.has(order.status) ? <p className="mt-2 text-severity-warning">Terminal order: <code className="font-mono">{order.status}</code>. Attribution updates its received amount but does not reopen it.</p> : null}{mutationError ? <><p className="mt-3 text-severity-warning" role="alert">{mutationCopy(mutationError.code)}</p><p className="mt-1 text-xs text-ink-secondary">Error code: <code className="select-all font-mono">{mutationError.code}</code></p><pre className="mt-2 overflow-auto border-t border-border-subtle pt-2 text-xs text-ink-secondary">{JSON.stringify(mutationError.details, null, 2)}</pre></> : null}</>} /> : null}
  </>;
}
