"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, ExternalLink, Filter, Terminal, X } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useState } from "react";

import { Amount } from "@/components/data/amount";
import { CursorPager } from "@/components/data/cursor-pager";
import { DataTable } from "@/components/data/data-table";
import { EmptyState } from "@/components/data/empty-state";
import { ErrorState } from "@/components/data/error-state";
import { AddressLink, EntityId } from "@/components/data/links";
import { RefreshButton } from "@/components/data/refresh-button";
import { StatusBadge } from "@/components/data/status-badge";
import { Timestamp } from "@/components/data/timestamp";
import { ConfirmDialog } from "@/components/forms/confirm-dialog";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { FundedOrder, FundedOrderList } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

function Payers({ order }: Readonly<{ order: FundedOrder }>) {
  return <div className="space-y-1 text-[13px]">{order.payers.map((payer) => <AddressLink key={payer} address={payer} href={`/addresses/${encodeURIComponent(payer)}`} />)}</div>;
}

function ResolveAction({ order }: Readonly<{ order: FundedOrder }>) {
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [resolution, setResolution] = useState("refunded");
  const [note, setNote] = useState("");
  const mutation = useMutation({ mutationFn: () => paydRequest<{ resolved: true }>(["orders", order.id, "resolve"], { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ resolution, resolution_note: note }) }), onSuccess: () => { client.setQueriesData({ queryKey: queryKeys.orders.fundedTerminalAll() }, (old: unknown) => old && typeof old === "object" && "orders" in old && Array.isArray((old as FundedOrderList).orders) ? { ...(old as FundedOrderList), orders: (old as FundedOrderList).orders.filter((item) => item.id !== order.id) } : old); void client.invalidateQueries({ queryKey: queryKeys.orders.all }); void client.invalidateQueries({ queryKey: queryKeys.stats() }); } });
  const payd = isPaydError(mutation.error) ? mutation.error : null;
  const withdrawalHref = `/withdrawals/new?from=${encodeURIComponent(order.address)}&to=${encodeURIComponent(order.payers[0] ?? "")}`;
  return <>
    <button
      type="button"
      className="btn btn-secondary h-7 px-2 text-xs"
      disabled={mutation.isPending}
      onClick={() => { mutation.reset(); setOpen(true); }}
    >
      <Check aria-hidden="true" size={14} strokeWidth={1.75} />
      Resolve
    </button>
    <ConfirmDialog
      open={open}
      onClose={() => setOpen(false)}
      title="Record funded-terminal resolution"
      confirmLabel={`Record ${resolution}`}
      ready={note.trim().length > 0}
      onConfirm={async () => {
        try {
          await mutation.mutateAsync();
          setOpen(false);
        } catch {
          /* The error is rendered; no automatic resubmission. */
        }
      }}
      error={payd}
      apiText={<>
        <p>payd order <code className="select-all font-mono">{order.id}</code> received <Amount value={order.received} asset={order.asset} /> at <code className="select-all font-mono">{order.address}</code>.</p>
        <p className="mt-2 font-medium">This records a decision; it does not move any funds. Choosing refunded does not issue a refund. A refund is a separate withdrawal made afterwards from the deposit address.</p>
        <p className="mt-2">This action is written to <code className="font-mono">audit_log</code>. No TOTP code is required.</p>
        <label className="field mt-3">
          Resolution
          <select value={resolution} onChange={(event) => setResolution(event.currentTarget.value)} className="input cursor-pointer">
            <option value="refunded">refunded</option>
            <option value="written_off">written_off</option>
            <option value="reattributed">reattributed</option>
          </select>
        </label>
        <label className="field mt-3">
          Resolution note (required)
          <textarea value={note} onChange={(event) => setNote(event.currentTarget.value)} rows={3} className="min-h-20 w-full rounded border border-border-subtle bg-raised p-2 text-[13px] text-ink focus-visible:border-[var(--focus-ring)] focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]" />
        </label>
        {resolution === "refunded" ? <Link href={withdrawalHref} className="mt-3 inline-flex cursor-pointer items-center gap-1.5 text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"><ExternalLink aria-hidden="true" size={14} strokeWidth={1.75} />Open a separate, pre-filled withdrawal</Link> : null}
        {payd ? <p role="alert" className="mt-3 text-severity-warning">payd did not record the decision. Error code: <code className="select-all font-mono">{payd.code}</code></p> : null}
      </>}
    />
  </>;
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

  return <main className="page">
    <header>
      <p className="page-kicker"><Terminal aria-hidden="true" size={14} strokeWidth={1.75} />OPERATIONS / ORDERS / FUNDED TERMINAL</p>
      <div className="mt-1 flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="page-title">Funded-terminal worklist</h1>
          <p className="mt-1 text-[13px] text-ink-secondary">Oldest first. Age is the risk.</p>
        </div><div className="flex flex-wrap gap-2"><RefreshButton /></div>
      </div>
    </header>

    <section className="card space-y-3" aria-labelledby="funded-terminal-heading">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 id="funded-terminal-heading" className="card-title">Unresolved funds</h2>
          <p className="mt-1 text-[13px] text-ink-secondary">Each payer address is copyable for a separate, deliberate refund workflow.</p>
        </div>
        {filter ? <button type="button" className="btn btn-ghost" onClick={() => setParams({ q: "" })}><X aria-hidden="true" size={14} strokeWidth={1.75} />Clear filter</button> : null}
      </div>

      <label className="field max-w-md">
        <span className="flex items-center gap-1.5"><Filter aria-hidden="true" size={14} strokeWidth={1.75} />Filter order ID or payer address</span>
        <input value={filter} onChange={(event) => setParams({ q: event.currentTarget.value })} className="input" />
      </label>

      <DataTable
        columns={[
          { id: "order", label: "Order" },
          { id: "status", label: "Status" },
          { id: "asset", label: "Asset" },
          { id: "received", label: "Received", className: "text-right" },
          { id: "payers", label: "Payer addresses" },
          { id: "age", label: "Age", className: "text-right" },
          { id: "action", label: "Action", className: "text-right" },
        ]}
        rows={rows}
        rowKey={(order) => order.id}
        defaultSort="Oldest first by created time"
        caption="Unresolved funded terminal orders"
        loading={worklist.isLoading}
        emptyState={empty}
        renderRow={(order) => <>
          <td className="td whitespace-nowrap"><Link href={`/orders/${encodeURIComponent(order.id)}`} className="cursor-pointer transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"><EntityId value={order.id} /></Link></td>
          <td className="td whitespace-nowrap"><StatusBadge status={order.status} resolution={order.resolution} /></td>
          <td className="td font-mono tabular-nums">{order.asset}</td>
          <td className="td text-right font-mono tabular-nums"><Amount value={order.received} asset={order.asset} /></td>
          <td className="td"><Payers order={order} /></td>
          <td className="td whitespace-nowrap text-right font-mono tabular-nums"><Timestamp seconds={order.created_at} /></td>
          <td className="td text-right"><ResolveAction order={order} /></td>
        </>}
      />
    </section>

    {failure ? <ErrorState error={failure} copyByCode={{ unauthorized: "This dashboard session or its upstream scope is not authorised.", rate_limited: "Refresh has slowed because payd is rate limited.", upstream_unreachable: "payd could not be reached; the last good rows remain visible.", upstream_timeout: "payd did not answer in time; the last good rows remain visible." }} lastUpdatedAt={worklist.dataUpdatedAt || undefined} pollingIntervalMs={30_000} onRetry={() => void worklist.refetch()} /> : null}
    <CursorPager nextCursor={worklist.data?.next_cursor} hasResults={rows.length > 0} limit={limit} onNext={(next) => setParams({ cursor: next })} onStart={() => setParams({ cursor: "" })} onLimitChange={(next) => setParams({ limit: String(next) })} />
  </main>;
}
