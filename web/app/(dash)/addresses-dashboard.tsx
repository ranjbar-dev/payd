"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

import { Amount } from "@/components/data/amount";
import { CursorPager } from "@/components/data/cursor-pager";
import { DataTable, TableFilters } from "@/components/data/data-table";
import { EmptyState } from "@/components/data/empty-state";
import { ErrorState } from "@/components/data/error-state";
import { AddressLink } from "@/components/data/links";
import { StatusBadge } from "@/components/data/status-badge";
import { Timestamp } from "@/components/data/timestamp";
import { AddressDelegate } from "@/app/(dash)/address-delegate";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { ConfigResponse, OperationalStats, ResourceWalletResponse, WalletPage, WalletResource, WalletResourceList } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

const LIST_INTERVAL = 30_000;
const copyByCode: Record<string, string> = {
  unauthorized: "This dashboard session or its upstream scope is not authorised.",
  rate_limited: "Refresh has slowed because payd is rate limited.",
  upstream_unreachable: "payd could not be reached; showing the last available data when present.",
  upstream_timeout: "payd did not answer in time; showing the last available data when present.",
};

type Filters = Record<"state" | "has_balance" | "needs_resources" | "drift_detected" | "asset", string>;

function filtersFrom(params: URLSearchParams): Filters {
  return {
    state: params.get("state") ?? "",
    has_balance: params.get("has_balance") ?? "",
    needs_resources: params.get("needs_resources") ?? "",
    drift_detected: params.get("drift_detected") ?? "",
    asset: params.get("asset") ?? "",
  };
}

function address(value: string) {
  return <AddressLink address={value} href={`/addresses/${encodeURIComponent(value)}`} />;
}

function Balances({ wallet }: Readonly<{ wallet: WalletResource }>) {
  return <div className="space-y-2">{wallet.balances.map((balance) => <div key={balance.asset} className="font-mono text-xs tabular-nums"><div className="text-ink-secondary">{balance.asset}</div><div><span className="mr-1 text-ink-faint">Confirmed</span><Amount value={balance.confirmed} asset={balance.asset} variant="compact" /></div><div><span className="mr-1 text-ink-faint">Pending</span><Amount value={balance.pending} asset={balance.asset} variant="compact" /></div>{wallet.can_withdraw[balance.asset] ? <div className="text-severity-success">Can withdraw</div> : <div className="inline-flex items-center gap-1 text-severity-warning"><AlertTriangle aria-hidden="true" size={13} />Cannot withdraw</div>}</div>)}</div>;
}

function ResourceFlag({ wallet }: Readonly<{ wallet: WalletResource }>) {
  if (!wallet.needs_resources) return <span className="text-severity-success">Sufficient</span>;
  return <span className="inline-flex items-center gap-1 text-severity-warning"><AlertTriangle aria-hidden="true" size={14} />Needs {wallet.blocked_by.join(" and ") || "resources"}</span>;
}

function DriftFlag({ wallet }: Readonly<{ wallet: WalletResource }>) {
  return wallet.drift_detected ? <StatusBadge status="drift_detected" /> : <span className="text-ink-faint">No drift</span>;
}

function extra(wallet: WalletResource, key: string): unknown {
  return (wallet as WalletResource & Record<string, unknown>)[key];
}

function CoolingUntil({ wallet }: Readonly<{ wallet: WalletResource }>) {
  const value = extra(wallet, "cooling_until");
  if (wallet.state !== "cooling") return <span className="text-ink-faint">—</span>;
  if (typeof value !== "number") return <span className="text-ink-secondary">Cooldown time not supplied</span>;
  const minutes = Math.ceil((value * 1000 - Date.now()) / 60_000);
  return <><Timestamp seconds={value} /><div className="mt-1 text-xs text-ink-secondary">{minutes > 0 ? `${minutes}m remaining` : "Awaiting pool refresh"}</div></>;
}

function Assignment({ wallet }: Readonly<{ wallet: WalletResource }>) {
  const value = extra(wallet, "assigned_order_id");
  return typeof value === "string" && value ? <Link href={`/orders/${encodeURIComponent(value)}`} className="font-mono text-xs text-severity-progress underline underline-offset-2" onClick={(event) => event.stopPropagation()}>{value}</Link> : <span className="text-ink-faint">—</span>;
}

