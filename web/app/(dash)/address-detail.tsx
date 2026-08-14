"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

import { Amount } from "@/components/data/amount";
import { CursorPager } from "@/components/data/cursor-pager";
import { DataTable } from "@/components/data/data-table";
import { EmptyState } from "@/components/data/empty-state";
import { ErrorState } from "@/components/data/error-state";
import { AddressLink, EntityId, TxidLink } from "@/components/data/links";
import { StatusBadge } from "@/components/data/status-badge";
import { Timestamp } from "@/components/data/timestamp";
import { useTronscanBaseUrl } from "@/app/providers";
import { AddressClearDrift } from "@/app/(dash)/address-clear-drift";
import { AddressDelegate } from "@/app/(dash)/address-delegate";
import { AddressDisable } from "@/app/(dash)/address-disable";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { Payment, WalletDetail, WalletResource } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

const LIST_INTERVAL = 30_000;
const copyByCode: Record<string, string> = {
  not_found: "payd does not own this address.",
  unauthorized: "This dashboard session or its upstream scope is not authorised.",
  rate_limited: "Refresh has slowed because payd is rate limited.",
  upstream_unreachable: "payd could not be reached; showing the last available data when present.",
  upstream_timeout: "payd did not answer in time; showing the last available data when present.",
};

function Field({ label, children }: Readonly<{ label: string; children: React.ReactNode }>) {
  return <div><dt className="text-xs uppercase tracking-wide text-ink-faint">{label}</dt><dd className="mt-1 break-words">{children}</dd></div>;
}

function extra(wallet: WalletDetail, key: string): unknown {
  return (wallet as WalletDetail & Record<string, unknown>)[key];
}

function ResourceState({ label, state }: Readonly<{ label: string; state: WalletResource["energy"] }>) {
  return <section className="border border-border-subtle p-3"><h2 className="font-medium">{label}</h2><dl className="mt-3 grid gap-3 sm:grid-cols-4"><Field label="Available"><span className="font-mono tabular-nums">{state.available}</span></Field><Field label="Limit"><span className="font-mono tabular-nums">{state.limit}</span></Field><Field label="Required"><span className="font-mono tabular-nums">{state.required}</span></Field><Field label="Sufficient">{state.sufficient ? <span className="text-severity-success">Yes</span> : <span className="inline-flex items-center gap-1 text-severity-warning"><AlertTriangle aria-hidden="true" size={13} />No</span>}</Field></dl></section>;
}

function DriftValues({ balance }: Readonly<{ balance: WalletResource["balances"][number] }>) {
  return <div className="mt-2 grid gap-2 text-xs sm:grid-cols-2"><div><span className="text-ink-faint">Ledger base units</span><code className="mt-1 block select-all font-mono">{balance.confirmed_raw}</code></div><div><span className="text-ink-faint">Chain base units</span><code className="mt-1 block select-all font-mono">{balance.chain_raw ?? "not yet reconciled"}</code></div></div>;
}

function BalanceRows({ wallet }: Readonly<{ wallet: WalletDetail }>) {
  return <section><h2 className="text-lg font-semibold">Balances</h2><div className="mt-3 overflow-x-auto border border-border-subtle"><table className="w-full min-w-max text-left text-sm"><thead className="bg-raised text-xs uppercase tracking-wide text-ink-secondary"><tr><th className="px-3 py-2">Asset</th><th className="px-3 py-2">Confirmed</th><th className="px-3 py-2">Pending</th><th className="px-3 py-2">Withdrawable</th><th className="px-3 py-2">Drift</th></tr></thead><tbody>{wallet.balances.map((balance) => <tr key={balance.asset} className="border-t border-border-subtle align-top"><td className="px-3 py-2 font-mono">{balance.asset}</td><td className="px-3 py-2"><Amount value={balance.confirmed} asset={balance.asset} />{balance.usd ? <div className="mt-1"><Amount value={balance.usd} asset="USD" variant="usd-live" /></div> : <span className="mt-1 block text-ink-faint" title="The backend did not return a fresh USD value">—</span>}</td><td className="px-3 py-2"><Amount value={balance.pending} asset={balance.asset} /></td><td className="px-3 py-2">{wallet.can_withdraw[balance.asset] ? <span className="text-severity-success">Can withdraw</span> : <span className="inline-flex items-center gap-1 text-severity-warning"><AlertTriangle aria-hidden="true" size={13} />Cannot withdraw</span>}{!wallet.can_withdraw[balance.asset] && wallet.blocked_by.length ? <div className="mt-1 text-xs text-severity-warning">Blocked by {wallet.blocked_by.join(" and ")}</div> : null}</td><td className="px-3 py-2">{balance.drift_detected ? <><StatusBadge status="drift_detected" /><DriftValues balance={balance} />{balance.chain_raw ? <AddressClearDrift address={wallet.address} asset={balance.asset} chainRaw={balance.chain_raw} /> : null}</> : "No drift"}</td></tr>)}</tbody></table></div></section>;
}

function PaymentRow({ payment }: Readonly<{ payment: Payment }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  const address = (value: string) => <AddressLink address={value} href={`/addresses/${encodeURIComponent(value)}`} />;
  return <><td className="px-3 py-2"><TxidLink txid={payment.txid} tronscanBaseUrl={tronscanBaseUrl} /></td><td className="px-3 py-2 font-mono">{payment.direction}</td><td className="px-3 py-2"><Amount value={payment.amount} asset={payment.asset} /></td><td className="px-3 py-2">{address(payment.from_address)}</td><td className="px-3 py-2">{address(payment.to_address)}</td><td className="px-3 py-2"><StatusBadge status={payment.status} /></td><td className="px-3 py-2"><Timestamp seconds={payment.block_timestamp} /></td></>;
}

