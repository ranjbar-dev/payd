"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertOctagon, AlertTriangle, Zap } from "lucide-react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useEffect } from "react";

import { Amount } from "@/components/data/amount";
import { ErrorState } from "@/components/data/error-state";
import { Timestamp } from "@/components/data/timestamp";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { ChainParameters, EnergyStatus } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

import { ResourceGrants } from "./resource-grants";
import { ResourcePurchases } from "./resource-purchases";

type WalletResources = { available: number; limit: number };
type Delegation = { count: number; stake_trx: string };
type ResourceWallet = { address: string; trx_balance: string; energy: WalletResources; bandwidth: WalletResources; outstanding_delegations: Record<string, Delegation> };
type ResourceConfig = { resources: { bandwidth_topup_trx: string } };
type FeesReport = { energy_by_source_trx: Record<string, string>; rental_spend_trx: string };

function ReadProblem({ error, reload }: Readonly<{ error: unknown; reload: () => void }>) {
  if (!error) return null;
  const paydError = isPaydError(error) ? error : null;
  const code = paydError?.code ?? "upstream_unreachable";
  const copy: Record<string, string> = { unauthorized: "This dashboard session or its upstream scope is not authorised.", rate_limited: "Refresh has slowed because payd is rate limited.", upstream_unreachable: "payd could not be reached; any last available data remains visible.", upstream_timeout: "payd did not answer in time; any last available data remains visible." };
  return <div className="mt-3"><ErrorState error={paydError ?? { code }} copyByCode={copy} onRetry={reload} /></div>;
}

function decimal(value: string): boolean {
  return /^\d+(?:\.\d+)?$/.test(value);
}

function ProviderCard({ status }: Readonly<{ status?: EnergyStatus }>) {
  if (!status) return <section className="card" aria-busy="true"><h2 className="card-title">Energy provider</h2><div className="mt-3 grid animate-pulse gap-3 sm:grid-cols-2"><div className="h-9 bg-raised" /><div className="h-9 bg-raised" /><div className="h-9 bg-raised" /><div className="h-9 bg-raised" /></div></section>;
  const balanceKnown = decimal(status.balance_trx);
  const failuresCritical = status.consecutive_failures >= 5;
  const concern = failuresCritical || status.balance_low || !balanceKnown;
  const Icon = failuresCritical ? AlertOctagon : AlertTriangle;
  const severity = failuresCritical ? "critical" : "warning";
  const balanceState = !balanceKnown ? "Unknown — not reported as healthy" : status.balance_low ? "Low (backend verdict)" : "Not low (backend verdict)";
  return <section className={`card ${concern ? `border-severity-${severity}` : ""}`}><div className="flex items-center justify-between gap-3"><h2 className="card-title">Energy provider</h2>{concern ? <span className={`inline-flex items-center gap-1 text-sm text-severity-${severity}`}><Icon aria-hidden="true" size={16} strokeWidth={1.75} />{failuresCritical ? "Tier 1 unavailable" : "Attention"}</span> : null}</div><dl className="mt-3 grid gap-3 text-[13px] sm:grid-cols-2"><div><dt className="text-ink-faint text-[11px] uppercase">Provider</dt><dd className="mt-1 font-mono">{status.provider}</dd></div><div><dt className="text-ink-faint text-[11px] uppercase">Balance / warning threshold</dt><dd className="mt-1 text-right font-mono tabular-nums"><Amount value={status.balance_trx} asset="TRX" /> <span className="text-ink-faint">/</span> <Amount value={status.balance_warn_trx} asset="TRX" /><p className={concern ? `mt-1 text-left text-xs text-severity-${severity}` : "mt-1 text-left text-xs text-ink-secondary"}>{balanceState}</p></dd></div><div><dt className="text-ink-faint text-[11px] uppercase">Last checked</dt><dd className="mt-1 text-right font-mono tabular-nums"><Timestamp seconds={status.last_checked_at} /><p className="mt-1 text-left text-xs font-sans text-ink-secondary">Provider balance checks run every 15 minutes.</p></dd></div><div><dt className="text-ink-faint text-[11px] uppercase">Consecutive failures</dt><dd className="mt-1 text-right font-mono tabular-nums">{status.consecutive_failures}{failuresCritical ? <p className="mt-1 text-left text-xs text-severity-critical">Tier 1 is skipped entirely for 10 minutes.</p> : null}</dd></div><div className="sm:col-span-2"><dt className="text-ink-faint text-[11px] uppercase">Last provider error</dt><dd className="mt-1"><code className="select-all font-mono text-xs">{status.last_error || "—"}</code><p className="mt-1 text-xs text-ink-secondary">Recorded at the last provider check: <Timestamp seconds={status.last_checked_at} />.</p></dd></div></dl><div className="mt-4 border-t border-border-subtle pt-3"><p className="text-xs text-ink-secondary">Provider calls do not count against the TronGrid quota.</p><p className="mt-2 text-[11px] uppercase tracking-wide text-ink-faint">Purchase outcomes</p><div className="mt-1 flex flex-wrap gap-x-4 gap-y-1 font-mono text-xs tabular-nums">{Object.entries(status.purchases).map(([outcome, count]) => <span key={outcome}>{outcome}: {count}</span>)}</div></div></section>;
}

