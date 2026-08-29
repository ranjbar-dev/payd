"use client";

import { useQuery } from "@tanstack/react-query";
import { Banknote, Filter, WalletCards } from "lucide-react";
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
import type { Withdrawal, WithdrawalLimits, WithdrawalList } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

import { ExportDialog } from "./export-dialog";

const LIST_INTERVAL = 30_000;
const progress = new Set(["awaiting_resources", "awaiting_energy", "signing", "broadcast"]);
const copyByCode: Record<string, string> = {
  unauthorized: "This dashboard session or its upstream scope is not authorised.",
  rate_limited: "Refresh has slowed because payd is rate limited.",
  upstream_unreachable: "payd could not be reached; showing the last available data when present.",
  upstream_timeout: "payd did not answer in time; showing the last available data when present.",
};

function address(value: string) {
  return <AddressLink address={value} href={`/addresses/${encodeURIComponent(value)}`} />;
}

function Elapsed({ status, updatedAt }: Readonly<{ status: string; updatedAt: number }>) {
  if (!progress.has(status)) return null;
  return <div className="mt-1 text-xs text-ink-secondary">In this state <Timestamp seconds={Math.floor(Date.now() / 1000) - updatedAt} variant="duration" /></div>;
}

function ReadError({ error, updatedAt, reload }: Readonly<{ error: unknown; updatedAt: number; reload: () => void }>) {
  if (!error) return null;
  const paydError = isPaydError(error) ? error : null;
  return <ErrorState error={{ code: paydError?.code ?? "upstream_unreachable", details: paydError?.details }} copyByCode={copyByCode} lastUpdatedAt={updatedAt || undefined} pollingIntervalMs={LIST_INTERVAL} onRetry={reload} />;
}

function Meter({ limits }: Readonly<{ limits?: WithdrawalLimits }>) {
  return <section className="card" aria-label="Daily withdrawal allowance">
    <div className="flex flex-wrap items-baseline justify-between gap-2"><h2 className="card-title">Daily withdrawal allowance</h2><p className="font-mono text-[11px] tabular-nums text-ink-faint">UTC — resets at 00:00 UTC</p></div>
    {limits ? <dl className="mt-3 grid gap-3 sm:grid-cols-3"><div><dt className="text-ink-faint text-[11px] uppercase">Used</dt><dd className="mt-1 text-right font-mono tabular-nums"><Amount value={limits.used_usd} asset="USD" variant="usd-snapshot" /></dd></div><div><dt className="text-ink-faint text-[11px] uppercase">Remaining</dt><dd className="mt-1 text-right font-mono tabular-nums"><Amount value={limits.remaining_usd} asset="USD" variant="usd-snapshot" /></dd></div><div><dt className="text-ink-faint text-[11px] uppercase">Cap</dt><dd className="mt-1 text-right font-mono tabular-nums"><Amount value={limits.daily_limit_usd} asset="USD" variant="usd-snapshot" /></dd></div></dl> : <div className="mt-3 grid gap-3 sm:grid-cols-3">{Array.from({ length: 3 }, (_, index) => <div key={index} className="space-y-2"><div className="h-3 w-12 animate-pulse bg-border-subtle" /><div className="ml-auto h-4 w-28 animate-pulse bg-border-subtle" /></div>)}</div>}
    <p className="mt-3 text-[12px] text-ink-secondary">The allowance includes requested, awaiting_resources, awaiting_energy, signing, broadcast, and confirmed withdrawals. In-flight withdrawals consume the cap.</p>
  </section>;
}

