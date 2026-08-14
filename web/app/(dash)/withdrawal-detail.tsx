"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";
import Link from "next/link";
import { useEffect, useState, type ReactNode } from "react";

import { useTronscanBaseUrl } from "@/app/providers";
import { Amount } from "@/components/data/amount";
import { AddressLink, EntityId, TxidLink } from "@/components/data/links";
import { StatusBadge } from "@/components/data/status-badge";
import { Timestamp } from "@/components/data/timestamp";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { Withdrawal } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

const DETAIL_INTERVAL = 10_000;
const terminal = new Set(["confirmed", "rejected", "failed", "needs_operator"]);
const progress = new Set(["awaiting_resources", "awaiting_energy", "signing", "broadcast"]);
const copyByCode: Record<string, string> = {
  not_found: "payd could not find this withdrawal.",
  unauthorized: "This dashboard session or its upstream scope is not authorised.",
  rate_limited: "Refresh has slowed because payd is rate limited.",
  upstream_unreachable: "payd could not be reached; showing the last available data when present.",
  upstream_timeout: "payd did not answer in time; showing the last available data when present.",
};

function Field({ label, children }: Readonly<{ label: string; children: ReactNode }>) {
  return <div><dt className="text-xs uppercase tracking-wide text-ink-faint">{label}</dt><dd className="mt-1 break-words">{children}</dd></div>;
}

function Elapsed({ status, updatedAt }: Readonly<{ status: string; updatedAt: number }>) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => { const timer = window.setInterval(() => setNow(Date.now()), 1_000); return () => window.clearInterval(timer); }, []);
  if (!progress.has(status)) return null;
  return <div className="mt-1 text-xs text-ink-secondary">In this state <Timestamp seconds={Math.floor(now / 1000) - updatedAt} variant="duration" /></div>;
}

function resolvedBy(value: string) {
  const labels: Record<string, string> = {
    chain_absence: "The transaction was confirmed absent from the chain.",
    resource_acquisition: "It failed while sourcing energy or bandwidth, before broadcast.",
    operator: "A human recorded the outcome.",
  };
  return value ? <>{value}{labels[value] ? <span className="ml-2 text-sm text-ink-secondary">{labels[value]}</span> : null}</> : <span className="text-ink-faint">—</span>;
}

function ReadError({ error, updatedAt, reload }: Readonly<{ error: unknown; updatedAt: number; reload: () => void }>) {
  if (!error) return null;
  const paydError = isPaydError(error) ? error : null;
  const stale = updatedAt > 0 && Date.now() - updatedAt > DETAIL_INTERVAL * 3;
  return <div className="border border-severity-warning bg-[var(--severity-warning-bg)] p-3" role="alert">
    {stale ? <p className="mb-2 text-sm text-severity-warning">Showing stale data; last updated {Math.floor((Date.now() - updatedAt) / 60_000)}m ago.</p> : null}
    <p className="font-medium">{copyByCode[paydError?.code ?? "upstream_unreachable"] ?? "An unrecognised error was returned."}</p>
    <p className="mt-1 text-sm text-ink-secondary">Error code: <code className="select-all font-mono text-ink">{paydError?.code ?? "upstream_unreachable"}</code></p>
    {paydError?.details ? <pre className="mt-2 overflow-auto border-t border-border-subtle pt-2 text-xs text-ink-secondary">{JSON.stringify(paydError.details, null, 2)}</pre> : null}
    <button type="button" className="mt-3 border border-border-strong px-3 py-1.5 text-sm hover:bg-raised" onClick={reload}>Reload</button>
  </div>;
}

function AmbiguousOutcome({ withdrawal }: Readonly<{ withdrawal: Withdrawal }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  return <section className="border-2 border-severity-critical bg-panel p-4" aria-label="Ambiguous outcome"><div className="flex items-center gap-2"><StatusBadge status="needs_operator" /><h2 className="font-semibold">Outcome requires an operator decision</h2></div><ol className="mt-3 list-decimal space-y-2 pl-5 text-sm"><li>The funds may or may not have moved.</li><li>{withdrawal.txid ? <>Check this transaction on Tronscan: <TxidLink txid={withdrawal.txid} tronscanBaseUrl={tronscanBaseUrl} />.</> : "No transaction ID was recorded; the chain cannot be checked from this record."}</li><li>Last lookup error: <code className="select-all font-mono">{withdrawal.last_lookup_error || "—"}</code>.</li><li>The service will attempt nothing further.</li><li>Recording an outcome is a decision record, not an action.</li></ol></section>;
}

function Resources({ withdrawal }: Readonly<{ withdrawal: Withdrawal }>) {
  const expensive = withdrawal.energy_source === "burned";
  return <section className="border border-border-subtle bg-panel p-4"><h2 className="font-semibold">Resource and fee breakdown</h2><dl className="mt-3 grid gap-4 sm:grid-cols-2 xl:grid-cols-4"><Field label="Energy source">{expensive ? <span className="inline-flex items-center gap-1 text-severity-warning"><AlertTriangle aria-hidden="true" size={14} />burned — expensive path</span> : <span className="font-mono">{withdrawal.energy_source || "—"}</span>}</Field><Field label="Energy cost"><Amount value={withdrawal.energy_cost_trx} asset="TRX" /></Field><Field label="Energy used"><span className="font-mono tabular-nums">{withdrawal.energy_used}</span></Field><Field label="Bandwidth source"><span className="font-mono">{withdrawal.bandwidth_source || "—"}</span></Field><Field label="Bandwidth cost"><Amount value={withdrawal.bandwidth_cost_trx} asset="TRX" /></Field><Field label="Chain fee"><Amount value={withdrawal.network_fee_trx} asset="TRX" /></Field><Field label="Fee raw (SUN)"><code className="select-all font-mono">{withdrawal.fee_raw}</code></Field><Field label="Resource fee"><Amount value={withdrawal.resource_fee_trx} asset="TRX" /></Field><Field label="Total cost"><Amount value={withdrawal.total_cost_trx} asset="TRX" /></Field></dl></section>;
}

