"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import { useSessionExpiry } from "@/app/providers";
import { Amount } from "@/components/data/amount";
import { ConfirmDialog } from "@/components/forms/confirm-dialog";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { AssetsResponse, ConfigResponse, WalletPage, Withdrawal, WithdrawalEstimateResponse, WithdrawalLimits } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

import { EstimateReport, WithdrawalWizardSteps } from "./withdrawal-wizard-steps";

type Draft = { from_address: string; to_address: string; asset: string; amount: string; pasted: boolean };
type FieldErrors = Partial<Record<"from_address" | "to_address" | "asset" | "amount", string>>;
type CreateError = { status: number; code: string; details: Record<string, unknown> };
type Submission = { kind: "existing"; withdrawal: Withdrawal } | { kind: "ambiguous"; lastError: string } | { kind: "error"; title: string; detail: string; allowNew: boolean };
type StoredIdempotencyKey = { key: string; signature: string };

const emptyDraft: Draft = { from_address: "", to_address: "", asset: "", amount: "", pasted: false };
const idempotencyStorageKey = "payd_withdrawal_idempotency";

function transferSignature(draft: Draft): string {
  return JSON.stringify([draft.from_address, draft.to_address, draft.asset, draft.amount]);
}

function storedIdempotencyKey(): StoredIdempotencyKey | null {
  try {
    const value: unknown = JSON.parse(sessionStorage.getItem(idempotencyStorageKey) ?? "null");
    return typeof value === "object" && value !== null && "key" in value && "signature" in value && typeof value.key === "string" && typeof value.signature === "string" ? value as StoredIdempotencyKey : null;
  } catch {
    return null;
  }
}

function storeIdempotencyKey(key: string, signature: string): void {
  // An idempotency key is not a secret; persist it with only its transfer signature to prevent a second payout after navigation.
  sessionStorage.setItem(idempotencyStorageKey, JSON.stringify({ key, signature }));
}

function isPrecisionValid(value: string, decimals: number | undefined): boolean {
  const pieces = value.split(".");
  return /^(?:0|[1-9]\d*)(?:\.\d+)?$/.test(value) && (decimals === undefined || new RegExp(`^\\d{0,${decimals}}$`).test(pieces[1] ?? ""));
}

function csrfToken(): string | undefined {
  return document.cookie.split(";").map((item) => item.trim()).find((item) => item.startsWith("payd_csrf="))?.slice("payd_csrf=".length);
}

async function createWithdrawal(draft: Draft, totp: string, idempotencyKey: string): Promise<{ status: number; withdrawal: Withdrawal }> {
  const csrf = csrfToken();
  const response = await fetch("/api/payd/withdrawals", {
    method: "POST",
    headers: { "content-type": "application/json", "idempotency-key": idempotencyKey, ...(csrf ? { "x-csrf-token": csrf } : {}) },
    credentials: "same-origin",
    cache: "no-store",
    body: JSON.stringify({ from_address: draft.from_address, to_address: draft.to_address, asset: draft.asset, amount: draft.amount, totp }),
  });
  const body: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    const error = typeof body === "object" && body !== null && "error" in body ? (body as { error?: { code?: unknown; details?: unknown } }).error : undefined;
    throw { status: response.status, code: typeof error?.code === "string" ? error.code : "upstream_unreachable", details: typeof error?.details === "object" && error.details !== null ? error.details as Record<string, unknown> : {} } satisfies CreateError;
  }
  return { status: response.status, withdrawal: body as Withdrawal };
}

async function allWallets(path: readonly string[]): Promise<WalletPage> {
  const wallets: WalletPage["wallets"] = [];
  let cursor = "";
  do {
    const query = new URLSearchParams({ limit: "200" });
    if (cursor) query.set("cursor", cursor);
    const page = await paydRequest<WalletPage>(path, {}, query);
    wallets.push(...page.wallets);
    cursor = page.next_cursor;
  } while (cursor);
  return { wallets, next_cursor: "" };
}

function fieldErrorsFor(error: CreateError): FieldErrors {
  switch (error.code) {
    case "invalid_asset": return { asset: "payd rejected this asset." };
    case "invalid_source": case "balance_drift": case "insufficient_confirmed_balance": return { from_address: "payd rejected this source for the requested withdrawal." };
    case "daily_limit_exceeded": return { amount: "This amount exceeds the remaining UTC-day allowance." };
    case "invalid_withdrawal": return { to_address: "payd rejected the destination or amount.", amount: "payd rejected the destination or amount." };
    default: return {};
  }
}

