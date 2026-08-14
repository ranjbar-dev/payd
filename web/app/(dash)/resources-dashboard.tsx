"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertOctagon, AlertTriangle } from "lucide-react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useEffect } from "react";

import { Amount } from "@/components/data/amount";
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

function ReadProblem({ error }: Readonly<{ error: unknown }>) {
  if (!error) return null;
  const paydError = isPaydError(error) ? error : null;
  const code = paydError?.code ?? "upstream_unreachable";
  const copy: Record<string, string> = { unauthorized: "This dashboard session or its upstream scope is not authorised.", rate_limited: "Refresh has slowed because payd is rate limited.", upstream_unreachable: "payd could not be reached; any last available data remains visible.", upstream_timeout: "payd did not answer in time; any last available data remains visible." };
  return <div className="mt-3 border border-severity-warning bg-[var(--severity-warning-bg)] p-3 text-sm" role="alert"><p className="inline-flex items-center gap-1"><AlertTriangle aria-hidden="true" size={15} />{copy[code] ?? "An unrecognised error was returned."}</p><p className="mt-1 text-ink-secondary">Error code: <code className="select-all font-mono text-ink">{code}</code></p>{paydError?.details ? <pre className="mt-2 overflow-auto border-t border-border-subtle pt-2 text-xs text-ink-secondary">{JSON.stringify(paydError.details, null, 2)}</pre> : null}</div>;
}

function decimal(value: string): boolean {
  return /^\d+(?:\.\d+)?$/.test(value);
}

function ProviderCard({ status }: Readonly<{ status?: EnergyStatus }>) {
  if (!status) return <section className="border border-border-subtle bg-panel p-4"><h2 className="font-semibold">Energy provider</h2><p className="mt-2 text-sm text-ink-secondary">Loading provider diagnostics.</p></section>;
  const balanceKnown = decimal(status.balance_trx);
  const failuresCritical = status.consecutive_failures >= 5;
  const concern = failuresCritical || status.balance_low || !balanceKnown;
  const Icon = failuresCritical ? AlertOctagon : AlertTriangle;
  const severity = failuresCritical ? "critical" : "warning";
  const balanceState = !balanceKnown ? "Unknown — not reported as healthy" : status.balance_low ? "Low (backend verdict)" : "Not low (backend verdict)";
  return <section className={`border bg-panel p-4 ${concern ? `border-severity-${severity}` : "border-border-subtle"}`}><div className="flex items-center justify-between gap-3"><h2 className="font-semibold">Energy provider</h2>{concern ? <span className={`inline-flex items-center gap-1 text-sm text-severity-${severity}`}><Icon aria-hidden="true" size={16} />{failuresCritical ? "Tier 1 unavailable" : "Attention"}</span> : null}</div><dl className="mt-3 grid gap-3 text-sm sm:grid-cols-2"><div><dt className="text-ink-faint">Provider</dt><dd className="font-mono">{status.provider}</dd></div><div><dt className="text-ink-faint">Provider balance / warning threshold</dt><dd><Amount value={status.balance_trx} asset="TRX" /> <span className="text-ink-faint">/</span> <Amount value={status.balance_warn_trx} asset="TRX" /><p className={concern ? `mt-1 text-xs text-severity-${severity}` : "mt-1 text-xs text-ink-secondary"}>{balanceState}</p></dd></div><div><dt className="text-ink-faint">Last checked</dt><dd><Timestamp seconds={status.last_checked_at} /><p className="mt-1 text-xs text-ink-secondary">Provider balance checks run every 15 minutes.</p></dd></div><div><dt className="text-ink-faint">Consecutive failures</dt><dd className="font-mono tabular-nums">{status.consecutive_failures}{failuresCritical ? <p className="mt-1 text-xs text-severity-critical">Tier 1 is skipped entirely for 10 minutes.</p> : null}</dd></div><div className="sm:col-span-2"><dt className="text-ink-faint">Last provider error</dt><dd><code className="select-all font-mono text-xs">{status.last_error || "—"}</code><p className="mt-1 text-xs text-ink-secondary">Recorded at the last provider check: <Timestamp seconds={status.last_checked_at} />.</p></dd></div></dl><div className="mt-4 border-t border-border-subtle pt-3"><p className="text-xs text-ink-secondary">Provider calls do not count against the TronGrid quota.</p><p className="mt-2 text-xs uppercase tracking-wide text-ink-faint">Purchase outcomes</p><div className="mt-1 flex flex-wrap gap-x-4 gap-y-1 font-mono text-xs tabular-nums">{Object.entries(status.purchases).map(([outcome, count]) => <span key={outcome}>{outcome}: {count}</span>)}</div></div></section>;
}

