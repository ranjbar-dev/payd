"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, ExternalLink, RefreshCw, Wallet } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

import { Amount } from "@/components/data/amount";
import { CursorPager } from "@/components/data/cursor-pager";
import { EmptyState } from "@/components/data/empty-state";
import { ErrorState } from "@/components/data/error-state";
import { AddressLink, TxidLink } from "@/components/data/links";
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

function Field({ label, children, numeric = false }: Readonly<{ label: string; children: React.ReactNode; numeric?: boolean }>) {
  return <div><dt className="text-[11px] uppercase tracking-wide text-ink-faint">{label}</dt><dd className={`mt-1 break-words ${numeric ? "text-right font-mono tabular-nums" : ""}`}>{children}</dd></div>;
}

function extra(wallet: WalletDetail, key: string): unknown {
  return (wallet as WalletDetail & Record<string, unknown>)[key];
}

function ResourceState({ label, state }: Readonly<{ label: string; state: WalletResource["energy"] }>) {
  return <section className="card"><h2 className="card-title">{label}</h2><dl className="mt-3 grid gap-3 sm:grid-cols-4"><Field label="Available" numeric>{state.available}</Field><Field label="Limit" numeric>{state.limit}</Field><Field label="Required" numeric>{state.required}</Field><Field label="Sufficient">{state.sufficient ? <span className="text-severity-success">Yes</span> : <span className="inline-flex items-center gap-1 text-severity-warning"><AlertTriangle aria-hidden="true" size={13} strokeWidth={1.75} />No</span>}</Field></dl></section>;
}

function DriftValues({ balance }: Readonly<{ balance: WalletResource["balances"][number] }>) {
  return <dl className="mt-2 grid gap-2 text-xs sm:grid-cols-2"><Field label="Ledger base units" numeric>{balance.confirmed_raw}</Field><Field label="Chain base units" numeric>{balance.chain_raw ?? "not yet reconciled"}</Field></dl>;
}

function BalanceRows({ wallet }: Readonly<{ wallet: WalletDetail }>) {
  return <section className="card"><h2 className="card-title">Balances</h2><p className="mt-1 text-sm text-ink-secondary">Confirmed and pending balances are reported separately for each asset.</p><div className="mt-3 overflow-x-auto"><table className="w-full min-w-max text-left text-[13px]"><caption className="sr-only">Address balances by asset</caption><thead><tr><th className="th">Asset</th><th className="th text-right">Confirmed</th><th className="th text-right">Pending</th><th className="th">Withdrawable</th><th className="th">Drift</th></tr></thead><tbody>{wallet.balances.map((balance) => <tr key={balance.asset} className="row-hover align-top"><td className="td font-mono tabular-nums">{balance.asset}</td><td className="td text-right"><Amount value={balance.confirmed} asset={balance.asset} />{balance.usd ? <div className="mt-1"><Amount value={balance.usd} asset="USD" variant="usd-live" /></div> : <span className="mt-1 block text-[11px] text-ink-faint" title="The backend did not return a fresh USD value">—</span>}</td><td className="td text-right"><Amount value={balance.pending} asset={balance.asset} /></td><td className="td">{wallet.can_withdraw[balance.asset] ? <span className="text-severity-success">Can withdraw</span> : <span className="inline-flex items-center gap-1 text-severity-warning"><AlertTriangle aria-hidden="true" size={13} strokeWidth={1.75} />Cannot withdraw</span>}{!wallet.can_withdraw[balance.asset] && wallet.blocked_by.length ? <div className="mt-1 text-[11px] text-severity-warning">Blocked by {wallet.blocked_by.join(" and ")}</div> : null}</td><td className="td">{balance.drift_detected ? <><StatusBadge status="drift_detected" /><DriftValues balance={balance} />{balance.chain_raw ? <AddressClearDrift address={wallet.address} asset={balance.asset} chainRaw={balance.chain_raw} /> : null}</> : <span className="text-ink-faint">No drift</span>}</td></tr>)}</tbody></table></div></section>;
}

function PaymentRow({ payment }: Readonly<{ payment: Payment }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  const address = (value: string) => <AddressLink address={value} href={`/addresses/${encodeURIComponent(value)}`} />;
  return <><td className="td"><TxidLink txid={payment.txid} tronscanBaseUrl={tronscanBaseUrl} /></td><td className="td font-mono tabular-nums">{payment.direction}</td><td className="td text-right"><Amount value={payment.amount} asset={payment.asset} /></td><td className="td">{address(payment.from_address)}</td><td className="td">{address(payment.to_address)}</td><td className="td"><StatusBadge status={payment.status} /></td><td className="td text-right font-mono tabular-nums"><Timestamp seconds={payment.block_timestamp} /></td></>;
}

function ErrorNotice({ error, updatedAt, retry }: Readonly<{ error: unknown; updatedAt: number; retry: () => void }>) {
  if (!error) return null;
  const paydError = isPaydError(error) ? error : null;
  return <ErrorState error={{ code: paydError?.code ?? "upstream_unreachable", details: paydError?.details }} copyByCode={copyByCode} lastUpdatedAt={updatedAt || undefined} pollingIntervalMs={LIST_INTERVAL} onRetry={retry} />;
}

