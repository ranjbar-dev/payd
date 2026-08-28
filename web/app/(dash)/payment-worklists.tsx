"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";
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
  if (order.data) return <span className="inline-flex items-center gap-2"><Link href={href} className="text-severity-progress underline underline-offset-2"><EntityId value={payment.order_id} /></Link><StatusBadge status={order.data.status} resolution={order.data.resolution} /></span>;
  if (order.isLoading) return <span className="text-ink-secondary">Order <EntityId value={payment.order_id} /> · loading current status…</span>;
  const code = isPaydError(order.error) ? order.error.code : "upstream_unreachable";
  return <span className="text-severity-warning"><Link href={href} className="underline underline-offset-2"><EntityId value={payment.order_id} /></Link> · current status unavailable (error code: <code className="select-all font-mono">{code}</code>)</span>;
}

function UnattributedCells({ payment }: Readonly<{ payment: Payment }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  return <><td className="px-3 py-2"><TxidLink txid={payment.txid} tronscanBaseUrl={tronscanBaseUrl} /></td><td className="px-3 py-2"><Amount value={payment.amount} asset={payment.asset} /></td><td className="px-3 py-2">{address(payment.to_address)}</td><td className="px-3 py-2"><ReasonBadge payment={payment} /></td><td className="px-3 py-2"><Timestamp seconds={payment.block_timestamp} /></td><td className="px-3 py-2"><PaymentAttribute payment={payment} /></td></>;
}

function OrphanedCells({ payment }: Readonly<{ payment: Payment }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  return <><td className="px-3 py-2"><TxidLink txid={payment.txid} tronscanBaseUrl={tronscanBaseUrl} /></td><td className="px-3 py-2"><Amount value={payment.amount} asset={payment.asset} /></td><td className="px-3 py-2">{address(payment.to_address)}</td><td className="px-3 py-2"><OrphanedOrder payment={payment} /></td><td className="px-3 py-2"><Timestamp seconds={payment.block_timestamp} /></td></>;
}

function UnattributedCards({ rows }: Readonly<{ rows: Payment[] }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  return <div className="grid gap-2 lg:hidden">{rows.map((payment) => <article key={`${payment.txid}:${payment.log_index}`} className="border border-severity-warning bg-panel p-3"><TxidLink txid={payment.txid} tronscanBaseUrl={tronscanBaseUrl} /><p className="mt-2"><Amount value={payment.amount} asset={payment.asset} /></p><p className="mt-2">Recipient {address(payment.to_address)}</p><p className="mt-2"><ReasonBadge payment={payment} /></p><p className="mt-2 text-xs text-ink-secondary">Block time <Timestamp seconds={payment.block_timestamp} /></p><div className="mt-3"><PaymentAttribute payment={payment} /></div></article>)}</div>;
}

function OrphanedCards({ rows }: Readonly<{ rows: Payment[] }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  return <div className="grid gap-2 lg:hidden">{rows.map((payment) => <article key={`${payment.txid}:${payment.log_index}`} className="border border-severity-warning bg-panel p-3"><TxidLink txid={payment.txid} tronscanBaseUrl={tronscanBaseUrl} /><p className="mt-2"><Amount value={payment.amount} asset={payment.asset} /></p><p className="mt-2">Recipient {address(payment.to_address)}</p><p className="mt-2">Contributing order: <OrphanedOrder payment={payment} /></p><p className="mt-2 text-xs text-ink-secondary">Block time <Timestamp seconds={payment.block_timestamp} /></p></article>)}</div>;
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
  const empty = unattributed ? <EmptyState kind="worklist" title="No unattributed payments" description="Every payment found its order." /> : <EmptyState kind="worklist" title="No orphaned payments" description="Nothing credited by payd was later taken away by a chain reorganisation." />;

  return <main className="mx-auto max-w-7xl space-y-4 p-4 lg:p-6"><header><p className="font-mono text-xs uppercase tracking-[0.2em] text-ink-faint">Operations / Payments / {unattributed ? "Unattributed" : "Orphaned"}</p><h1 className="mt-1 text-2xl font-semibold">{title}</h1>{unattributed ? <p className="mt-1 text-sm text-ink-secondary">Oldest first. These funds are real and already credited to the address’s balance; they are unattributed, not lost.</p> : <p className="mt-1 text-sm text-ink-secondary">A payment was seen in a block, that block was reorganised away, and the transaction did not reappear within the reorg depth. The money is very likely not there.</p>}<p className="mt-2 flex gap-3 text-sm"><Link href="/payments" className="underline underline-offset-2">All payments</Link><Link href={unattributed ? "/payments/orphaned" : "/payments/unattributed"} className="text-severity-warning underline underline-offset-2">{unattributed ? "Orphaned worklist" : "Unattributed worklist"}</Link></p></header>{!unattributed && rows.length ? <p role="alert" className="flex items-center gap-2 border border-severity-warning bg-[var(--severity-warning-bg)] p-3 text-sm text-severity-warning"><AlertTriangle aria-hidden="true" size={16} />This worklist is unresolved: even one orphaned payment may mean a customer was credited for money that no longer exists.</p> : null}<div className="hidden lg:block"><DataTable columns={unattributed ? [{ id: "txid", label: "Transaction" }, { id: "amount", label: "Amount" }, { id: "address", label: "Address" }, { id: "reason", label: "Failed condition" }, { id: "time", label: "Block time" }, { id: "action", label: "Action" }] : [{ id: "txid", label: "Transaction" }, { id: "amount", label: "Amount" }, { id: "address", label: "Address" }, { id: "order", label: "Contributing order / current status" }, { id: "time", label: "Block time" }]} rows={rows} rowKey={(payment) => `${payment.txid}:${payment.log_index}`} defaultSort="Oldest first by payment ID" caption={title} loading={worklist.isLoading} emptyState={empty} renderRow={(payment) => unattributed ? <UnattributedCells payment={payment} /> : <OrphanedCells payment={payment} />} /></div><div className="lg:hidden">{worklist.isLoading ? <DataTable columns={[{ id: "loading", label: "Loading" }]} rows={[]} rowKey={() => "loading"} defaultSort="Oldest first" caption={title} loading emptyState={empty} renderRow={() => null} /> : rows.length ? unattributed ? <UnattributedCards rows={rows} /> : <OrphanedCards rows={rows} /> : empty}</div>{failure ? <ErrorState error={failure} copyByCode={readCopy} lastUpdatedAt={worklist.dataUpdatedAt || undefined} pollingIntervalMs={LIST_INTERVAL} onRetry={() => void worklist.refetch()} /> : null}<CursorPager nextCursor={worklist.data?.next_cursor} hasResults={rows.length > 0} limit={limit} onNext={(next) => setParams({ cursor: next })} onStart={() => setParams({ cursor: "" })} onLimitChange={(next) => setParams({ limit: String(next) })} /></main>;
}