function ChainParametersCard({ params, error }: Readonly<{ params?: ChainParameters; error: unknown }>) {
  const unavailable = isPaydError(error) && error.code === "chain_params_unavailable";
  if (unavailable) return <section className="border-2 border-severity-critical bg-panel p-4" role="alert"><div className="flex items-center gap-2 text-severity-critical"><AlertOctagon aria-hidden="true" size={18} /><h2 className="font-semibold">Chain parameters unavailable</h2></div><p className="mt-2 text-sm">The service holds withdrawals rather than assuming a price.</p><p className="mt-2 text-xs text-ink-secondary">Error code: <code className="select-all font-mono text-ink">chain_params_unavailable</code></p></section>;
  if (!params) return <section className="border border-border-subtle bg-panel p-4"><h2 className="font-semibold">Chain parameters</h2><p className="mt-2 text-sm text-ink-secondary">Loading live fee parameters.</p><ReadProblem error={error} /></section>;
  const verdict = params.burn_exceeds_ceiling === undefined ? "Unknown — backend could not compare the figures" : params.burn_exceeds_ceiling ? "Exceeds ceiling (backend verdict)" : "Within ceiling (backend verdict)";
  return <section className={`border bg-panel p-4 ${params.burn_exceeds_ceiling ? "border-severity-warning" : "border-border-subtle"}`}><div className="flex flex-wrap items-center justify-between gap-3"><h2 className="font-semibold">Chain parameters</h2>{params.burn_exceeds_ceiling ? <span className="inline-flex items-center gap-1 text-sm text-severity-warning"><AlertTriangle aria-hidden="true" size={16} />Burn ceiling exceeded</span> : params.stale ? <span className="inline-flex items-center gap-1 text-sm text-severity-warning"><AlertTriangle aria-hidden="true" size={16} />Backend marks this reading stale</span> : null}</div><dl className="mt-3 grid gap-3 text-sm sm:grid-cols-2"><div><dt className="text-ink-faint">Energy fee</dt><dd className="font-mono tabular-nums">{params.getEnergyFee} SUN / energy</dd><p className="mt-1 text-xs text-ink-secondary">For a worst-case 131,000-energy transfer, backend-calculated burn: <Amount value={params.worst_case_burn_trx} asset="TRX" />.</p></div><div><dt className="text-ink-faint">Transaction fee</dt><dd className="font-mono tabular-nums">{params.getTransactionFee} SUN / bandwidth</dd></div><div><dt className="text-ink-faint">Worst-case burn / configured ceiling</dt><dd><Amount value={params.worst_case_burn_trx} asset="TRX" /> <span className="text-ink-faint">/</span> <Amount value={params.max_burn_trx} asset="TRX" /><p className={`mt-1 text-xs ${params.burn_exceeds_ceiling ? "text-severity-warning" : "text-ink-secondary"}`}>{verdict}</p></dd></div><div><dt className="text-ink-faint">Read at</dt><dd><Timestamp seconds={params.fetched_at} /><p className="mt-1 text-xs text-ink-secondary">Refreshed at startup and every 6 hours.</p></dd></div></dl><ReadProblem error={error} /></section>;
}

function ResourceLine({ name, value, delegation }: Readonly<{ name: string; value: WalletResources; delegation?: Delegation }>) {
  return <div><dt className="text-ink-faint">{name}</dt><dd className="font-mono tabular-nums">available {value.available} / limit {value.limit}</dd><p className="mt-1 text-xs text-ink-secondary">Non-failed self-delegations: {delegation?.count ?? 0} grants · {delegation ? <Amount value={delegation.stake_trx} asset="TRX" /> : "—"}</p></div>;
}