function Row({ withdrawal }: Readonly<{ withdrawal: Withdrawal }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  return <><td className="td"><EntityId value={withdrawal.id} /></td><td className="td"><StatusBadge status={withdrawal.status} /><Elapsed status={withdrawal.status} updatedAt={withdrawal.status_updated_at} /></td><td className="td font-mono tabular-nums">{withdrawal.asset}</td><td className="td text-right font-mono tabular-nums"><Amount value={withdrawal.amount} asset={withdrawal.asset} /></td><td className="td text-right font-mono tabular-nums"><Amount value={withdrawal.amount_usd} asset="USD" variant="usd-snapshot" /></td><td className="td">{address(withdrawal.from_address)}</td><td className="td">{address(withdrawal.to_address)}</td><td className="td">{withdrawal.txid ? <TxidLink txid={withdrawal.txid} tronscanBaseUrl={tronscanBaseUrl} /> : <span className="text-ink-faint">—</span>}</td><td className="td font-mono text-[11px] tabular-nums">{withdrawal.energy_source || "—"}</td><td className="td font-mono text-[11px] tabular-nums">{withdrawal.bandwidth_source || "—"}</td><td className="td text-right font-mono tabular-nums"><Amount value={withdrawal.total_cost_trx} asset="TRX" /></td><td className="td text-right font-mono tabular-nums"><Timestamp seconds={withdrawal.created_at} /></td><td className="td text-right font-mono tabular-nums"><Timestamp seconds={withdrawal.confirmed_at} /></td></>;
}

function Cards({ rows }: Readonly<{ rows: Withdrawal[] }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  return <div className="grid gap-3 lg:hidden">{rows.map((withdrawal) => <article key={withdrawal.id} className={`card ${withdrawal.status === "needs_operator" ? "needs-operator" : ""}`}><div className="flex items-start justify-between gap-2"><Link href={`/withdrawals/${encodeURIComponent(withdrawal.id)}`} className="cursor-pointer text-ink hover:text-accent focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"><EntityId value={withdrawal.id} /></Link><span><StatusBadge status={withdrawal.status} /><Elapsed status={withdrawal.status} updatedAt={withdrawal.status_updated_at} /></span></div><dl className="mt-3 grid gap-3 text-[13px]"><div><dt className="text-ink-faint text-[11px] uppercase">Amount / USD</dt><dd className="mt-1 text-right font-mono tabular-nums"><Amount value={withdrawal.amount} asset={withdrawal.asset} /> · <Amount value={withdrawal.amount_usd} asset="USD" variant="usd-snapshot" /></dd></div><div><dt className="text-ink-faint text-[11px] uppercase">From / to</dt><dd className="mt-1">{address(withdrawal.from_address)}<br />{address(withdrawal.to_address)}</dd></div><div><dt className="text-ink-faint text-[11px] uppercase">Transaction / resources</dt><dd className="mt-1">{withdrawal.txid ? <TxidLink txid={withdrawal.txid} tronscanBaseUrl={tronscanBaseUrl} /> : "—"}<br /><span className="font-mono text-[11px] tabular-nums">energy {withdrawal.energy_source || "—"} · bandwidth {withdrawal.bandwidth_source || "—"}</span></dd></div><div><dt className="text-ink-faint text-[11px] uppercase">Total cost / dates</dt><dd className="mt-1 text-right font-mono tabular-nums"><Amount value={withdrawal.total_cost_trx} asset="TRX" /> · created <Timestamp seconds={withdrawal.created_at} /> · confirmed <Timestamp seconds={withdrawal.confirmed_at} /></dd></div></dl></article>)}</div>;
}