function WalletRow({ wallet, resourceWallet }: Readonly<{ wallet: WalletResource; resourceWallet?: string }>) {
  const isResourceWallet = resourceWallet === wallet.address;
  return <><td className="px-3 py-2">{address(wallet.address)}{isResourceWallet ? <div className="mt-1 text-xs text-ink-secondary">Resource wallet — permanently disabled</div> : null}</td><td className="px-3 py-2 font-mono tabular-nums">{wallet.hd_index}</td><td className="px-3 py-2"><StatusBadge status={wallet.state} /></td><td className="px-3 py-2"><Balances wallet={wallet} /></td><td className="px-3 py-2"><ResourceFlag wallet={wallet} /></td><td className="px-3 py-2"><DriftFlag wallet={wallet} /></td><td className="px-3 py-2"><Assignment wallet={wallet} /></td><td className="px-3 py-2"><CoolingUntil wallet={wallet} /></td><td className="px-3 py-2"><Timestamp seconds={wallet.checked_at} /></td></>;
}

function ErrorNotice({ error, updatedAt, retry }: Readonly<{ error: unknown; updatedAt: number; retry: () => void }>) {
  if (!error) return null;
  const paydError = isPaydError(error) ? error : null;
  return <ErrorState error={{ code: paydError?.code ?? "upstream_unreachable", details: paydError?.details }} copyByCode={copyByCode} lastUpdatedAt={updatedAt || undefined} pollingIntervalMs={LIST_INTERVAL} onRetry={retry} />;
}

function poolCounts(stats?: OperationalStats) {
  const addresses = stats?.addresses;
  if (typeof addresses !== "object" || addresses === null || Array.isArray(addresses)) return null;
  const record = addresses as Record<string, unknown>;
  const counts = ["free", "assigned", "cooling", "disabled"].map((state) => record[state]);
  if (!counts.every((count) => typeof count === "number")) return null;
  const [free, assigned, cooling, disabled] = counts as number[];
  return { free, assigned, cooling, disabled, total: free + assigned + cooling + disabled };
}

function PoolHealth({ stats, config }: Readonly<{ stats?: OperationalStats; config?: ConfigResponse }>) {
  const counts = poolCounts(stats);
  if (!counts) return <p className="text-sm text-ink-secondary">Pool health unavailable.</p>;
  if (!config) return <p className="text-sm text-ink-secondary">Pool health thresholds are loading from configuration.</p>;
  return <p className="font-mono text-sm tabular-nums text-ink-secondary">Pool health · free: {counts.free} / min {config.wallet.pool_min_free} · total: {counts.total} / max {config.wallet.pool_max_size} · assigned: {counts.assigned} · cooling: {counts.cooling} · disabled: {counts.disabled}</p>;
}