function ErrorNotice({ error, updatedAt, retry }: Readonly<{ error: unknown; updatedAt: number; retry: () => void }>) {
  if (!error) return null;
  const paydError = isPaydError(error) ? error : null;
  return <ErrorState error={{ code: paydError?.code ?? "upstream_unreachable", details: paydError?.details }} copyByCode={copyByCode} lastUpdatedAt={updatedAt || undefined} pollingIntervalMs={LIST_INTERVAL} onRetry={retry} />;
}

export function AddressDetail({ address }: Readonly<{ address: string }>) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const cursor = searchParams.get("cursor") ?? "";
  const limit = searchParams.get("limit") === "200" ? 200 : 50;
  const query = new URLSearchParams({ limit: String(limit) });
  if (cursor) query.set("cursor", cursor);
  const wallet = useQuery(paydQueryOptions({ queryKey: queryKeys.wallets.detail(`${address}:${query}`), queryFn: () => paydRequest<WalletDetail>(["wallets", address], {}, query), polling: { tier: "B" } }));
  const setParams = (next: Record<string, string>) => {
    const value = new URLSearchParams(searchParams);
    Object.entries(next).forEach(([key, item]) => item ? value.set(key, item) : value.delete(key));
    router.replace(`${pathname}${value.size ? `?${value}` : ""}`);
  };
  const data = wallet.data;

  return <main className="mx-auto max-w-7xl space-y-5 p-4 lg:p-6"><header><p className="font-mono text-xs uppercase tracking-[0.2em] text-ink-faint">Operations / Addresses</p><h1 className="mt-1 text-2xl font-semibold">Address detail</h1><code className="mt-2 block select-all break-all font-mono text-sm">{address}</code><div className="mt-3 flex flex-wrap gap-3 text-sm"><Link href={`/withdrawals/new?from_address=${encodeURIComponent(address)}`} className="text-severity-progress underline underline-offset-2">Withdraw from this address</Link><Link href={`/orders?address=${encodeURIComponent(address)}`} className="text-severity-progress underline underline-offset-2">Orders for this address</Link><Link href={`/payments?address=${encodeURIComponent(address)}`} className="text-severity-progress underline underline-offset-2">Payments for this address</Link></div></header>
    {data ? <><section className="border border-border-subtle bg-panel p-4"><dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4"><Field label="Address"><AddressLink address={data.address} href={`/addresses/${encodeURIComponent(data.address)}`} /></Field><Field label="HD index"><span className="font-mono tabular-nums">{data.hd_index}</span></Field><Field label="State"><StatusBadge status={data.state} /></Field><Field label="Resource state"><span className={data.needs_resources ? "text-severity-warning" : "text-severity-success"}>{data.needs_resources ? `Needs ${data.blocked_by.join(" and ") || "resources"}` : "Sufficient"}</span></Field><Field label="TRX available for bandwidth burn"><Amount value={data.trx_for_bandwidth_burn} asset="TRX" /></Field><Field label="Address drift">{data.drift_detected ? <StatusBadge status="drift_detected" /> : "No drift"}</Field><Field label="Last chain poll">{data.checked_at == null ? <span className="text-ink-secondary">Never polled</span> : <Timestamp seconds={data.checked_at} />}<p className="mt-1 text-xs text-ink-secondary">Low-balance addresses may be polled every six hours; this is expected, not an alarm.</p></Field><Field label="Assigned order">{typeof extra(data, "assigned_order_id") === "string" ? <Link href={`/orders/${encodeURIComponent(extra(data, "assigned_order_id") as string)}`} className="font-mono text-xs text-severity-progress underline underline-offset-2">{extra(data, "assigned_order_id") as string}</Link> : <span className="text-ink-faint">No assignment recorded</span>}</Field><Field label="Cooling until">{typeof extra(data, "cooling_until") === "number" ? <Timestamp seconds={extra(data, "cooling_until") as number} /> : <span className="text-ink-faint">Not cooling</span>}</Field></dl></section>
      <section className="flex flex-wrap items-center gap-3 border border-border-subtle bg-panel p-4"><AddressDisable wallet={data} /><AddressDelegate address={data.address} /></section>
      <BalanceRows wallet={data} />
      <section className="grid gap-3 lg:grid-cols-2"><ResourceState label="Energy" state={data.energy} /><ResourceState label="Bandwidth" state={data.bandwidth} /></section>
      <section className="space-y-3"><h2 className="text-lg font-semibold">Payment history</h2><DataTable columns={[{ id: "txid", label: "Transaction" }, { id: "direction", label: "Direction" }, { id: "amount", label: "Amount" }, { id: "from", label: "From" }, { id: "to", label: "To" }, { id: "status", label: "Status" }, { id: "time", label: "Block time" }]} rows={data.payments} rowKey={(payment) => `${payment.txid}:${payment.log_index}`} renderRow={(payment) => <PaymentRow payment={payment} />} defaultSort="Backend payment cursor order" caption="Address payment history" emptyState={<EmptyState kind="search" title="No payments for this address" description="Inbound and outbound payments appear here when payd records them." />} /><CursorPager nextCursor={data.next_cursor} hasResults={data.payments.length > 0} limit={limit} onNext={(next) => setParams({ cursor: next })} onStart={() => setParams({ cursor: "" })} onLimitChange={(next) => setParams({ limit: String(next) })} /></section>
    </> : null}
    <ErrorNotice error={wallet.isError ? wallet.error : null} updatedAt={wallet.dataUpdatedAt} retry={() => void wallet.refetch()} />
  </main>;
}
