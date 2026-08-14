"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

import { useTronscanBaseUrl } from "@/app/providers";
import { Amount } from "@/components/data/amount";
import { CursorPager } from "@/components/data/cursor-pager";
import { DataTable, TableFilters } from "@/components/data/data-table";
import { EmptyState } from "@/components/data/empty-state";
import { AddressLink, EntityId, TxidLink } from "@/components/data/links";
import { StatusBadge } from "@/components/data/status-badge";
import { Timestamp } from "@/components/data/timestamp";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { Withdrawal, WithdrawalLimits, WithdrawalList } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

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
  const stale = updatedAt > 0 && Date.now() - updatedAt > LIST_INTERVAL * 3;
  return <div className="border border-severity-warning bg-[var(--severity-warning-bg)] p-3" role="alert">
    {stale ? <p className="mb-2 text-sm text-severity-warning">Showing stale data; last updated {Math.floor((Date.now() - updatedAt) / 60_000)}m ago.</p> : null}
    <p className="font-medium">{copyByCode[paydError?.code ?? "upstream_unreachable"] ?? "An unrecognised error was returned."}</p>
    <p className="mt-1 text-sm text-ink-secondary">Error code: <code className="select-all font-mono text-ink">{paydError?.code ?? "upstream_unreachable"}</code></p>
    {paydError?.details ? <pre className="mt-2 overflow-auto border-t border-border-subtle pt-2 text-xs text-ink-secondary">{JSON.stringify(paydError.details, null, 2)}</pre> : null}
    <button type="button" className="mt-3 border border-border-strong px-3 py-1.5 text-sm hover:bg-raised" onClick={reload}>Reload</button>
  </div>;
}

function Meter({ limits }: Readonly<{ limits?: WithdrawalLimits }>) {
  return <section className="border border-border-subtle bg-panel p-4" aria-label="Daily withdrawal allowance">
    <div className="flex flex-wrap items-baseline justify-between gap-2"><h2 className="font-semibold">Daily withdrawal allowance</h2><p className="font-mono text-xs text-ink-secondary">UTC — resets at 00:00 UTC</p></div>
    {limits ? <dl className="mt-3 grid gap-3 sm:grid-cols-3"><div><dt className="text-xs uppercase tracking-wide text-ink-faint">Used</dt><dd className="mt-1"><Amount value={limits.used_usd} asset="USD" variant="usd-snapshot" /></dd></div><div><dt className="text-xs uppercase tracking-wide text-ink-faint">Remaining</dt><dd className="mt-1"><Amount value={limits.remaining_usd} asset="USD" variant="usd-snapshot" /></dd></div><div><dt className="text-xs uppercase tracking-wide text-ink-faint">Cap</dt><dd className="mt-1"><Amount value={limits.daily_limit_usd} asset="USD" variant="usd-snapshot" /></dd></div></dl> : <p className="mt-3 text-sm text-ink-secondary">Loading allowance…</p>}
    <p className="mt-3 text-xs text-ink-secondary">The allowance includes requested, awaiting_resources, awaiting_energy, signing, broadcast, and confirmed withdrawals. In-flight withdrawals consume the cap.</p>
  </section>;
}

function Row({ withdrawal }: Readonly<{ withdrawal: Withdrawal }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  return <><td className="px-3 py-2"><EntityId value={withdrawal.id} /></td><td className="px-3 py-2"><StatusBadge status={withdrawal.status} /><Elapsed status={withdrawal.status} updatedAt={withdrawal.status_updated_at} /></td><td className="px-3 py-2 font-mono">{withdrawal.asset}</td><td className="px-3 py-2"><Amount value={withdrawal.amount} asset={withdrawal.asset} /></td><td className="px-3 py-2"><Amount value={withdrawal.amount_usd} asset="USD" variant="usd-snapshot" /></td><td className="px-3 py-2">{address(withdrawal.from_address)}</td><td className="px-3 py-2">{address(withdrawal.to_address)}</td><td className="px-3 py-2">{withdrawal.txid ? <TxidLink txid={withdrawal.txid} tronscanBaseUrl={tronscanBaseUrl} /> : <span className="text-ink-faint">—</span>}</td><td className="px-3 py-2 font-mono text-xs">{withdrawal.energy_source || "—"}</td><td className="px-3 py-2 font-mono text-xs">{withdrawal.bandwidth_source || "—"}</td><td className="px-3 py-2"><Amount value={withdrawal.total_cost_trx} asset="TRX" /></td><td className="px-3 py-2"><Timestamp seconds={withdrawal.created_at} /></td><td className="px-3 py-2"><Timestamp seconds={withdrawal.confirmed_at} /></td></>;
}