function isAmbiguous(error: unknown): boolean {
  if (!(typeof error === "object" && error !== null && "status" in error)) return true;
  const status = (error as CreateError).status;
  return status !== 503 && status >= 500;
}

export function WithdrawalWizard() {
  const router = useRouter();
  const { isExpired, isExpiringSoon } = useSessionExpiry();
  const [step, setStep] = useState<1 | 2 | 3>(1);
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [estimate, setEstimate] = useState<WithdrawalEstimateResponse | null>(null);
  const [estimateError, setEstimateError] = useState<string | null>(null);
  const [estimating, setEstimating] = useState(false);
  const [idempotencyKey, setIdempotencyKey] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [submission, setSubmission] = useState<Submission | null>(null);

  useEffect(() => {
    setIdempotencyKey(storedIdempotencyKey()?.key ?? null);
  }, []);

  const sources = useQuery(paydQueryOptions({ queryKey: queryKeys.wallets.withBalanceAll(), queryFn: () => allWallets(["wallets", "with-balance"]), polling: { tier: "D" } }));
  const knownAddresses = useQuery(paydQueryOptions({ queryKey: queryKeys.wallets.pooledAll(), queryFn: () => allWallets(["wallets"]), polling: { tier: "D" } }));
  const assets = useQuery(paydQueryOptions({ queryKey: queryKeys.assets(), queryFn: () => paydRequest<AssetsResponse>(["assets"]), polling: { tier: "D" } }));
  const limits = useQuery(paydQueryOptions({ queryKey: queryKeys.withdrawals.limits(), queryFn: () => paydRequest<WithdrawalLimits>(["withdrawals", "limits"]), polling: { tier: "D" } }));
  const config = useQuery(paydQueryOptions({ queryKey: queryKeys.config(), queryFn: () => paydRequest<ConfigResponse>(["config"]), polling: { tier: "D" } }));
  const selectedSource = sources.data?.wallets.find((wallet) => wallet.address === draft.from_address);
  const selectedAsset = assets.data?.assets.find((item) => item.symbol === draft.asset);
  const knownDestination = knownAddresses.data?.wallets.some((wallet) => wallet.address === draft.to_address);
  const readyToEstimate = Boolean(draft.from_address && draft.to_address && draft.asset && draft.amount && draft.pasted && isPrecisionValid(draft.amount, selectedAsset?.decimals));

  const updateDraft = (next: Partial<Draft>) => {
    setDraft((current) => ({ ...current, ...next }));
    setEstimate(null);
    setEstimateError(null);
    setFieldErrors({});
  };
  const estimateWithdrawal = async () => {
    if (!readyToEstimate || estimating) return;
    setEstimating(true);
    setEstimateError(null);
    try {
      const response = await paydRequest<WithdrawalEstimateResponse>(["withdrawals", "estimate"], { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ from_address: draft.from_address, to_address: draft.to_address, asset: draft.asset, amount: draft.amount }) });
      setEstimate(response);
      setStep(2);
    } catch (error) {
      setEstimateError(isPaydError(error) ? `payd could not estimate this withdrawal: ${error.code}.` : "payd could not estimate this withdrawal.");
      setStep(2);
    } finally {
      setEstimating(false);
    }
  };
  const openConfirmation = () => {
    if (!estimate?.can_proceed) return;
    const signature = transferSignature(draft);
    const stored = storedIdempotencyKey();
    const key = stored?.signature === signature ? stored.key : crypto.randomUUID();
    if (stored?.signature !== signature) storeIdempotencyKey(key, signature);
    setIdempotencyKey(key);
    setStep(3);
  };
  const startNew = () => {
    sessionStorage.removeItem(idempotencyStorageKey);
    setIdempotencyKey(null);
    setEstimate(null);
    setSubmission(null);
    setStep(1);
  };
  const submit = async (totp: string) => {
    if (!idempotencyKey) return;
    try {
      const result = await createWithdrawal(draft, totp, idempotencyKey);
      if (result.status === 201) {
        router.push(`/withdrawals/${encodeURIComponent(result.withdrawal.id)}`);
        return;
      }
      setSubmission({ kind: "existing", withdrawal: result.withdrawal });
    } catch (error) {
      if (typeof error === "object" && error !== null && "status" in error && (error as CreateError).status === 503) {
        const payd = error as CreateError;
        setSubmission({ kind: "error", title: payd.code === "price_unavailable" ? "Price is stale or unavailable" : "Withdrawal service unavailable", detail: payd.code === "price_unavailable" ? "payd has no fresh price for this asset. The withdrawal was not created." : "payd refused this withdrawal while the service is unavailable. The withdrawal was not created.", allowNew: true });
        return;
      }
      if (isAmbiguous(error)) {
        const detail = typeof error === "object" && error !== null && "code" in error ? String((error as CreateError).code) : "connection reset";
        setSubmission({ kind: "ambiguous", lastError: detail });
        return;
      }
      const payd = error as CreateError;
      const consumed = payd.details.totp_consumed === true;
      if (payd.status === 409 && payd.code === "idempotency_key_reuse") setSubmission({ kind: "error", title: "Idempotency key mismatch", detail: "This is a client bug: this key was presented with different withdrawal parameters. Start a new withdrawal; do not edit this submitted request.", allowNew: true });
      else if (payd.status === 409 && consumed) setSubmission({ kind: "error", title: "payd code consumed", detail: "That code has been used. Wait for the next code before correcting the request. The request was not created.", allowNew: true });
      else if (payd.status === 401) setSubmission({ kind: "error", title: "payd code rejected", detail: "The payd code was discarded and the withdrawal was not created. Wait for a fresh code before beginning a new withdrawal.", allowNew: true });
      else if (payd.status === 429) setSubmission({ kind: "error", title: "payd rate limit reached", detail: "payd did not create this withdrawal. Wait for the rate limit to clear before beginning a new withdrawal.", allowNew: true });
      else if (payd.status === 400 || payd.status === 409) {
        const fields = fieldErrorsFor(payd);
        setFieldErrors(fields);
        setSubmission(Object.keys(fields).length ? { kind: "error", title: "payd rejected the withdrawal", detail: "Correct the highlighted field in a new withdrawal. The payd code was discarded.", allowNew: true } : { kind: "error", title: "Request rejected", detail: `payd rejected this request (${payd.code}) and did not create a withdrawal. The payd code was discarded.`, allowNew: true });
      } else setSubmission({ kind: "error", title: "Request was not processed", detail: `payd did not create this withdrawal. Status: ${payd.status}. Error code: ${payd.code}. The payd code was discarded.`, allowNew: true });
    }
  };

  return <>
    <section className="lg:hidden p-6"><h1 className="text-2xl font-semibold">Full screen required</h1><p className="mt-2 text-sm text-ink-secondary">Withdrawals must be composed on a screen at least 1024px wide. This flow is intentionally unavailable on smaller screens.</p></section>
    <main className="mx-auto hidden max-w-5xl space-y-5 p-4 lg:block lg:p-6">
      <header><p className="font-mono text-xs uppercase tracking-[0.2em] text-ink-faint">Operations / Withdrawals / New</p><h1 className="mt-1 text-2xl font-semibold">Create a new withdrawal</h1><p className="mt-1 text-sm text-ink-secondary">A new, separate movement of funds. Only confirmed funds are spendable; pending deposits are not.</p></header>
      <WithdrawalWizardSteps step={step} />
      {submission ? <SubmissionPanel submission={submission} onNew={startNew} /> : <>
        {step === 1 ? <section className="space-y-4 border border-border-subtle bg-panel p-4">
          <div className="grid gap-4 md:grid-cols-2">
            <label className="grid gap-1 text-sm">Source address<select value={draft.from_address} onChange={(event) => updateDraft({ from_address: event.currentTarget.value, asset: "" })} className="border border-border-strong bg-inset px-3 py-2 font-mono"><option value="">Choose a funded source address</option>{sources.data?.wallets.map((wallet) => <option key={wallet.address} value={wallet.address}>{wallet.address}</option>)}</select>{fieldErrors.from_address ? <span className="text-xs text-severity-warning">{fieldErrors.from_address}</span> : null}</label>
            <label className="grid gap-1 text-sm">Asset<select value={draft.asset} disabled={!selectedSource} onChange={(event) => updateDraft({ asset: event.currentTarget.value })} className="border border-border-strong bg-inset px-3 py-2 font-mono"><option value="">Choose asset</option>{selectedSource?.balances.map((balance) => <option key={balance.asset} value={balance.asset}>{balance.asset}</option>)}</select>{fieldErrors.asset ? <span className="text-xs text-severity-warning">{fieldErrors.asset}</span> : null}</label>
          </div>
          {selectedSource ? <div className="overflow-x-auto border border-border-subtle"><table className="w-full min-w-max text-left text-sm"><thead className="bg-raised text-xs uppercase tracking-wide text-ink-secondary"><tr><th className="px-3 py-2">Asset</th><th className="px-3 py-2">Confirmed</th><th className="px-3 py-2">Pending</th><th className="px-3 py-2">Withdrawal state</th></tr></thead><tbody>{selectedSource.balances.map((balance) => <tr key={balance.asset} className="border-t border-border-subtle"><td className="px-3 py-2 font-mono">{balance.asset}</td><td className="px-3 py-2"><Amount value={balance.confirmed} asset={balance.asset} /></td><td className="px-3 py-2"><Amount value={balance.pending} asset={balance.asset} /></td><td className="px-3 py-2">{selectedSource.can_withdraw[balance.asset] ? <span className="text-severity-success">Can withdraw</span> : <span className="inline-flex items-center gap-1 text-severity-warning"><AlertTriangle aria-hidden="true" size={14} />Blocked{selectedSource.blocked_by.length ? `: ${selectedSource.blocked_by.join(", ")}` : ""}</span>}</td></tr>)}</tbody></table></div> : null}
          <label className="grid gap-1 text-sm">Destination address<input value={draft.to_address} onChange={(event) => updateDraft({ to_address: event.currentTarget.value, pasted: false })} className="border border-border-strong bg-inset px-3 py-2 font-mono" autoComplete="off" />{fieldErrors.to_address ? <span className="text-xs text-severity-warning">{fieldErrors.to_address}</span> : null}</label>
          <label className="flex items-start gap-2 text-sm"><input type="checkbox" checked={draft.pasted} onChange={(event) => updateDraft({ pasted: event.currentTarget.checked })} className="mt-1" />I pasted and verified this destination address.</label>
          {knownDestination ? <p role="alert" className="flex items-start gap-2 border border-severity-warning bg-[var(--severity-warning-bg)] p-3 text-sm text-severity-warning"><AlertTriangle aria-hidden="true" className="mt-0.5" size={16} />This destination is a known pooled deposit address. Verify that sending funds back into the pool is intentional.</p> : null}
          <label className="grid gap-1 text-sm">Amount<input value={draft.amount} onChange={(event) => updateDraft({ amount: event.currentTarget.value })} inputMode="decimal" className="border border-border-strong bg-inset px-3 py-2 font-mono tabular-nums" aria-describedby="amount-help" />{fieldErrors.amount ? <span className="text-xs text-severity-warning">{fieldErrors.amount}</span> : null}<span id="amount-help" className="text-xs text-ink-faint">Decimal string only{selectedAsset ? `; up to ${selectedAsset.decimals} fractional digits for ${selectedAsset.symbol}.` : "."}</span></label>
          <section className="border border-border-subtle bg-inset p-3 text-sm"><p className="font-medium">UTC-day allowance</p>{limits.data ? <p className="mt-1"><Amount value={limits.data.remaining_usd} asset="USD" /> remaining of <Amount value={limits.data.daily_limit_usd} asset="USD" />; <Amount value={limits.data.used_usd} asset="USD" /> used.</p> : <p className="mt-1 text-ink-secondary">Loading payd allowance.</p>}</section>
          <button type="button" disabled={!readyToEstimate || estimating} onClick={() => void estimateWithdrawal()} className="border border-severity-progress px-3 py-2 font-medium disabled:cursor-not-allowed disabled:opacity-50">{estimating ? "Calculating estimate…" : "Calculate estimate"}</button>
        </section> : null}
        {step === 2 ? <>{estimate ? <EstimateReport estimate={estimate} config={config.data} /> : null}{estimateError ? <p role="alert" className="border border-severity-warning bg-[var(--severity-warning-bg)] p-3 text-sm text-severity-warning">{estimateError}</p> : null}<div className="flex gap-3"><button type="button" onClick={() => setStep(1)} className="border border-border-strong px-3 py-2">Back to compose</button>{estimate?.can_proceed ? <button type="button" onClick={openConfirmation} className="border border-severity-warning bg-[var(--severity-warning-bg)] px-3 py-2 font-medium">Review confirmation</button> : null}</div></> : null}
      </>}
    </main>
    {estimate ? <ConfirmDialog open={step === 3 && !submission} onClose={() => setStep(2)} title="Authorise withdrawal" confirmLabel={`Withdraw ${estimate.amount} ${estimate.asset}`} requiresTotp ready={estimate.can_proceed && !isExpired} onConfirm={submit} apiText={<Confirmation estimate={estimate} expiring={isExpiringSoon} expired={isExpired} />} /> : null}
  </>;
}

