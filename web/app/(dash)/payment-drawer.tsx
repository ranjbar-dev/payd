"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, X } from "lucide-react";
import Link from "next/link";
import { useEffect, useRef } from "react";

import { useTronscanBaseUrl } from "@/app/providers";
import { Amount } from "@/components/data/amount";
import { AddressLink, EntityId, TxidLink } from "@/components/data/links";
import { StatusBadge } from "@/components/data/status-badge";
import { Timestamp } from "@/components/data/timestamp";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { AssetsResponse, OrderDetailResponse, Payment } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

const terminalOrders = new Set(["confirmed", "expired", "expired_funded", "cancelled", "cancelled_funded"]);

function Field({ label, children }: Readonly<{ label: string; children: React.ReactNode }>) {
  return <div><dt className="text-xs uppercase tracking-wide text-ink-faint">{label}</dt><dd className="mt-1 break-words">{children}</dd></div>;
}

function address(value: string) {
  return <AddressLink address={value} href={`/addresses/${encodeURIComponent(value)}`} />;
}

function assignmentWindow(payment: Payment, order: OrderDetailResponse) {
  const lower = order.created_at;
  const upper = order.address_released_at;
  const window = <><span>From <Timestamp seconds={lower} /> to </span>{upper == null ? terminalOrders.has(order.status) ? <span>no longer recorded</span> : <span>still held (no upper boundary)</span> : <Timestamp seconds={upper} />}</>;

  if (upper == null && terminalOrders.has(order.status)) {
    return { window, result: "Cannot determine: the assignment window's upper boundary is no longer recorded." };
  }
  const inside = payment.block_timestamp >= lower && (upper == null || payment.block_timestamp <= upper);
  return { window, result: inside ? "Inside this order's assignment window." : "Outside this order's assignment window." };
}

function OrderAttribution({ payment }: Readonly<{ payment: Payment }>) {
  const order = useQuery(paydQueryOptions({
    queryKey: queryKeys.orders.detail(payment.order_id ?? ""),
    queryFn: () => paydRequest<OrderDetailResponse>(["orders", payment.order_id ?? ""]),
    enabled: payment.order_id != null,
    polling: { tier: "D" },
  }));

  if (payment.order_id == null) {
    return <Field label="Attribution"><span className="text-ink-faint">No order recorded</span></Field>;
  }
  if (order.isLoading) return <Field label="Attribution"><Link href={`/orders/${encodeURIComponent(payment.order_id)}`} className="text-severity-progress underline underline-offset-2"><EntityId value={payment.order_id} /></Link><p className="mt-1 text-sm text-ink-secondary">Loading the order assignment window…</p></Field>;
  if (!order.data) {
    const code = isPaydError(order.error) ? order.error.code : "upstream_unreachable";
    return <Field label="Attribution"><Link href={`/orders/${encodeURIComponent(payment.order_id)}`} className="text-severity-progress underline underline-offset-2"><EntityId value={payment.order_id} /></Link><p className="mt-1 text-sm text-severity-warning">Assignment window unavailable (error code: <code className="select-all">{code}</code>).</p></Field>;
  }
  const assignment = assignmentWindow(payment, order.data);
  return <><Field label="Attributed order"><Link href={`/orders/${encodeURIComponent(payment.order_id)}`} className="text-severity-progress underline underline-offset-2"><EntityId value={payment.order_id} /></Link></Field><Field label="Assignment window">{assignment.window}</Field><Field label="Block timestamp">{assignment.result}</Field></>;
}

function UnattributedReason({ payment }: Readonly<{ payment: Payment }>) {
  if (payment.status !== "unattributed") return null;
  const reason = payment.unattributed_reason ?? "reason not recorded";
  return <Field label="Attribution decision"><span className="font-mono">{reason}</span><p className="mt-1 text-sm text-ink-secondary">Recorded by payd when it matched this payment.</p></Field>;
}

export function PaymentDrawer({ payment, minDeposit, onClose }: Readonly<{ payment: Payment; minDeposit?: string; onClose: () => void }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  const dialog = useRef<HTMLDivElement>(null);

  useEffect(() => {
    dialog.current?.focus();
  }, []);

  return <div className="fixed inset-0 z-30 bg-black/60 p-4" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
    <div ref={dialog} role="dialog" aria-modal="true" aria-labelledby="payment-detail-title" tabIndex={-1} className="card ml-auto h-full w-full max-w-2xl overflow-y-auto" onKeyDown={(event) => { if (event.key === "Escape") onClose(); }}>
      <header className="flex items-start justify-between gap-4 border-b border-border-subtle pb-3"><div><p className="page-kicker">Payment detail</p><h2 id="payment-detail-title" className="mt-1 text-[18px] font-semibold">Payment {payment.id}</h2></div><button type="button" className="btn btn-ghost px-2" onClick={onClose} aria-label="Close payment detail"><X aria-hidden="true" size={14} strokeWidth={1.75} /></button></header>
      <section className="mt-4"><dl className="grid gap-4 sm:grid-cols-2"><Field label="Full transaction ID"><code className="select-all break-all font-mono text-xs">{payment.txid}</code><div className="mt-1"><TxidLink txid={payment.txid} tronscanBaseUrl={tronscanBaseUrl} /></div></Field><Field label="Payment ID / log index"><span className="font-mono">{payment.id} / {payment.log_index}</span></Field><Field label="Direction"><span className={payment.direction === "out" ? "font-mono text-severity-progress" : "font-mono text-severity-neutral"}>{payment.direction}</span></Field><Field label="Status"><StatusBadge status={payment.status} /></Field><Field label="Asset">{payment.asset}</Field><Field label="Amount"><Amount value={payment.amount} asset={payment.asset} /></Field><Field label="Raw amount (base units)"><code className="select-all font-mono tabular-nums">{payment.amount_raw}</code></Field><Field label="Dust">{payment.is_dust ? <span className="inline-flex items-center gap-1 text-severity-warning" title={minDeposit ? `Dust: below the configured minimum deposit of ${minDeposit} ${payment.asset}.` : "Dust: loading the configured minimum deposit."}><AlertTriangle aria-hidden="true" size={14} />Dust{minDeposit ? ` (minimum ${minDeposit} ${payment.asset})` : null}</span> : "—"}</Field><Field label="From address">{address(payment.from_address)}</Field><Field label="To address">{address(payment.to_address)}</Field><Field label="Block height"><span className="font-mono">{payment.block_height}</span></Field><Field label="Block ID"><code className="select-all break-all font-mono text-xs">{payment.block_id}</code></Field><Field label="Block timestamp"><Timestamp seconds={payment.block_timestamp} /></Field><Field label="Observed"><Timestamp seconds={payment.detected_at} /></Field><Field label="Confirmed"><Timestamp seconds={payment.confirmed_at} /></Field>{payment.direction === "out" ? <Field label="Withdrawal">{payment.withdrawal_id ? <Link href={`/withdrawals/${encodeURIComponent(payment.withdrawal_id)}`} className="text-severity-progress underline underline-offset-2"><EntityId value={payment.withdrawal_id} /></Link> : <span>not a service withdrawal</span>}</Field> : null}<OrderAttribution payment={payment} /><UnattributedReason payment={payment} /></dl></section>
    </div>
  </div>;
}