function ResourceWalletCard({ wallet, config, error }: Readonly<{ wallet?: ResourceWallet; config?: ResourceConfig; error: unknown }>) {
  if (!wallet) return <section className="border border-border-subtle bg-panel p-4"><h2 className="font-semibold">Resource wallet</h2><p className="mt-2 text-sm text-ink-secondary">Loading the withdrawal path dependency.</p><ReadProblem error={error} /></section>;
  return <section className="border border-border-subtle bg-panel p-4"><h2 className="font-semibold">Resource wallet</h2><p className="mt-1 text-sm text-ink-secondary">Withdrawal path dependency: an empty wallet blocks tier-2 delegation and bandwidth top-ups.</p><dl className="mt-3 grid gap-3 text-sm sm:grid-cols-2"><div><dt className="text-ink-faint">Permanent disabled pool entry</dt><dd><Link href={`/addresses/${encodeURIComponent(wallet.address)}`} className="font-mono text-severity-progress underline underline-offset-2">{wallet.address}</Link></dd></div><div><dt className="text-ink-faint">Confirmed TRX / bandwidth reserve</dt><dd><Amount value={wallet.trx_balance} asset="TRX" /> <span className="text-ink-faint">/</span> {config ? <Amount value={config.resources.bandwidth_topup_trx} asset="TRX" /> : <span className="text-ink-faint">loading reserve</span>}<p className="mt-1 text-xs text-ink-secondary">Both figures are shown for operator review; payd supplies no comparison verdict.</p></dd></div><ResourceLine name="Energy" value={wallet.energy} delegation={wallet.outstanding_delegations.ENERGY} /><ResourceLine name="Bandwidth" value={wallet.bandwidth} delegation={wallet.outstanding_delegations.BANDWIDTH} /></dl><p className="mt-4 text-xs text-ink-secondary">Staking and unstaking are manual chain operations the service never performs. Unstaking takes 14 days.</p><ReadProblem error={error} /></section>;
}

function FeeCard({ fees, error }: Readonly<{ fees?: FeesReport; error: unknown }>) {
  const burned = fees?.energy_by_source_trx.burned;
  const rented = fees?.energy_by_source_trx.rented;
  const value = (amount?: string) => amount ? <Amount value={amount} asset="TRX" /> : <span className="text-ink-faint">—</span>;
  return <section className="border border-border-subtle bg-panel p-4"><div className="flex flex-wrap items-start justify-between gap-3"><div><h2 className="font-semibold">Burn versus rent</h2><p className="mt-1 text-sm text-ink-secondary">Recent seven-day window (UTC). Rising burn cost is what a silently failing provider looks like.</p></div><Link href="/reports/fees" className="text-sm text-severity-progress underline underline-offset-2">Open full fee report</Link></div>{fees ? <dl className="mt-3 grid gap-3 text-sm sm:grid-cols-3"><div><dt className="text-ink-faint">Energy burned</dt><dd>{value(burned)}</dd></div><div><dt className="text-ink-faint">Energy rented</dt><dd>{value(rented)}</dd></div><div><dt className="text-ink-faint">Rental spend</dt><dd><Amount value={fees.rental_spend_trx} asset="TRX" /></dd></div></dl> : <p className="mt-3 text-sm text-ink-secondary">Loading backend fee split.</p>}<ReadProblem error={error} /></section>;
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

  return <main className="mx-auto max-w-7xl space-y-4 p-4 lg:p-6"><header><p className="font-mono text-xs uppercase tracking-[0.2em] text-ink-faint">Operations / Resources</p><h1 className="mt-1 text-2xl font-semibold">Resources and energy</h1><p className="mt-1 text-sm text-ink-secondary">Why withdrawals are waiting, what resources cost, and whether the provider is quietly failing.</p></header><div className="grid gap-4 xl:grid-cols-2"><ProviderCard status={provider.data} /><ChainParametersCard params={params.data} error={params.error} /><ResourceWalletCard wallet={wallet.data} config={config.data} error={wallet.error} /><FeeCard fees={fees.data} error={fees.error} /></div><ResourcePurchases /><ResourceGrants /><ReadProblem error={provider.error} /><ReadProblem error={config.error} /></main>;
}