function Cards({ rows }: Readonly<{ rows: Withdrawal[] }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  return <div className="grid gap-2 lg:hidden">{rows.map((withdrawal) => <article key={withdrawal.id} className={withdrawal.status === "needs_operator" ? "border-2 border-severity-critical bg-panel p-3" : "border border-border-subtle bg-panel p-3"}><div className="flex items-start justify-between gap-2"><Link href={`/withdrawals/${encodeURIComponent(withdrawal.id)}`} className="focus-visible:outline-offset-2"><EntityId value={withdrawal.id} /></Link><span><StatusBadge status={withdrawal.status} /><Elapsed status={withdrawal.status} updatedAt={withdrawal.status_updated_at} /></span></div><dl className="mt-3 grid gap-2 text-sm"><div><dt className="text-xs text-ink-faint">Amount / USD</dt><dd><Amount value={withdrawal.amount} asset={withdrawal.asset} /> · <Amount value={withdrawal.amount_usd} asset="USD" variant="usd-snapshot" /></dd></div><div><dt className="text-xs text-ink-faint">From / to</dt><dd>{address(withdrawal.from_address)}<br />{address(withdrawal.to_address)}</dd></div><div><dt className="text-xs text-ink-faint">Transaction / resources</dt><dd>{withdrawal.txid ? <TxidLink txid={withdrawal.txid} tronscanBaseUrl={tronscanBaseUrl} /> : "—"}<br /><span className="font-mono text-xs">energy {withdrawal.energy_source || "—"} · bandwidth {withdrawal.bandwidth_source || "—"}</span></dd></div><div><dt className="text-xs text-ink-faint">Total cost / dates</dt><dd><Amount value={withdrawal.total_cost_trx} asset="TRX" /> · created <Timestamp seconds={withdrawal.created_at} /> · confirmed <Timestamp seconds={withdrawal.confirmed_at} /></dd></div></dl></article>)}</div>;
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

  return <main className="mx-auto max-w-7xl space-y-4 p-4 lg:p-6"><header><p className="font-mono text-xs uppercase tracking-[0.2em] text-ink-faint">Operations / Withdrawals</p><h1 className="mt-1 text-2xl font-semibold">Withdrawals</h1><p className="mt-1 text-sm text-ink-secondary">Newest first as returned by payd. Needs-operator records are pinned first because their outcome is unknown.</p></header>
    <Meter limits={limits.data} />
    <TableFilters active={Boolean(status)} onClear={() => setParams({ status: "" })}><label className="grid gap-1 text-xs text-ink-secondary">Status<select value={status} onChange={(event) => setParams({ status: event.currentTarget.value })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink"><option value="">Any</option><option value="requested">requested</option><option value="awaiting_resources">awaiting_resources</option><option value="awaiting_energy">awaiting_energy</option><option value="signing">signing</option><option value="broadcast">broadcast</option><option value="confirmed">confirmed</option><option value="rejected">rejected</option><option value="failed">failed</option><option value="needs_operator">needs_operator</option></select></label></TableFilters>
    <div className="hidden lg:block"><DataTable columns={[{ id: "id", label: "ID" }, { id: "status", label: "Status" }, { id: "asset", label: "Asset" }, { id: "amount", label: "Amount" }, { id: "usd", label: "USD snapshot" }, { id: "from", label: "From" }, { id: "to", label: "To" }, { id: "txid", label: "Transaction" }, { id: "energy", label: "Energy source" }, { id: "bandwidth", label: "Bandwidth source" }, { id: "cost", label: "Total cost" }, { id: "created", label: "Created" }, { id: "confirmed", label: "Confirmed" }]} rows={rows} rowKey={(item) => item.id} renderRow={(item) => <Row withdrawal={item} />} onRowClick={(item) => router.push(`/withdrawals/${encodeURIComponent(item.id)}`)} defaultSort="Backend newest-first cursor order; needs_operator pinned above all rows" caption="Withdrawals" loading={withdrawals.isLoading} emptyState={<EmptyState kind={status ? "search" : "search"} title={status ? "No withdrawals match this status" : "No withdrawals in this cursor page"} description="Withdrawals appear after payd accepts a separate withdrawal request." />} /></div>
    {!withdrawals.isLoading && rows.length ? <Cards rows={rows} /> : null}
    <ReadError error={withdrawals.isError ? withdrawals.error : null} updatedAt={withdrawals.dataUpdatedAt} reload={() => void withdrawals.refetch()} />
    <ReadError error={limits.isError ? limits.error : null} updatedAt={limits.dataUpdatedAt} reload={() => void limits.refetch()} />
    <CursorPager nextCursor={withdrawals.data?.next_cursor} hasResults={rows.length > 0} limit={limit} onNext={(next) => setParams({ cursor: next })} onStart={() => setParams({ cursor: "" })} onLimitChange={(next) => setParams({ limit: String(next) })} />
  </main>;
}