function ChainParametersCard({ params, error, reload }: Readonly<{ params?: ChainParameters; error: unknown; reload: () => void }>) {
  const unavailable = isPaydError(error) && error.code === "chain_params_unavailable";
  if (unavailable) return <section className="card border-severity-critical" role="alert"><div className="flex items-center gap-2 text-severity-critical"><AlertOctagon aria-hidden="true" size={18} strokeWidth={1.75} /><h2 className="card-title text-severity-critical">Chain parameters unavailable</h2></div><p className="mt-2 text-sm">The service holds withdrawals rather than assuming a price.</p><p className="mt-2 text-xs text-ink-secondary">Error code: <code className="select-all font-mono text-ink">chain_params_unavailable</code></p></section>;
  if (!params) return <section className="card" aria-busy="true"><h2 className="card-title">Chain parameters</h2><div className="mt-3 grid animate-pulse gap-3 sm:grid-cols-2"><div className="h-9 bg-raised" /><div className="h-9 bg-raised" /></div><ReadProblem error={error} reload={reload} /></section>;
  const verdict = params.burn_exceeds_ceiling === undefined ? "Unknown — backend could not compare the figures" : params.burn_exceeds_ceiling ? "Exceeds ceiling (backend verdict)" : "Within ceiling (backend verdict)";
  return <section className={`card ${params.burn_exceeds_ceiling ? "border-severity-warning" : ""}`}><div className="flex flex-wrap items-center justify-between gap-3"><h2 className="card-title">Chain parameters</h2>{params.burn_exceeds_ceiling ? <span className="inline-flex items-center gap-1 text-sm text-severity-warning"><AlertTriangle aria-hidden="true" size={16} strokeWidth={1.75} />Burn ceiling exceeded</span> : params.stale ? <span className="inline-flex items-center gap-1 text-sm text-severity-warning"><AlertTriangle aria-hidden="true" size={16} strokeWidth={1.75} />Backend marks this reading stale</span> : null}</div><dl className="mt-3 grid gap-3 text-[13px] sm:grid-cols-2"><div><dt className="text-ink-faint text-[11px] uppercase">Energy fee</dt><dd className="mt-1 text-right font-mono tabular-nums">{params.getEnergyFee} SUN / energy</dd><p className="mt-1 text-xs text-ink-secondary">For a worst-case 131,000-energy transfer, backend-calculated burn: <Amount value={params.worst_case_burn_trx} asset="TRX" />.</p></div><div><dt className="text-ink-faint text-[11px] uppercase">Transaction fee</dt><dd className="mt-1 text-right font-mono tabular-nums">{params.getTransactionFee} SUN / bandwidth</dd></div><div><dt className="text-ink-faint text-[11px] uppercase">Worst-case burn / ceiling</dt><dd className="mt-1 text-right font-mono tabular-nums"><Amount value={params.worst_case_burn_trx} asset="TRX" /> <span className="text-ink-faint">/</span> <Amount value={params.max_burn_trx} asset="TRX" /><p className={`mt-1 text-left text-xs font-sans ${params.burn_exceeds_ceiling ? "text-severity-warning" : "text-ink-secondary"}`}>{verdict}</p></dd></div><div><dt className="text-ink-faint text-[11px] uppercase">Read at</dt><dd className="mt-1 text-right font-mono tabular-nums"><Timestamp seconds={params.fetched_at} /><p className="mt-1 text-left text-xs font-sans text-ink-secondary">Refreshed at startup and every 6 hours.</p></dd></div></dl><ReadProblem error={error} reload={reload} /></section>;
}