export function WithdrawalsDashboard() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const status = searchParams.get("status") ?? "";
  const cursor = searchParams.get("cursor") ?? "";
  const limit = searchParams.get("limit") === "200" ? 200 : 50;
  const query = new URLSearchParams({ limit: String(limit) });
  if (status) query.set("status", status);
  if (cursor) query.set("cursor", cursor);
  const withdrawals = useQuery(paydQueryOptions({ queryKey: queryKeys.withdrawals.list(Object.fromEntries(query)), queryFn: () => paydRequest<WithdrawalList>(["withdrawals"], {}, query), polling: { tier: "B" } }));
  const limits = useQuery(paydQueryOptions({ queryKey: queryKeys.withdrawals.limits(), queryFn: () => paydRequest<WithdrawalLimits>(["withdrawals", "limits"]), polling: { tier: "B" } }));
  const setParams = (next: Record<string, string>) => {
    const value = new URLSearchParams(searchParams);
    Object.entries(next).forEach(([key, item]) => item ? value.set(key, item) : value.delete(key));
    if (!("cursor" in next)) value.delete("cursor");
    router.replace(`${pathname}${value.size ? `?${value}` : ""}`);
  };
  const items = withdrawals.data?.items ?? [];
  const rows = [...items.filter((item) => item.status === "needs_operator"), ...items.filter((item) => item.status !== "needs_operator")];

  return <main className="page"><header><p className="page-kicker"><Banknote aria-hidden="true" size={14} strokeWidth={1.75} />Operations / Withdrawals</p><div className="mt-1 flex flex-wrap items-end justify-between gap-3"><div><h1 className="page-title">Withdrawals</h1><p className="mt-1 text-[13px] text-ink-secondary">Newest first as returned by payd. Needs-operator records are pinned first because their outcome is unknown.</p></div>{/* WRPT-030/WRPT-036: the CURRENT list filter, straight through, not a separate export filter state. */}<ExportDialog kind="withdrawals" filters={{ status }} /></div></header>
    <Meter limits={limits.data} />
    <section className="card" aria-labelledby="withdrawal-filters-title"><div className="flex items-center gap-2"><Filter aria-hidden="true" size={14} strokeWidth={1.75} className="text-ink-faint" /><h2 id="withdrawal-filters-title" className="card-title">Filters</h2></div><div className="mt-3"><TableFilters active={Boolean(status)} onClear={() => setParams({ status: "" })}><label className="field">Status<select value={status} onChange={(event) => setParams({ status: event.currentTarget.value })} className="input cursor-pointer transition-colors duration-150 hover:border-ink-faint focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"><option value="">Any</option><option value="requested">requested</option><option value="awaiting_resources">awaiting_resources</option><option value="awaiting_energy">awaiting_energy</option><option value="signing">signing</option><option value="broadcast">broadcast</option><option value="confirmed">confirmed</option><option value="rejected">rejected</option><option value="failed">failed</option><option value="needs_operator">needs_operator</option></select></label></TableFilters></div></section>
    <section aria-labelledby="withdrawal-ledger-title"><div className="mb-2 flex items-center justify-between gap-3"><h2 id="withdrawal-ledger-title" className="card-title">Withdrawal ledger</h2><span className="font-mono text-[11px] tabular-nums text-ink-faint" data-count={rows.length}>{rows.length} loaded</span></div><div className="hidden lg:block"><DataTable columns={[{ id: "id", label: "ID" }, { id: "status", label: "Status" }, { id: "asset", label: "Asset" }, { id: "amount", label: "Amount", className: "text-right" }, { id: "usd", label: "USD snapshot", className: "text-right" }, { id: "from", label: "From" }, { id: "to", label: "To" }, { id: "txid", label: "Transaction" }, { id: "energy", label: "Energy source" }, { id: "bandwidth", label: "Bandwidth source" }, { id: "cost", label: "Total cost", className: "text-right" }, { id: "created", label: "Created", className: "text-right" }, { id: "confirmed", label: "Confirmed", className: "text-right" }]} rows={rows} rowKey={(item) => item.id} renderRow={(item) => <Row withdrawal={item} />} onRowClick={(item) => router.push(`/withdrawals/${encodeURIComponent(item.id)}`)} defaultSort="Backend newest-first cursor order; needs_operator pinned above all rows" caption="Withdrawals" loading={withdrawals.isLoading} emptyState={<EmptyState kind="search" title={status ? "No withdrawals match this status" : "No withdrawals in this cursor page"} description="Withdrawals appear after payd accepts a separate withdrawal request." icon={<WalletCards aria-hidden="true" size={20} strokeWidth={1.75} />} />} /></div>
    {!withdrawals.isLoading && rows.length ? <Cards rows={rows} /> : null}
    </section>
    <ReadError error={withdrawals.isError ? withdrawals.error : null} updatedAt={withdrawals.dataUpdatedAt} reload={() => void withdrawals.refetch()} />
    <ReadError error={limits.isError ? limits.error : null} updatedAt={limits.dataUpdatedAt} reload={() => void limits.refetch()} />
    <CursorPager nextCursor={withdrawals.data?.next_cursor} hasResults={rows.length > 0} limit={limit} onNext={(next) => setParams({ cursor: next })} onStart={() => setParams({ cursor: "" })} onLimitChange={(next) => setParams({ limit: String(next) })} />
  </main>;
}
