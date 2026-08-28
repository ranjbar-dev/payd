"use client";

import { AlertTriangle } from "lucide-react";
import Link from "next/link";

import { Amount } from "@/components/data/amount";
import type { ConfigResponse, WithdrawalEstimateResponse } from "@/lib/payd/types";

const blockedCopy = {
  withdrawals_disabled: "Withdrawals are disabled. Ask the payd administrator to enable withdrawals before creating a separate withdrawal.",
  confirmed_balance: "The source does not have enough confirmed funds of the asset being sent. Deposit more of that asset and wait for confirmation.",
  trx_for_resources: "The source does not have enough confirmed TRX for projected resources. Top this address up with TRX and wait for confirmation.",
  daily_usd_cap: "This UTC-day allowance would be exceeded. Wait for the UTC reset or use the limits view to plan a separate withdrawal.",
  energy_unavailable: "Energy cannot be sourced now. Check the resources service and wait until payd can obtain energy.",
  energy_burn_limit: "The live burn cost exceeds the configured burn ceiling. Review the energy configuration before creating a separate withdrawal.",
  chain_parameters_unavailable: "payd has not read getEnergyFee yet, so it is holding withdrawals instead of assuming a price.",
} as const;

export function WithdrawalWizardSteps({ step }: Readonly<{ step: 1 | 2 | 3 }>) {
  return <ol className="grid gap-px border border-border-subtle bg-border-subtle sm:grid-cols-3" aria-label="Withdrawal steps">
    {(["Compose", "Estimate", "Confirm + payd code"] as const).map((label, index) => {
      const position = (index + 1) as 1 | 2 | 3;
      const current = step === position;
      const complete = step > position;
      return <li key={label} className={`flex items-center gap-2 bg-panel px-3 py-3 text-sm ${current ? "text-ink" : "text-ink-secondary"}`} aria-current={current ? "step" : undefined}>
        <span className={`flex h-6 w-6 items-center justify-center rounded-full border font-mono text-xs ${current ? "border-severity-progress text-severity-progress" : complete ? "border-severity-success text-severity-success" : "border-border-strong"}`}>{position}</span>
        <span>{label}</span>
      </li>;
    })}
  </ol>;
}

export function EstimateReport({ estimate, config }: Readonly<{ estimate: WithdrawalEstimateResponse; config?: ConfigResponse }>) {
  const source = estimate.projected_energy_source || "unknown";
  return <section className="space-y-4 border border-border-subtle bg-panel p-4" aria-live="polite">
    <div><h2 className="font-semibold">payd projection</h2><p className="mt-1 text-sm text-ink-secondary">This is a projection only. No state was written, and conditions can change before signing.</p></div>
    <dl className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <div><dt className="text-xs uppercase tracking-wide text-ink-faint">Projected energy source</dt><dd className="mt-1 font-mono">{source}</dd></div>
      <div><dt className="text-xs uppercase tracking-wide text-ink-faint">Projected TRX cost</dt><dd className="mt-1"><Amount value={estimate.projected_trx_cost} asset="TRX" /></dd></div>
      <div><dt className="text-xs uppercase tracking-wide text-ink-faint">Daily-cap status</dt><dd className={`mt-1 ${estimate.daily_cap_blocked ? "text-severity-warning" : "text-severity-success"}`}>{estimate.daily_cap_blocked ? "Blocked" : "Within UTC-day allowance"}</dd></div>
      <div><dt className="text-xs uppercase tracking-wide text-ink-faint">Proceed</dt><dd className={`mt-1 ${estimate.can_proceed ? "text-severity-success" : "text-severity-warning"}`}>{estimate.can_proceed ? "Allowed by this projection" : "Blocked by this projection"}</dd></div>
    </dl>
    <div className="grid gap-3 sm:grid-cols-2">
      <Verdict label="Confirmed asset balance" sufficient={estimate.confirmed_balance_sufficient} remedy="Deposit more of the asset being sent; pending deposits are not spendable." />
      <Verdict label="Confirmed TRX for resources" sufficient={estimate.trx_for_resources_sufficient} remedy="Top the source address up with confirmed TRX for resource costs." />
    </div>
    {estimate.blocked_by.length ? <div className="space-y-3 border border-severity-warning bg-[var(--severity-warning-bg)] p-3 text-sm text-severity-warning" role="alert">
      <div className="flex items-center gap-2 font-medium"><AlertTriangle aria-hidden="true" size={16} />payd will not allow this withdrawal</div>
      <ul className="space-y-2">{estimate.blocked_by.map((reason) => <li key={reason}>{reason === "energy_burn_limit" ? <><p>{blockedCopy[reason]}</p><p className="mt-1">Configured <code className="font-mono">energy.max_burn_trx</code>: {config ? <Amount value={config.energy.max_burn_trx} asset="TRX" /> : "loading"}. Live computed burn cost: <Amount value={estimate.projected_trx_cost} asset="TRX" />.</p></> : reason === "chain_parameters_unavailable" ? <><p>{blockedCopy[reason]}</p><Link href="/" className="underline underline-offset-2">Open the chain parameters card</Link></> : blockedCopy[reason]}</li>)}</ul>
    </div> : null}
  </section>;
}

function Verdict({ label, sufficient, remedy }: Readonly<{ label: string; sufficient: boolean; remedy: string }>) {
  return <div className={`border p-3 text-sm ${sufficient ? "border-severity-success" : "border-severity-warning bg-[var(--severity-warning-bg)]"}`}><p className="font-medium">{label}: {sufficient ? "sufficient" : "insufficient"}</p>{!sufficient ? <p className="mt-1 text-ink-secondary">{remedy}</p> : null}</div>;
}