function ResourceLine({ name, value, delegation }: Readonly<{ name: string; value: WalletResources; delegation?: Delegation }>) {
  return <div><dt className="text-ink-faint text-[11px] uppercase">{name}</dt><dd className="mt-1 text-right font-mono tabular-nums">available {value.available} / limit {value.limit}</dd><p className="mt-1 text-xs text-ink-secondary">Non-failed self-delegations: {delegation?.count ?? 0} grants · {delegation ? <Amount value={delegation.stake_trx} asset="TRX" /> : "—"}</p></div>;
}

function ResourceWalletCard({ wallet, config, error, reload }: Readonly<{ wallet?: ResourceWallet; config?: ResourceConfig; error: unknown; reload: () => void }>) {
  if (!wallet) return <section className="card" aria-busy="true"><h2 className="card-title">Resource wallet</h2><div className="mt-3 grid animate-pulse gap-3 sm:grid-cols-2"><div className="h-9 bg-raised" /><div className="h-9 bg-raised" /><div className="h-9 bg-raised" /><div className="h-9 bg-raised" /></div><ReadProblem error={error} reload={reload} /></section>;
  return <section className="card"><h2 className="card-title">Resource wallet</h2><p className="mt-1 text-[13px] text-ink-secondary">Withdrawal path dependency: an empty wallet blocks tier-2 delegation and bandwidth top-ups.</p><dl className="mt-3 grid gap-3 text-[13px] sm:grid-cols-2"><div><dt className="text-ink-faint text-[11px] uppercase">Permanent disabled pool entry</dt><dd className="mt-1"><Link href={`/addresses/${encodeURIComponent(wallet.address)}`} className="cursor-pointer font-mono text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">{wallet.address}</Link></dd></div><div><dt className="text-ink-faint text-[11px] uppercase">Confirmed TRX / bandwidth reserve</dt><dd className="mt-1 text-right font-mono tabular-nums"><Amount value={wallet.trx_balance} asset="TRX" /> <span className="text-ink-faint">/</span> {config ? <Amount value={config.resources.bandwidth_topup_trx} asset="TRX" /> : <span className="text-ink-faint">loading reserve</span>}<p className="mt-1 text-left text-xs font-sans text-ink-secondary">Both figures are shown for operator review; payd supplies no comparison verdict.</p></dd></div><ResourceLine name="Energy" value={wallet.energy} delegation={wallet.outstanding_delegations.ENERGY} /><ResourceLine name="Bandwidth" value={wallet.bandwidth} delegation={wallet.outstanding_delegations.BANDWIDTH} /></dl><p className="mt-4 text-xs text-ink-secondary">Staking and unstaking are manual chain operations the service never performs. Unstaking takes 14 days.</p><ReadProblem error={error} reload={reload} /></section>;
}

