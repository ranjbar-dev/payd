"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, CircleOff, Link2, ListChecks } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

import { useTronscanBaseUrl } from "@/app/providers";
import { PaymentAttribute } from "@/app/(dash)/payment-attribute";
import { Amount } from "@/components/data/amount";
import { CursorPager } from "@/components/data/cursor-pager";
import { DataTable } from "@/components/data/data-table";
import { EmptyState } from "@/components/data/empty-state";
import { ErrorState } from "@/components/data/error-state";
import { AddressLink, EntityId, TxidLink } from "@/components/data/links";
import { RefreshButton } from "@/components/data/refresh-button";
import { StatusBadge } from "@/components/data/status-badge";
import { Timestamp } from "@/components/data/timestamp";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { OrderDetailResponse, Payment, PaymentList } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

const LIST_INTERVAL = 30_000;
const readCopy = {
  unauthorized: "This dashboard session or its upstream scope is not authorised.",
  rate_limited: "Refresh has slowed because payd is rate limited.",
  upstream_unreachable: "payd could not be reached; showing the last available data when present.",
  upstream_timeout: "payd did not answer in time; showing the last available data when present.",
};
const worklistLinkClass = "cursor-pointer text-ink-secondary underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]";

type WorklistKind = "unattributed" | "orphaned";

function address(value: string) {
  return <AddressLink address={value} href={`/addresses/${encodeURIComponent(value)}`} />;
}

function ReasonBadge({ payment }: Readonly<{ payment: Payment }>) {
  const reason = payment.unattributed_reason ?? "reason not recorded";
  const warning = reason === "asset_mismatch";
  return <span className="status-badge" data-severity={warning ? "warning" : "neutral"} aria-label={`${reason}, ${warning ? "warning" : "neutral"} attribution reason`}>{warning ? <AlertTriangle aria-hidden="true" size={13} strokeWidth={2.5} /> : null}{reason}</span>;
}

function OrphanedOrder({ payment }: Readonly<{ payment: Payment }>) {
  const order = useQuery(paydQueryOptions({
    queryKey: queryKeys.orders.detail(payment.order_id ?? ""),
    queryFn: () => paydRequest<OrderDetailResponse>(["orders", payment.order_id ?? ""]),
    enabled: payment.order_id != null,
    polling: { tier: "D" },
  }));
  if (payment.order_id == null) return <span className="text-ink-secondary">Never attributed</span>;
  const href = `/orders/${encodeURIComponent(payment.order_id)}`;
  if (order.data) return <span className="inline-flex items-center gap-2"><Link href={href} className="cursor-pointer text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"><EntityId value={payment.order_id} /></Link><StatusBadge status={order.data.status} resolution={order.data.resolution} /></span>;
  if (order.isLoading) return <span className="text-ink-secondary">Order <EntityId value={payment.order_id} /> · loading current status…</span>;
  const code = isPaydError(order.error) ? order.error.code : "upstream_unreachable";
  return <span className="text-severity-warning"><Link href={href} className="cursor-pointer underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"><EntityId value={payment.order_id} /></Link> · current status unavailable (error code: <code className="select-all font-mono">{code}</code>)</span>;
}

function UnattributedCells({ payment }: Readonly<{ payment: Payment }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  return <><td className="td"><TxidLink txid={payment.txid} tronscanBaseUrl={tronscanBaseUrl} /></td><td className="td text-right font-mono tabular-nums"><Amount value={payment.amount} asset={payment.asset} /></td><td className="td">{address(payment.to_address)}</td><td className="td"><ReasonBadge payment={payment} /></td><td className="td text-right font-mono tabular-nums"><Timestamp seconds={payment.block_timestamp} /></td><td className="td text-right"><PaymentAttribute payment={payment} /></td></>;
}

