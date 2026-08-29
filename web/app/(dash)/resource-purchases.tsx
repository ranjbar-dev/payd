"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, Filter, RefreshCw } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

import { useTronscanBaseUrl } from "@/app/providers";
import { Amount } from "@/components/data/amount";
import { CursorPager } from "@/components/data/cursor-pager";
import { DataTable, TableFilters } from "@/components/data/data-table";
import { EmptyState } from "@/components/data/empty-state";
import { ErrorState } from "@/components/data/error-state";
import { AddressLink, EntityId, TxidLink } from "@/components/data/links";
import { StatusBadge } from "@/components/data/status-badge";
import { Timestamp } from "@/components/data/timestamp";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { EnergyPurchasePage } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

type Purchase = EnergyPurchasePage["purchases"][number];

function ReadProblem({ error, reload }: Readonly<{ error: unknown; reload: () => void }>) {
  if (!error) return null;
  const paydError = isPaydError(error) ? error : null;
  const code = paydError?.code ?? "upstream_unreachable";
  const copy: Record<string, string> = {
    invalid_status: "The purchase status filter is not recognised by payd.",
    unauthorized: "This dashboard session or its upstream scope is not authorised.",
    rate_limited: "Refresh has slowed because payd is rate limited.",
    upstream_unreachable: "payd could not be reached; any last available rows remain visible.",
    upstream_timeout: "payd did not answer in time; any last available rows remain visible.",
  };
  return <ErrorState error={paydError ?? { code }} copyByCode={copy} onRetry={reload} />;
}

function PurchaseRow({ purchase, tronscanBaseUrl }: Readonly<{ purchase: Purchase; tronscanBaseUrl: string }>) {
  return <><td className="td"><EntityId value={purchase.id} /></td><td className="td font-mono text-xs">{purchase.provider}</td><td className="td font-mono text-xs">{purchase.provider_order_id || "—"}</td><td className="td">{purchase.withdrawal_id ? <Link href={`/withdrawals/${encodeURIComponent(purchase.withdrawal_id)}`} className="cursor-pointer font-mono text-xs text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">{purchase.withdrawal_id}</Link> : <span className="text-ink-faint">—</span>}</td><td className="td"><AddressLink address={purchase.receiver_address} href={`/addresses/${encodeURIComponent(purchase.receiver_address)}`} /></td><td className="td font-mono">{purchase.resource_type}</td><td className="td text-right font-mono tabular-nums">{purchase.amount}</td><td className="td text-right font-mono tabular-nums"><Timestamp seconds={purchase.duration_seconds} variant="duration" /></td><td className="td text-right font-mono tabular-nums"><Amount value={purchase.quoted_trx} asset="TRX" /></td><td className="td text-right font-mono tabular-nums"><Amount value={purchase.actual_trx} asset="TRX" /></td><td className="td"><StatusBadge status={purchase.status} />{purchase.status === "purchased" ? <p className="mt-1 inline-flex items-center gap-1 text-[11px] text-severity-warning"><AlertTriangle aria-hidden="true" size={13} strokeWidth={1.75} />Paid; delegation pending</p> : null}</td><td className="td"><code className="select-all font-mono text-xs">{purchase.failure_reason || "—"}</code></td><td className="td">{purchase.delegation_txid ? <TxidLink txid={purchase.delegation_txid} tronscanBaseUrl={tronscanBaseUrl} /> : <span className="text-ink-faint">—</span>}</td><td className="td text-right font-mono tabular-nums"><Timestamp seconds={purchase.created_at} /></td><td className="td text-right font-mono tabular-nums"><Timestamp seconds={purchase.delegated_at} /></td></>;
}

