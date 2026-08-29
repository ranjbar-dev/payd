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
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

type Grant = { id: string; withdrawal_id: string; receiver_address: string; resource_type: "ENERGY" | "BANDWIDTH"; source: string; stake_trx: string; txid: string; status: string; created_at: number; confirmed_at: number | null };
type GrantsPage = { grants: Grant[]; next_cursor: string };

function ReadProblem({ error, reload }: Readonly<{ error: unknown; reload: () => void }>) {
  if (!error) return null;
  const paydError = isPaydError(error) ? error : null;
  const code = paydError?.code ?? "upstream_unreachable";
  const copy: Record<string, string> = { unauthorized: "This dashboard session or its upstream scope is not authorised.", rate_limited: "Refresh has slowed because payd is rate limited.", upstream_unreachable: "payd could not be reached; any last available rows remain visible.", upstream_timeout: "payd did not answer in time; any last available rows remain visible." };
  return <ErrorState error={paydError ?? { code }} copyByCode={copy} onRetry={reload} />;
}

function GrantRow({ grant, tronscanBaseUrl }: Readonly<{ grant: Grant; tronscanBaseUrl: string }>) {
  const olderUnconfirmed = grant.confirmed_at == null && grant.created_at <= Math.floor(Date.now() / 1000) - 300;
  return <><td className="td"><EntityId value={grant.id} /></td><td className="td">{grant.withdrawal_id ? <Link href={`/withdrawals/${encodeURIComponent(grant.withdrawal_id)}`} className="cursor-pointer font-mono text-xs text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">{grant.withdrawal_id}</Link> : <span className="text-ink-faint">—</span>}</td><td className="td"><AddressLink address={grant.receiver_address} href={`/addresses/${encodeURIComponent(grant.receiver_address)}`} /></td><td className="td font-mono">{grant.resource_type}</td><td className="td font-mono">{grant.source}</td><td className="td text-right font-mono tabular-nums"><Amount value={grant.stake_trx} asset="TRX" /></td><td className="td">{grant.txid ? <TxidLink txid={grant.txid} tronscanBaseUrl={tronscanBaseUrl} /> : <span className="text-ink-faint">—</span>}</td><td className="td"><StatusBadge status={grant.status} /></td><td className="td text-right font-mono tabular-nums"><Timestamp seconds={grant.created_at} /></td><td className="td text-right font-mono tabular-nums"><Timestamp seconds={grant.confirmed_at} />{olderUnconfirmed ? <p className="mt-1 inline-flex items-center gap-1 text-[11px] text-severity-warning"><AlertTriangle aria-hidden="true" size={13} strokeWidth={1.75} />Unconfirmed for over five minutes; check chain.</p> : null}</td></>;
}

export function ResourceGrants() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const tronscanBaseUrl = useTronscanBaseUrl();
  const withdrawalId = searchParams.get("withdrawal_id") ?? "";
  const status = searchParams.get("grant_status") ?? "";
  const resourceType = searchParams.get("resource_type") ?? "";
  const cursor = searchParams.get("grant_cursor") ?? "";
  const limit = searchParams.get("grant_limit") === "200" ? 200 : 50;
  const query = new URLSearchParams({ limit: String(limit) });
  if (withdrawalId) query.set("withdrawal_id", withdrawalId);
  if (status) query.set("status", status);
  if (resourceType) query.set("resource_type", resourceType);
  if (cursor) query.set("cursor", cursor);
  const grants = useQuery(paydQueryOptions({ queryKey: queryKeys.resources.grants(Object.fromEntries(query)), queryFn: () => paydRequest<GrantsPage>(["resources", "grants"], {}, query), polling: { tier: "D" } }));
  const setParams = (next: Record<string, string>) => {
    const value = new URLSearchParams(searchParams);
    Object.entries(next).forEach(([key, item]) => item ? value.set(key, item) : value.delete(key));
    if (!("grant_cursor" in next)) value.delete("grant_cursor");
    router.replace(`${pathname}${value.size ? `?${value}` : ""}`);
  };
  const rows = grants.data?.grants ?? [];
  const active = Boolean(withdrawalId || status || resourceType);

  return <section id="grants" aria-labelledby="grants-heading" className="card space-y-3"><div className="flex flex-wrap items-start justify-between gap-3"><div><h2 id="grants-heading" className="card-title">Resource grants</h2><p className="mt-1 text-[13px] text-ink-secondary">An unresolved grant is resolved on chain rather than re-attempted.</p></div><button type="button" className="btn btn-secondary" onClick={() => void grants.refetch()}><RefreshCw aria-hidden="true" size={14} strokeWidth={1.75} />Refresh grants</button></div><div><div className="flex items-center gap-2"><Filter aria-hidden="true" size={14} strokeWidth={1.75} className="text-ink-faint" /><h3 className="text-[11px] font-semibold uppercase tracking-wide text-ink-faint">Filters</h3></div><TableFilters active={active} onClear={() => setParams({ withdrawal_id: "", grant_status: "", resource_type: "" })}><label className="field">Withdrawal<input value={withdrawalId} onChange={(event) => setParams({ withdrawal_id: event.currentTarget.value })} className="input font-mono" /></label><label className="field">Status<input value={status} onChange={(event) => setParams({ grant_status: event.currentTarget.value })} className="input" /></label><label className="field">Resource type<select value={resourceType} onChange={(event) => setParams({ resource_type: event.currentTarget.value })} className="input cursor-pointer transition-colors duration-150 hover:border-ink-faint focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"><option value="">Any</option><option value="ENERGY">ENERGY</option><option value="BANDWIDTH">BANDWIDTH</option></select></label></TableFilters></div><DataTable columns={[{ id: "id", label: "ID" }, { id: "withdrawal", label: "Withdrawal" }, { id: "address", label: "Address" }, { id: "type", label: "Resource" }, { id: "source", label: "Source" }, { id: "amount", label: "Amount (stake)", className: "text-right" }, { id: "txid", label: "Transaction" }, { id: "status", label: "Status" }, { id: "created", label: "Created", className: "text-right" }, { id: "confirmed", label: "Confirmed", className: "text-right" }]} rows={rows} rowKey={(grant) => grant.id} renderRow={(grant) => <GrantRow grant={grant} tronscanBaseUrl={tronscanBaseUrl} />} defaultSort="Backend newest-first cursor order" caption="Resource grants" loading={grants.isLoading} emptyState={<EmptyState kind="search" title={active ? "No grants match these filters" : "No grants in this cursor page"} description="Grants appear when payd delegates or self-stakes energy or bandwidth for a withdrawal." />} /><ReadProblem error={grants.isError ? grants.error : null} reload={() => void grants.refetch()} /><CursorPager nextCursor={grants.data?.next_cursor} hasResults={rows.length > 0} limit={limit} onNext={(next) => setParams({ grant_cursor: next })} onStart={() => setParams({ grant_cursor: "" })} onLimitChange={(next) => setParams({ grant_limit: String(next) })} /></section>;
}