export function WithdrawalDetail({ id }: Readonly<{ id: string }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  const [live, setLive] = useState(false);
  const withdrawalQuery = useQuery(paydQueryOptions({ queryKey: queryKeys.withdrawals.detail(id), queryFn: () => paydRequest<Withdrawal>(["withdrawals", id]), polling: { tier: "A", entity: "detail", withdrawal: true, isLive: () => live } }));
  const withdrawal = withdrawalQuery.data;
  useEffect(() => setLive(Boolean(withdrawal && !terminal.has(withdrawal.status))), [withdrawal]);
  const address = (value: string) => <AddressLink address={value} href={`/addresses/${encodeURIComponent(value)}`} />;
  const resourceQuery = encodeURIComponent(id);

  if (!withdrawal && withdrawalQuery.isLoading) return <main className="mx-auto max-w-7xl p-4 lg:p-6"><p className="text-ink-faint">Loading withdrawal…</p></main>;
  if (!withdrawal) return <main className="mx-auto max-w-7xl space-y-4 p-4 lg:p-6"><ReadError error={withdrawalQuery.error} updatedAt={withdrawalQuery.dataUpdatedAt} reload={() => void withdrawalQuery.refetch()} /></main>;

  return <main className="mx-auto max-w-7xl space-y-4 p-4 lg:p-6"><header><p className="font-mono text-xs uppercase tracking-[0.2em] text-ink-faint">Operations / Withdrawals / Detail</p><div className="mt-1 flex flex-wrap items-center gap-3"><h1 className="text-2xl font-semibold">Withdrawal <EntityId value={withdrawal.id} full /></h1><StatusBadge status={withdrawal.status} /></div></header>
    {withdrawal.status === "needs_operator" ? <AmbiguousOutcome withdrawal={withdrawal} /> : null}
    <section className="border border-border-subtle bg-panel p-4"><h2 className="font-semibold">Withdrawal record</h2><dl className="mt-3 grid gap-4 sm:grid-cols-2 xl:grid-cols-3"><Field label="Full withdrawal ID"><EntityId value={withdrawal.id} full /></Field><Field label="Status"><StatusBadge status={withdrawal.status} /><Elapsed status={withdrawal.status} updatedAt={withdrawal.status_updated_at} /></Field><Field label="Status entered"><Timestamp seconds={withdrawal.status_updated_at} /></Field><Field label="Asset">{withdrawal.asset}</Field><Field label="Amount"><Amount value={withdrawal.amount} asset={withdrawal.asset} /></Field><Field label="USD snapshot"><Amount value={withdrawal.amount_usd} asset="USD" variant="usd-snapshot" /></Field><Field label="Source address">{address(withdrawal.from_address)}</Field><Field label="Destination address">{address(withdrawal.to_address)}</Field><Field label="Transaction ID">{withdrawal.txid ? <TxidLink txid={withdrawal.txid} tronscanBaseUrl={tronscanBaseUrl} /> : <span className="text-ink-faint">—</span>}</Field><Field label="Created"><Timestamp seconds={withdrawal.created_at} /></Field><Field label="Broadcast"><Timestamp seconds={withdrawal.broadcast_at} /></Field><Field label="Confirmed"><Timestamp seconds={withdrawal.confirmed_at} /></Field><Field label="Failure reason"><code className="select-all font-mono text-sm">{withdrawal.failure_reason || "—"}</code></Field><Field label="Resolved by"><span className="font-mono">{resolvedBy(withdrawal.resolved_by)}</span></Field><Field label="Last lookup error"><code className="select-all font-mono text-sm">{withdrawal.last_lookup_error || "—"}</code></Field></dl><div className="mt-4"><p className="text-xs uppercase tracking-wide text-ink-faint">Raw broadcast response</p><pre className="mt-1 max-h-80 overflow-auto border border-border-subtle bg-inset p-3 text-xs text-ink-secondary"><code className="select-all font-mono">{withdrawal.broadcast_response || "—"}</code></pre></div></section>
    <Resources withdrawal={withdrawal} />
    <section className="border border-border-subtle bg-panel p-4"><h2 className="font-semibold">Related records</h2><div className="mt-3 flex flex-wrap gap-x-5 gap-y-2 text-sm"><Link href={`/addresses/${encodeURIComponent(withdrawal.from_address)}`} className="text-severity-progress underline underline-offset-2">Source address</Link>{withdrawal.txid ? <Link href={`/payments?txid=${encodeURIComponent(withdrawal.txid)}`} className="text-severity-progress underline underline-offset-2">Outbound payment ledger row</Link> : <span className="text-ink-secondary">Outbound payment ledger row is available once a transaction ID is recorded.</span>}<Link href={`/resources?withdrawal_id=${resourceQuery}`} className="text-severity-progress underline underline-offset-2">Energy purchase record</Link><Link href={`/resources?tab=grants&withdrawal_id=${resourceQuery}`} className="text-severity-progress underline underline-offset-2">Resource grants</Link></div></section>
    <ReadError error={withdrawalQuery.isError ? withdrawalQuery.error : null} updatedAt={withdrawalQuery.dataUpdatedAt} reload={() => void withdrawalQuery.refetch()} />
  </main>;
}