export function ResourcePurchases() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const tronscanBaseUrl = useTronscanBaseUrl();
  const status = searchParams.get("purchase_status") ?? "";
  const cursor = searchParams.get("purchase_cursor") ?? "";
  const limit = searchParams.get("purchase_limit") === "200" ? 200 : 50;
  const query = new URLSearchParams({ limit: String(limit) });
  if (status) query.set("status", status);
  if (cursor) query.set("cursor", cursor);
  const purchases = useQuery(paydQueryOptions({ queryKey: queryKeys.energy.purchases(Object.fromEntries(query)), queryFn: () => paydRequest<EnergyPurchasePage>(["energy", "purchases"], {}, query), polling: { tier: "D" } }));
  const setParams = (next: Record<string, string>) => {
    const value = new URLSearchParams(searchParams);
    Object.entries(next).forEach(([key, item]) => item ? value.set(key, item) : value.delete(key));
    if (!("purchase_cursor" in next)) value.delete("purchase_cursor");
    router.replace(`${pathname}${value.size ? `?${value}` : ""}`);
  };
  const rows = purchases.data?.purchases ?? [];

  return <section aria-labelledby="purchases-heading" className="card space-y-3"><div className="flex flex-wrap items-start justify-between gap-3"><div><h2 id="purchases-heading" className="card-title">Energy purchases</h2><p className="mt-1 text-[13px] text-ink-secondary">Manual refresh only. Quoted and actual costs stay adjacent for provider-cost diagnosis.</p></div><button type="button" className="btn btn-secondary" onClick={() => void purchases.refetch()}><RefreshCw aria-hidden="true" size={14} strokeWidth={1.75} />Refresh purchase history</button></div><div><div className="flex items-center gap-2"><Filter aria-hidden="true" size={14} strokeWidth={1.75} className="text-ink-faint" /><h3 className="text-[11px] font-semibold uppercase tracking-wide text-ink-faint">Filters</h3></div><TableFilters active={Boolean(status)} onClear={() => setParams({ purchase_status: "" })}><label className="field">Status<select value={status} onChange={(event) => setParams({ purchase_status: event.currentTarget.value })} className="input cursor-pointer transition-colors duration-150 hover:border-ink-faint focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"><option value="">Any</option><option value="quoted">quoted</option><option value="purchased">purchased</option><option value="delegated">delegated</option><option value="expired">expired</option><option value="failed">failed</option></select></label></TableFilters></div><DataTable columns={[{ id: "id", label: "ID" }, { id: "provider", label: "Provider" }, { id: "provider-order", label: "Provider order" }, { id: "withdrawal", label: "Withdrawal" }, { id: "receiver", label: "Receiver" }, { id: "resource", label: "Resource" }, { id: "amount", label: "Amount", className: "text-right" }, { id: "duration", label: "Duration", className: "text-right" }, { id: "quoted", label: "Quoted TRX", className: "text-right" }, { id: "actual", label: "Actual TRX", className: "text-right" }, { id: "status", label: "Status" }, { id: "failure", label: "Failure reason" }, { id: "txid", label: "Delegation txid" }, { id: "created", label: "Created", className: "text-right" }, { id: "delegated", label: "Delegated", className: "text-right" }]} rows={rows} rowKey={(purchase) => purchase.id} renderRow={(purchase) => <PurchaseRow purchase={purchase} tronscanBaseUrl={tronscanBaseUrl} />} defaultSort="Backend newest-first cursor order" caption="Energy purchases" loading={purchases.isLoading} emptyState={<EmptyState kind="search" title={status ? "No purchases match this status" : "No purchases in this cursor page"} description="Purchases appear when payd records an energy acquisition attempt." />} /><ReadProblem error={purchases.isError ? purchases.error : null} reload={() => void purchases.refetch()} /><CursorPager nextCursor={purchases.data?.next_cursor} hasResults={rows.length > 0} limit={limit} onNext={(next) => setParams({ purchase_cursor: next })} onStart={() => setParams({ purchase_cursor: "" })} onLimitChange={(next) => setParams({ purchase_limit: String(next) })} /></section>;
}