function FeeCard({ fees, error, reload }: Readonly<{ fees?: FeesReport; error: unknown; reload: () => void }>) {
  const burned = fees?.energy_by_source_trx.burned;
  const rented = fees?.energy_by_source_trx.rented;
  const value = (amount?: string) => amount ? <Amount value={amount} asset="TRX" /> : <span className="text-ink-faint">—</span>;
  return <section className="card"><div className="flex flex-wrap items-start justify-between gap-3"><div><h2 className="card-title">Burn versus rent</h2><p className="mt-1 text-[13px] text-ink-secondary">Recent seven-day window (UTC). Rising burn cost is what a silently failing provider looks like.</p></div><Link href="/reports/fees" className="inline-flex cursor-pointer items-center text-[13px] text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">Open full fee report</Link></div>{fees ? <dl className="mt-3 grid gap-3 text-[13px] sm:grid-cols-3"><div><dt className="text-ink-faint text-[11px] uppercase">Energy burned</dt><dd className="mt-1 text-right font-mono tabular-nums">{value(burned)}</dd></div><div><dt className="text-ink-faint text-[11px] uppercase">Energy rented</dt><dd className="mt-1 text-right font-mono tabular-nums">{value(rented)}</dd></div><div><dt className="text-ink-faint text-[11px] uppercase">Rental spend</dt><dd className="mt-1 text-right font-mono tabular-nums"><Amount value={fees.rental_spend_trx} asset="TRX" /></dd></div></dl> : <div className="mt-3 h-9 animate-pulse bg-raised" aria-busy="true" />}<ReadProblem error={error} reload={reload} /></section>;
}

export function ResourcesDashboard() {
  const searchParams = useSearchParams();
  useEffect(() => {
    if (searchParams.get("tab") === "grants") document.getElementById("grants")?.scrollIntoView();
  }, [searchParams]);
  const to = Math.floor(Date.now() / 1000);
  const from = to - 604800;
  const feesQuery = new URLSearchParams({ from: String(from), to: String(to) });
  const provider = useQuery(paydQueryOptions({ queryKey: queryKeys.energy.status(), queryFn: () => paydRequest<EnergyStatus>(["energy", "status"]), polling: { tier: "D" } }));
  const params = useQuery(paydQueryOptions({ queryKey: queryKeys.chain.params(), queryFn: () => paydRequest<ChainParameters>(["chain", "params"]), polling: { tier: "D" } }));
  const wallet = useQuery(paydQueryOptions({ queryKey: queryKeys.resources.wallets(), queryFn: () => paydRequest<ResourceWallet>(["resources", "wallet"]), polling: { tier: "D" } }));
  const config = useQuery(paydQueryOptions({ queryKey: queryKeys.config(), queryFn: () => paydRequest<ResourceConfig>(["config"]), polling: { tier: "D" } }));
  const fees = useQuery(paydQueryOptions({ queryKey: queryKeys.reports("fees", Object.fromEntries(feesQuery)), queryFn: () => paydRequest<FeesReport>(["reports", "fees"], {}, feesQuery), polling: { tier: "D" } }));

  return <main className="page"><header><p className="page-kicker"><Zap aria-hidden="true" size={14} strokeWidth={1.75} />Operations / Resources</p><h1 className="page-title mt-1">Resources and energy</h1><p className="mt-1 text-[13px] text-ink-secondary">Why withdrawals are waiting, what resources cost, and whether the provider is quietly failing.</p></header><div className="grid gap-4 xl:grid-cols-2"><ProviderCard status={provider.data} /><ChainParametersCard params={params.data} error={params.error} reload={() => void params.refetch()} /><ResourceWalletCard wallet={wallet.data} config={config.data} error={wallet.error} reload={() => void wallet.refetch()} /><FeeCard fees={fees.data} error={fees.error} reload={() => void fees.refetch()} /></div><ResourcePurchases /><ResourceGrants /><ReadProblem error={provider.error} reload={() => void provider.refetch()} /><ReadProblem error={config.error} reload={() => void config.refetch()} /></main>;
}