function PoolList() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const filters = filtersFrom(searchParams);
  const cursor = searchParams.get("cursor") ?? "";
  const limit = searchParams.get("limit") === "200" ? 200 : 50;
  const query = new URLSearchParams({ limit: String(limit) });
  if (cursor) query.set("cursor", cursor);
  const specialFilter = filters.has_balance ? "with-balance" : filters.needs_resources ? "needs-resources" : null;
  if (!specialFilter) {
    if (filters.state) query.set("state", filters.state);
    if (filters.asset) query.set("asset", filters.asset);
    if (filters.drift_detected) query.set("drift", "true");
  }
  const walletQuery = specialFilter ? ["wallets", specialFilter] : ["wallets"];
  const wallets = useQuery(paydQueryOptions({
    queryKey: specialFilter === "with-balance" ? queryKeys.wallets.withBalance(Object.fromEntries(query)) : specialFilter === "needs-resources" ? queryKeys.wallets.needsResources() : queryKeys.wallets.list(Object.fromEntries(query)),
    queryFn: () => paydRequest<WalletPage | WalletResourceList>(walletQuery, {}, query),
    polling: { tier: "B" },
  }));
  const config = useQuery(paydQueryOptions({ queryKey: queryKeys.config(), queryFn: () => paydRequest<ConfigResponse>(["config"]), polling: { tier: "D" } }));
  const stats = useQuery(paydQueryOptions({ queryKey: queryKeys.stats(), queryFn: () => paydRequest<OperationalStats>(["stats"]), polling: { tier: "B" } }));
  const resourceWallet = useQuery(paydQueryOptions({ queryKey: queryKeys.resources.wallets(), queryFn: () => paydRequest<ResourceWalletResponse>(["resources", "wallet"]), polling: { tier: "D" } }));
  const setParams = (next: Record<string, string>) => {
    const value = new URLSearchParams(searchParams);
    Object.entries(next).forEach(([key, item]) => item ? value.set(key, item) : value.delete(key));
    if (!("cursor" in next)) value.delete("cursor");
    router.replace(`${pathname}${value.size ? `?${value}` : ""}`);
  };
  const active = Object.values(filters).some(Boolean);
  const rows = wallets.data && "wallets" in wallets.data ? wallets.data.wallets : wallets.data && "addresses" in wallets.data ? wallets.data.addresses : [];

  return <main className="mx-auto max-w-7xl space-y-4 p-4 lg:p-6"><header><p className="font-mono text-xs uppercase tracking-[0.2em] text-ink-faint">Operations / Addresses</p><h1 className="mt-1 text-2xl font-semibold">Address pool</h1><p className="mt-1 text-sm text-ink-secondary">Backend cursor order. Confirmed and pending balances stay separate.</p></header>
    <PoolHealth stats={stats.data} config={config.data} />
    <TableFilters active={active} onClear={() => setParams({ state: "", has_balance: "", needs_resources: "", drift_detected: "", asset: "" })}>
      <label className="grid gap-1 text-xs text-ink-secondary">State<select value={filters.state} onChange={(event) => setParams({ state: event.currentTarget.value, has_balance: "", needs_resources: "" })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink"><option value="">Any</option><option value="free">free</option><option value="assigned">assigned</option><option value="cooling">cooling</option><option value="disabled">disabled</option></select></label>
      <label className="flex items-center gap-2 text-sm text-ink-secondary"><input type="checkbox" checked={Boolean(filters.has_balance)} onChange={(event) => setParams(event.currentTarget.checked ? { has_balance: "1", needs_resources: "", state: "", drift_detected: "", asset: "" } : { has_balance: "" })} />Has confirmed balance</label>
      <label className="flex items-center gap-2 text-sm text-ink-secondary"><input type="checkbox" checked={Boolean(filters.needs_resources)} onChange={(event) => setParams(event.currentTarget.checked ? { needs_resources: "1", has_balance: "", state: "", drift_detected: "", asset: "" } : { needs_resources: "" })} />Needs resources</label>
      <label className="flex items-center gap-2 text-sm text-ink-secondary"><input type="checkbox" checked={Boolean(filters.drift_detected)} onChange={(event) => setParams({ drift_detected: event.currentTarget.checked ? "1" : "", has_balance: "", needs_resources: "" })} />Drift detected</label>
      <label className="grid gap-1 text-xs text-ink-secondary">Asset<input value={filters.asset} onChange={(event) => setParams({ asset: event.currentTarget.value, has_balance: "", needs_resources: "" })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" /></label>
    </TableFilters>
    <DataTable columns={[{ id: "address", label: "Address" }, { id: "hd_index", label: "HD index" }, { id: "state", label: "State" }, { id: "balances", label: "Confirmed / pending by asset" }, { id: "resources", label: "Resources" }, { id: "drift", label: "Drift" }, { id: "assignment", label: "Assigned order" }, { id: "cooling", label: "Cooling until" }, { id: "checked", label: "Last checked" }]} rows={rows} rowKey={(wallet) => wallet.address} renderRow={(wallet) => <WalletRow wallet={wallet} resourceWallet={resourceWallet.data?.address} />} onRowClick={(wallet) => router.push(`/addresses/${encodeURIComponent(wallet.address)}`)} defaultSort="Backend wallet cursor order" caption="Address pool" loading={wallets.isLoading} emptyState={<EmptyState kind={active ? "search" : "search"} title={active ? "No addresses match these filters" : "No addresses in this cursor page"} description="Addresses appear after payd derives them into the pool." />} />
    <p className="text-xs text-ink-secondary">Filters are applied by payd. Has-balance and needs-resources use their dedicated server-side lists.</p>
    <ErrorNotice error={wallets.isError ? wallets.error : null} updatedAt={wallets.dataUpdatedAt} retry={() => void wallets.refetch()} />
    {specialFilter !== "needs-resources" ? <CursorPager nextCursor={wallets.data && "next_cursor" in wallets.data ? wallets.data.next_cursor : ""} hasResults={rows.length > 0} limit={limit} onNext={(next) => setParams({ cursor: next })} onStart={() => setParams({ cursor: "" })} onLimitChange={(next) => setParams({ limit: String(next) })} /> : null}
  </main>;
}

function Estimate({ value, kind }: Readonly<{ value?: string; kind: "burn" | "rent" }>) {
  if (value) return <Amount value={value} asset="TRX" />;
  if (kind === "rent") return <span className="text-ink-faint">provider unavailable</span>;
  return <Link href="/resources" className="text-severity-warning underline underline-offset-2">chain parameters not yet read</Link>;
}

function NeedsResources() {
  const wallets = useQuery(paydQueryOptions({ queryKey: queryKeys.wallets.needsResources(), queryFn: () => paydRequest<WalletResourceList>(["wallets", "needs-resources"]), polling: { tier: "B" } }));
  const rows = wallets.data?.addresses ?? [];
  return <main className="mx-auto max-w-7xl space-y-4 p-4 lg:p-6"><header><p className="font-mono text-xs uppercase tracking-[0.2em] text-ink-faint">Operations / Addresses</p><h1 className="mt-1 text-2xl font-semibold">Needs resources</h1><p className="mt-1 text-sm text-ink-secondary">An address that only received USDT can hold no TRX for bandwidth burn; delegated or rented energy does not cover bandwidth.</p></header>
    <DataTable columns={[{ id: "address", label: "Address" }, { id: "blocked", label: "Blocked by" }, { id: "energy", label: "Energy" }, { id: "bandwidth", label: "Bandwidth" }, { id: "burn", label: "Burn estimate" }, { id: "rent", label: "Rent estimate" }, { id: "fee", label: "Energy fee" }, { id: "action", label: "Action" }]} rows={rows} rowKey={(wallet) => wallet.address} renderRow={(wallet) => <><td className="px-3 py-2">{address(wallet.address)}</td><td className="px-3 py-2"><ResourceFlag wallet={wallet} /></td><td className="px-3 py-2 font-mono tabular-nums">available {wallet.energy.available} / limit {wallet.energy.limit} / required {wallet.energy.required} · {wallet.energy.sufficient ? "sufficient" : "short"}</td><td className="px-3 py-2 font-mono tabular-nums">available {wallet.bandwidth.available} / limit {wallet.bandwidth.limit} / required {wallet.bandwidth.required} · {wallet.bandwidth.sufficient ? "sufficient" : "short"}</td><td className="px-3 py-2"><Estimate value={wallet.estimated_burn_trx} kind="burn" /></td><td className="px-3 py-2"><Estimate value={typeof (wallet as WalletResource & { estimated_rent_trx?: unknown }).estimated_rent_trx === "string" ? (wallet as WalletResource & { estimated_rent_trx?: string }).estimated_rent_trx : undefined} kind="rent" /></td><td className="px-3 py-2 font-mono tabular-nums">{wallet.energy_fee_sun == null ? "—" : `${wallet.energy_fee_sun} SUN`}</td><td className="px-3 py-2" onClick={(event) => event.stopPropagation()}><AddressDelegate address={wallet.address} /></td></>} onRowClick={(wallet) => { window.location.assign(`/addresses/${encodeURIComponent(wallet.address)}`); }} defaultSort="Backend complete worklist order" caption="Addresses needing resources" loading={wallets.isLoading} emptyState={<EmptyState kind="worklist" title="No addresses need resources" description="Every address can currently move its funds according to payd." />} />
    <ErrorNotice error={wallets.isError ? wallets.error : null} updatedAt={wallets.dataUpdatedAt} retry={() => void wallets.refetch()} />
  </main>;
}

export function AddressesDashboard({ view }: Readonly<{ view?: "needs-resources" }>) {
  return view === "needs-resources" ? <NeedsResources /> : <PoolList />;
}