function LoadingState() {
  return <div className="space-y-4" aria-label="Loading address detail"><section className="card animate-pulse"><div className="h-3 w-28 bg-border-subtle" /><div className="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">{Array.from({ length: 8 }, (_, index) => <div key={index}><div className="h-3 w-20 bg-border-subtle" /><div className="mt-2 h-4 w-32 bg-border-subtle" /></div>)}</div></section><section className="card animate-pulse"><div className="h-3 w-24 bg-border-subtle" /><div className="mt-4 h-32 w-full bg-border-subtle" /></section></div>;
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

  return <main className="page"><header><p className="page-kicker"><Wallet aria-hidden="true" size={14} strokeWidth={1.75} />Operations / Addresses</p><div className="mt-1 flex flex-wrap items-start justify-between gap-3"><div><h1 className="page-title">Address detail</h1><div className="mt-2"><AddressLink address={address} href={`/addresses/${encodeURIComponent(address)}`} className="text-sm" /></div></div><button type="button" className="btn btn-secondary" onClick={() => void wallet.refetch()}><RefreshCw aria-hidden="true" size={14} strokeWidth={1.75} />Refresh</button></div><nav className="mt-3 flex flex-wrap gap-2" aria-label="Address actions"><Link href={`/withdrawals/new?from_address=${encodeURIComponent(address)}`} className="btn btn-primary"><ExternalLink aria-hidden="true" size={14} strokeWidth={1.75} />Withdraw from this address</Link><Link href={`/orders?address=${encodeURIComponent(address)}`} className="btn btn-secondary"><ExternalLink aria-hidden="true" size={14} strokeWidth={1.75} />Orders</Link><Link href={`/payments?address=${encodeURIComponent(address)}`} className="btn btn-secondary"><ExternalLink aria-hidden="true" size={14} strokeWidth={1.75} />Payments</Link></nav></header>
    {wallet.isLoading && !data ? <LoadingState /> : null}
    {data ? <><section className="card"><h2 className="card-title">Address status</h2><dl className="mt-3 grid gap-4 sm:grid-cols-2 lg:grid-cols-4"><Field label="Address"><AddressLink address={data.address} href={`/addresses/${encodeURIComponent(data.address)}`} /></Field><Field label="HD index" numeric>{data.hd_index}</Field><Field label="State"><StatusBadge status={data.state} /></Field><Field label="Resource state">{data.needs_resources ? <span className="text-severity-warning">Needs {data.blocked_by.join(" and ") || "resources"}</span> : <span className="text-severity-success">Sufficient</span>}</Field><Field label="TRX available for bandwidth burn" numeric><Amount value={data.trx_for_bandwidth_burn} asset="TRX" /></Field><Field label="Address drift">{data.drift_detected ? <StatusBadge status="drift_detected" /> : <span className="text-ink-faint">No drift</span>}</Field><Field label="Last chain poll">{data.checked_at == null ? <span className="text-ink-secondary">Never polled</span> : <Timestamp seconds={data.checked_at} />}<p className="mt-1 text-[11px] text-ink-secondary">Low-balance addresses may be polled every six hours; this is expected, not an alarm.</p></Field><Field label="Assigned order">{typeof extra(data, "assigned_order_id") === "string" ? <Link href={`/orders/${encodeURIComponent(extra(data, "assigned_order_id") as string)}`} className="cursor-pointer font-mono text-xs text-severity-progress underline underline-offset-2 transition-colors hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">{extra(data, "assigned_order_id") as string}</Link> : <span className="text-ink-faint">No assignment recorded</span>}</Field><Field label="Cooling until" numeric>{typeof extra(data, "cooling_until") === "number" ? <Timestamp seconds={extra(data, "cooling_until") as number} /> : <span className="text-ink-faint">Not cooling</span>}</Field></dl></section>
      <div className="grid gap-3 lg:grid-cols-2"><AddressDisable wallet={data} /><section className="card"><h2 className="card-title">Resource action</h2><div className="mt-3"><AddressDelegate address={data.address} /></div></section></div>
      <BalanceRows wallet={data} />
      <section className="grid gap-3 lg:grid-cols-2"><ResourceState label="Energy" state={data.energy} /><ResourceState label="Bandwidth" state={data.bandwidth} /></section>
      <section className="card"><h2 className="card-title">Payment history</h2><p className="mt-1 text-sm text-ink-secondary">Inbound and outbound payments recorded by payd.</p><div className="mt-3 overflow-x-auto"><table className="w-full min-w-max text-left text-[13px]" data-default-sort="Backend payment cursor order"><caption className="sr-only">Address payment history. Default sort: Backend payment cursor order.</caption><thead><tr><th className="th">Transaction</th><th className="th">Direction</th><th className="th text-right">Amount</th><th className="th">From</th><th className="th">To</th><th className="th">Status</th><th className="th text-right">Block time</th></tr></thead><tbody>{data.payments.length ? data.payments.map((payment) => <tr key={`${payment.txid}:${payment.log_index}`} className="row-hover"><PaymentRow payment={payment} /></tr>) : <tr><td colSpan={7} className="td p-3"><EmptyState kind="search" title="No payments for this address" description="Inbound and outbound payments appear here when payd records them." icon={<Wallet aria-hidden="true" size={20} strokeWidth={1.75} />} /></td></tr>}</tbody></table></div><CursorPager nextCursor={data.next_cursor} hasResults={data.payments.length > 0} limit={limit} onNext={(next) => setParams({ cursor: next })} onStart={() => setParams({ cursor: "" })} onLimitChange={(next) => setParams({ limit: String(next) })} /></section>
    </> : null}
    <ErrorNotice error={wallet.isError ? wallet.error : null} updatedAt={wallet.dataUpdatedAt} retry={() => void wallet.refetch()} />
  </main>;
}