function Confirmation({ estimate, expiring, expired }: Readonly<{ estimate: WithdrawalEstimateResponse; expiring: boolean; expired: boolean }>) {
  return <><p>Confirm the transfer exactly as payd estimated it.</p><dl className="mt-3 grid gap-3 text-sm"><div><dt className="text-xs uppercase tracking-wide text-ink-faint">Source</dt><dd><code className="select-all break-all font-mono">{estimate.from_address}</code></dd></div><div><dt className="text-xs uppercase tracking-wide text-ink-faint">Destination</dt><dd><code className="select-all break-all font-mono">{estimate.to_address}</code></dd></div><div><dt className="text-xs uppercase tracking-wide text-ink-faint">Asset / amount</dt><dd><Amount value={estimate.amount} asset={estimate.asset} /></dd></div><div><dt className="text-xs uppercase tracking-wide text-ink-faint">Base units / USD</dt><dd><code className="font-mono">{estimate.amount_raw}</code> · <Amount value={estimate.amount_usd} asset="USD" /></dd></div><div><dt className="text-xs uppercase tracking-wide text-ink-faint">Projected energy / cost</dt><dd><code className="font-mono">{estimate.projected_energy_source || "unknown"}</code> · <Amount value={estimate.projected_trx_cost} asset="TRX" /></dd></div></dl><p className="mt-3 font-medium">Only confirmed funds are spendable. Pending deposits are not.</p>{expired ? <p role="alert" className="mt-3 flex gap-2 text-severity-critical"><AlertTriangle aria-hidden="true" size={16} />Dashboard session expired. Log in again before submitting.</p> : null}{expiring ? <p role="alert" className="mt-3 flex gap-2 text-severity-warning"><AlertTriangle aria-hidden="true" size={16} />Dashboard session expires soon. Log in again before entering a payd code if it may expire during this action.</p> : null}</>;
}