function OrphanedCells({ payment }: Readonly<{ payment: Payment }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  return <><td className="td"><TxidLink txid={payment.txid} tronscanBaseUrl={tronscanBaseUrl} /></td><td className="td text-right font-mono tabular-nums"><Amount value={payment.amount} asset={payment.asset} /></td><td className="td">{address(payment.to_address)}</td><td className="td"><OrphanedOrder payment={payment} /></td><td className="td text-right font-mono tabular-nums"><Timestamp seconds={payment.block_timestamp} /></td></>;
}

function UnattributedCards({ rows }: Readonly<{ rows: Payment[] }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  return <div className="divide-y divide-border-subtle lg:hidden">{rows.map((payment) => <article key={`${payment.txid}:${payment.log_index}`} className="space-y-2 px-4 py-3 first:pt-0"><div className="flex items-start justify-between gap-3"><TxidLink txid={payment.txid} tronscanBaseUrl={tronscanBaseUrl} /><span className="font-mono tabular-nums"><Amount value={payment.amount} asset={payment.asset} /></span></div><p className="text-[12px] text-ink-secondary">Recipient {address(payment.to_address)}</p><div className="flex items-center justify-between gap-3"><ReasonBadge payment={payment} /><span className="font-mono text-[11px] tabular-nums text-ink-faint"><Timestamp seconds={payment.block_timestamp} /></span></div><div><PaymentAttribute payment={payment} /></div></article>)}</div>;
}

function OrphanedCards({ rows }: Readonly<{ rows: Payment[] }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  return <div className="divide-y divide-border-subtle lg:hidden">{rows.map((payment) => <article key={`${payment.txid}:${payment.log_index}`} className="space-y-2 px-4 py-3 first:pt-0"><div className="flex items-start justify-between gap-3"><TxidLink txid={payment.txid} tronscanBaseUrl={tronscanBaseUrl} /><span className="font-mono tabular-nums"><Amount value={payment.amount} asset={payment.asset} /></span></div><p className="text-[12px] text-ink-secondary">Recipient {address(payment.to_address)}</p><p className="text-[12px]">Contributing order: <OrphanedOrder payment={payment} /></p><p className="font-mono text-[11px] tabular-nums text-ink-faint"><Timestamp seconds={payment.block_timestamp} /></p></article>)}</div>;
}

