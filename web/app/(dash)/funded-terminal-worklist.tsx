"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useState } from "react";

import { Amount } from "@/components/data/amount";
import { CursorPager } from "@/components/data/cursor-pager";
import { DataTable } from "@/components/data/data-table";
import { EmptyState } from "@/components/data/empty-state";
import { AddressLink, EntityId } from "@/components/data/links";
import { StatusBadge } from "@/components/data/status-badge";
import { Timestamp } from "@/components/data/timestamp";
import { ConfirmDialog } from "@/components/forms/confirm-dialog";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { FundedOrder, FundedOrderList } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

function Payers({ order }: Readonly<{ order: FundedOrder }>) {
  return <div className="space-y-1">{order.payers.map((payer) => <AddressLink key={payer} address={payer} href={`/addresses/${encodeURIComponent(payer)}`} />)}</div>;
}

function ResolveAction({ order }: Readonly<{ order: FundedOrder }>) {
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [resolution, setResolution] = useState("refunded");
  const [note, setNote] = useState("");
  const mutation = useMutation({ mutationFn: () => paydRequest<{ resolved: true }>(["orders", order.id, "resolve"], { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ resolution, resolution_note: note }) }), onSuccess: () => { client.setQueriesData({ queryKey: queryKeys.orders.fundedTerminalAll() }, (old: unknown) => old && typeof old === "object" && "orders" in old && Array.isArray((old as FundedOrderList).orders) ? { ...(old as FundedOrderList), orders: (old as FundedOrderList).orders.filter((item) => item.id !== order.id) } : old); void client.invalidateQueries({ queryKey: queryKeys.orders.all }); void client.invalidateQueries({ queryKey: queryKeys.stats() }); } });
  const payd = isPaydError(mutation.error) ? mutation.error : null;
  const withdrawalHref = `/withdrawals/new?from=${encodeURIComponent(order.address)}&to=${encodeURIComponent(order.payers[0] ?? "")}`;
  return <><button type="button" className="border border-severity-warning px-2 py-1 text-xs" onClick={() => { mutation.reset(); setOpen(true); }}>Resolve</button><ConfirmDialog open={open} onClose={() => setOpen(false)} title="Record funded-terminal resolution" confirmLabel={`Record ${resolution}`} ready={note.trim().length > 0} onConfirm={async () => { try { await mutation.mutateAsync(); setOpen(false); } catch { /* The error is rendered; no automatic resubmission. */ } }} error={payd} apiText={<><p>payd order <code className="select-all font-mono">{order.id}</code> received <Amount value={order.received} asset={order.asset} /> at <code className="select-all font-mono">{order.address}</code>.</p><p className="mt-2 font-medium">This records a decision; it does not move any funds. Choosing refunded does not issue a refund. A refund is a separate withdrawal made afterwards from the deposit address.</p><p className="mt-2">This action is written to <code className="font-mono">audit_log</code>. No TOTP code is required.</p><label className="mt-3 grid gap-1 text-xs text-ink-secondary">Resolution<select value={resolution} onChange={(event) => setResolution(event.currentTarget.value)} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink"><option value="refunded">refunded</option><option value="written_off">written_off</option><option value="reattributed">reattributed</option></select></label><label className="mt-3 grid gap-1 text-xs text-ink-secondary">Resolution note (required)<textarea value={note} onChange={(event) => setNote(event.currentTarget.value)} rows={3} className="border border-border-strong bg-panel p-2 text-sm text-ink" /></label>{resolution === "refunded" ? <Link href={withdrawalHref} className="text-severity-progress underline underline-offset-2">Open a separate, pre-filled withdrawal</Link> : null}{payd ? <p role="alert" className="text-severity-warning">payd did not record the decision. Error code: <code className="select-all font-mono">{payd.code}</code></p> : null}</>} /></>;
}