function SubmissionPanel({ submission, onNew }: Readonly<{ submission: Submission; onNew: () => void }>) {
  if (submission.kind === "existing") return <section className="border border-severity-warning bg-[var(--severity-warning-bg)] p-4" role="alert"><h2 className="font-semibold">Existing withdrawal returned</h2><p className="mt-2 text-sm">payd returned an existing withdrawal for this idempotency key. No payd code was checked, and no new withdrawal was created.</p><Link href={`/withdrawals/${encodeURIComponent(submission.withdrawal.id)}`} className="mt-3 inline-block text-severity-progress underline underline-offset-2">Open withdrawal <code className="font-mono">{submission.withdrawal.id}</code></Link></section>;
  if (submission.kind === "ambiguous") return <section className="border border-severity-critical bg-[var(--severity-critical-bg)] p-4 text-severity-critical" role="alert"><div className="flex items-center gap-2"><AlertTriangle aria-hidden="true" size={18} /><h2 className="font-semibold">Outcome unknown</h2></div><p className="mt-3 text-sm">Funds may or may not have moved. No transaction ID or last lookup error was returned with this interrupted request. If a row exists, open it and use its transaction ID and last lookup error to determine the outcome. The service will not attempt the transfer again. Recording an outcome is a decision record, not an action. Check the withdrawal list for a row created in the last minute before doing anything else. The request may have reached payd, consumed the payd code, and created the row.</p><p className="mt-2 text-xs">Last error: <code className="font-mono">{submission.lastError}</code></p><Link href="/withdrawals" className="mt-3 inline-block underline underline-offset-2">Check withdrawal list</Link></section>;
  return <section className="border border-severity-warning bg-[var(--severity-warning-bg)] p-4 text-severity-warning" role="alert"><h2 className="font-semibold">{submission.title}</h2><p className="mt-2 text-sm">{submission.detail}</p>{submission.allowNew ? <button type="button" onClick={onNew} className="mt-3 border border-severity-warning px-3 py-2 font-medium">Create a new withdrawal</button> : null}</section>;
}