export function PaymentWorklist({ kind }: Readonly<{ kind: WorklistKind }>) {
  const router = useRouter();
  const pathname = usePathname();
  const search = useSearchParams();
  const cursor = search.get("cursor") ?? "";
  const limit = search.get("limit") === "200" ? 200 : 50;
  const query = new URLSearchParams({ limit: String(limit) });
  if (cursor) query.set("cursor", cursor);
  const worklist = useQuery(paydQueryOptions({
    queryKey: kind === "unattributed" ? queryKeys.payments.unattributed(Object.fromEntries(query)) : queryKeys.payments.orphaned(Object.fromEntries(query)),
    queryFn: () => paydRequest<PaymentList>(["payments", kind], {}, query),
    polling: { tier: "B" },
  }));
  const rows = worklist.data?.payments ?? [];
  const failure = isPaydError(worklist.error) ? worklist.error : null;
  const setParams = (next: Record<string, string>) => { const value = new URLSearchParams(search); Object.entries(next).forEach(([key, item]) => item ? value.set(key, item) : value.delete(key)); if (!("cursor" in next)) value.delete("cursor"); router.replace(`${pathname}${value.size ? `?${value}` : ""}`); };
  const unattributed = kind === "unattributed";
  const title = unattributed ? "Unattributed payments" : "Orphaned payments";
  const empty = unattributed ? <EmptyState kind="worklist" title="No unattributed payments" description="Every payment found its order." icon={<Link2 aria-hidden="true" size={20} strokeWidth={1.75} />} /> : <EmptyState kind="worklist" title="No orphaned payments" description="Nothing credited by payd was later taken away by a chain reorganisation." icon={<CircleOff aria-hidden="true" size={20} strokeWidth={1.75} />} />;
  const columns = unattributed ? [{ id: "txid", label: "Transaction" }, { id: "amount", label: "Amount", className: "text-right" }, { id: "address", label: "Address" }, { id: "reason", label: "Failed condition" }, { id: "time", label: "Block time", className: "text-right" }, { id: "action", label: "Action", className: "text-right" }] : [{ id: "txid", label: "Transaction" }, { id: "amount", label: "Amount", className: "text-right" }, { id: "address", label: "Address" }, { id: "order", label: "Contributing order / current status" }, { id: "time", label: "Block time", className: "text-right" }];

  return <main className="page mx-auto max-w-7xl space-y-4 p-4 lg:p-6"><header className="space-y-3"><div><p className="page-kicker"><ListChecks aria-hidden="true" size={14} strokeWidth={1.75} />OPERATIONS / PAYMENTS / {unattributed ? "UNATTRIBUTED" : "ORPHANED"}</p><h1 className="page-title mt-1">{title}</h1></div><p className="max-w-3xl text-[13px] text-ink-secondary">{unattributed ? "Oldest first. These funds are real and already credited to the address’s balance; they are unattributed, not lost." : "A payment was seen in a block, that block was reorganised away, and the transaction did not reappear within the reorg depth. The money is very likely not there."}</p><nav className="flex flex-wrap gap-x-4 gap-y-2 text-[13px]" aria-label="Payment worklists"><Link href="/payments" className={worklistLinkClass}>All payments</Link><Link href={unattributed ? "/payments/orphaned" : "/payments/unattributed"} className="cursor-pointer text-severity-warning underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">{unattributed ? "Orphaned worklist" : "Unattributed worklist"}</Link></nav><div className="flex flex-wrap gap-2"><RefreshButton /></div></header>
    {!unattributed && rows.length ? <p role="alert" className="flex items-center gap-2 border border-severity-warning bg-[var(--severity-warning-bg)] p-3 text-[13px] text-severity-warning"><AlertTriangle aria-hidden="true" size={16} strokeWidth={1.75} />This worklist is unresolved: even one orphaned payment may mean a customer was credited for money that no longer exists.</p> : null}
    <section className="card p-0" aria-labelledby="payment-worklist-heading"><div className="flex items-center justify-between gap-3 border-b border-border-subtle px-4 py-3"><h2 id="payment-worklist-heading" className="card-title">{unattributed ? "Attribution review" : "Reorganisation review"}</h2><span className="font-mono text-[11px] tabular-nums text-ink-faint" data-count={rows.length}>{rows.length} shown</span></div><div className="hidden lg:block"><DataTable columns={columns} rows={rows} rowKey={(payment) => `${payment.txid}:${payment.log_index}`} defaultSort="Oldest first by payment ID" caption={title} loading={worklist.isLoading} emptyState={empty} renderRow={(payment) => unattributed ? <UnattributedCells payment={payment} /> : <OrphanedCells payment={payment} />} /></div><div className="lg:hidden">{worklist.isLoading ? <DataTable columns={[{ id: "loading", label: "Loading" }]} rows={[]} rowKey={() => "loading"} defaultSort="Oldest first" caption={title} loading emptyState={empty} renderRow={() => null} /> : rows.length ? unattributed ? <UnattributedCards rows={rows} /> : <OrphanedCards rows={rows} /> : empty}</div></section>
    {failure ? <ErrorState error={failure} copyByCode={readCopy} lastUpdatedAt={worklist.dataUpdatedAt || undefined} pollingIntervalMs={LIST_INTERVAL} onRetry={() => void worklist.refetch()} /> : null}
    <CursorPager nextCursor={worklist.data?.next_cursor} hasResults={rows.length > 0} limit={limit} onNext={(next) => setParams({ cursor: next })} onStart={() => setParams({ cursor: "" })} onLimitChange={(next) => setParams({ limit: String(next) })} />
  </main>;
}