export function FundedTerminalWorklist() {
  const router = useRouter();
  const pathname = usePathname();
  const search = useSearchParams();
  const cursor = search.get("cursor") ?? "";
  const limit = search.get("limit") === "200" ? 200 : 50;
  const filter = search.get("q") ?? "";
  const query = new URLSearchParams({ limit: String(limit) });
  if (cursor) query.set("cursor", cursor);
  const worklist = useQuery(paydQueryOptions({ queryKey: queryKeys.orders.fundedTerminal(Object.fromEntries(query)), queryFn: () => paydRequest<FundedOrderList>(["orders", "funded-terminal"], {}, query), polling: { tier: "B" } }));
  const setParams = (next: Record<string, string>) => { const value = new URLSearchParams(search); Object.entries(next).forEach(([key, item]) => item ? value.set(key, item) : value.delete(key)); if (!("cursor" in next)) value.delete("cursor"); router.replace(`${pathname}${value.size ? `?${value}` : ""}`); };
  const all = [...(worklist.data?.orders ?? [])].sort((left, right) => left.created_at - right.created_at);
  const rows = filter ? all.filter((order) => order.id.includes(filter) || order.payers.some((payer) => payer.includes(filter))) : all;
  const failure = isPaydError(worklist.error) ? worklist.error : null;
  const empty = filter ? <EmptyState kind="search" title="No funded terminal orders match this filter" description="Change or clear the URL-backed filter to inspect unresolved funded orders." /> : <EmptyState kind="worklist" title="No unresolved funded orders" description="No customer money is sitting unresolved in a terminal order." />;

  return <main className="mx-auto max-w-7xl space-y-4 p-4 lg:p-6"><header><p className="font-mono text-xs uppercase tracking-[0.2em] text-ink-faint">Operations / Orders / Funded terminal</p><h1 className="mt-1 text-2xl font-semibold">Funded-terminal worklist</h1><p className="mt-1 text-sm text-ink-secondary">Oldest first. Age is the risk.</p></header><label className="grid max-w-md gap-1 text-xs text-ink-secondary">Filter order ID or payer address<input value={filter} onChange={(event) => setParams({ q: event.currentTarget.value })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>{filter ? <button type="button" className="text-left text-sm underline underline-offset-2" onClick={() => setParams({ q: "" })}>Clear filter</button> : null}<DataTable columns={[{ id: "order", label: "Order" }, { id: "status", label: "Status" }, { id: "asset", label: "Asset" }, { id: "received", label: "Received" }, { id: "payers", label: "Payer addresses" }, { id: "age", label: "Age" }, { id: "action", label: "Action" }]} rows={rows} rowKey={(order) => order.id} defaultSort="Oldest first by created time" caption="Unresolved funded terminal orders" loading={worklist.isLoading} emptyState={empty} renderRow={(order) => <><td className="px-3 py-2"><Link href={`/orders/${encodeURIComponent(order.id)}`}><EntityId value={order.id} /></Link></td><td className="px-3 py-2"><StatusBadge status={order.status} resolution={order.resolution} /></td><td className="px-3 py-2 font-mono">{order.asset}</td><td className="px-3 py-2"><Amount value={order.received} asset={order.asset} /></td><td className="px-3 py-2"><Payers order={order} /></td><td className="px-3 py-2"><Timestamp seconds={order.created_at} /></td><td className="px-3 py-2"><ResolveAction order={order} /></td></>} />{failure ? <p role="alert" className="border border-severity-warning bg-[var(--severity-warning-bg)] p-3">The worklist could not refresh; the last good rows remain visible. Error code: <code className="select-all font-mono">{failure.code}</code></p> : null}<CursorPager nextCursor={worklist.data?.next_cursor} hasResults={rows.length > 0} limit={limit} onNext={(next) => setParams({ cursor: next })} onStart={() => setParams({ cursor: "" })} onLimitChange={(next) => setParams({ limit: String(next) })} /></main>;
}
