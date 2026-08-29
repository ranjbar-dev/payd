"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, ArrowLeft, ArrowRight, Check, Loader2, Plus, Wallet } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import { useSessionExpiry } from "@/app/providers";
import { Amount } from "@/components/data/amount";
import { EmptyState } from "@/components/data/empty-state";
import { ErrorState } from "@/components/data/error-state";
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
    <main className="page hidden max-w-5xl lg:block">
      <header><p className="page-kicker"><Plus aria-hidden="true" size={14} strokeWidth={1.75} />Operations / Withdrawals / New</p><h1 className="page-title mt-1">Create a new withdrawal</h1><p className="mt-1 text-sm text-ink-secondary">A new, separate movement of funds. Only confirmed funds are spendable; pending deposits are not.</p></header>
      <WithdrawalWizardSteps step={step} />
      {submission ? <SubmissionPanel submission={submission} onNew={startNew} /> : <>
        {sources.isError ? <ErrorState error={{ code: "wallets_unavailable" }} copyByCode={{ wallets_unavailable: "payd did not return funded source addresses." }} onRetry={() => void sources.refetch()} /> : null}
        {assets.isError ? <ErrorState error={{ code: "assets_unavailable" }} copyByCode={{ assets_unavailable: "payd did not return the available withdrawal assets." }} onRetry={() => void assets.refetch()} /> : null}
        {limits.isError ? <ErrorState error={{ code: "limits_unavailable" }} copyByCode={{ limits_unavailable: "payd did not return the UTC-day withdrawal allowance." }} onRetry={() => void limits.refetch()} /> : null}
        {step === 1 ? <section className="card space-y-4" aria-labelledby="withdrawal-compose-title">
          <div><h2 id="withdrawal-compose-title" className="card-title">Compose withdrawal</h2><p className="mt-1 text-sm text-ink-secondary">Choose confirmed funds, then paste and verify the destination before estimating.</p></div>
          {sources.isLoading || assets.isLoading ? <div className="animate-pulse space-y-3" aria-label="Loading withdrawal form options"><div className="h-3 w-32 rounded bg-raised" /><div className="h-8 rounded bg-raised" /><div className="h-3 w-20 rounded bg-raised" /><div className="h-8 rounded bg-raised" /></div> : null}
          {sources.data && sources.data.wallets.length === 0 ? <EmptyState kind="worklist" title="No funded source addresses" description="A source address appears here after it holds confirmed funds." icon={<Wallet aria-hidden="true" size={20} strokeWidth={1.75} />} /> : null}
          <div className="grid gap-4 md:grid-cols-2">
            <label className="field">Source address<select value={draft.from_address} onChange={(event) => updateDraft({ from_address: event.currentTarget.value, asset: "" })} className="input cursor-pointer font-mono"><option value="">Choose a funded source address</option>{sources.data?.wallets.map((wallet) => <option key={wallet.address} value={wallet.address}>{wallet.address}</option>)}</select>{fieldErrors.from_address ? <span className="normal-case tracking-normal text-[13px] text-severity-warning">{fieldErrors.from_address}</span> : null}</label>
            <label className="field">Asset<select value={draft.asset} disabled={!selectedSource} onChange={(event) => updateDraft({ asset: event.currentTarget.value })} className="input cursor-pointer font-mono"><option value="">Choose asset</option>{selectedSource?.balances.map((balance) => <option key={balance.asset} value={balance.asset}>{balance.asset}</option>)}</select>{fieldErrors.asset ? <span className="normal-case tracking-normal text-[13px] text-severity-warning">{fieldErrors.asset}</span> : null}</label>
          </div>
          {selectedSource ? <div className="overflow-x-auto"><table className="w-full min-w-max text-left text-[13px]"><thead><tr><th className="th">Asset</th><th className="th text-right">Confirmed</th><th className="th text-right">Pending</th><th className="th">Withdrawal state</th></tr></thead><tbody>{selectedSource.balances.map((balance) => <tr key={balance.asset} className="row-hover"><td className="td font-mono">{balance.asset}</td><td className="td text-right font-mono tabular-nums"><Amount value={balance.confirmed} asset={balance.asset} /></td><td className="td text-right font-mono tabular-nums"><Amount value={balance.pending} asset={balance.asset} /></td><td className="td">{selectedSource.can_withdraw[balance.asset] ? <span className="text-severity-success">Can withdraw</span> : <span className="inline-flex items-center gap-1 text-severity-warning"><AlertTriangle aria-hidden="true" size={14} />Blocked{selectedSource.blocked_by.length ? `: ${selectedSource.blocked_by.join(", ")}` : ""}</span>}</td></tr>)}</tbody></table></div> : null}
          <label className="field">Destination address<input value={draft.to_address} onChange={(event) => updateDraft({ to_address: event.currentTarget.value, pasted: false })} className="input font-mono" autoComplete="off" />{fieldErrors.to_address ? <span className="normal-case tracking-normal text-[13px] text-severity-warning">{fieldErrors.to_address}</span> : null}</label>
          <label className="inline-flex cursor-pointer items-start gap-2 text-[13px] text-ink-secondary transition-colors duration-150 hover:text-ink"><input type="checkbox" checked={draft.pasted} onChange={(event) => updateDraft({ pasted: event.currentTarget.checked })} className="mt-1" />I pasted and verified this destination address.</label>
          {knownDestination ? <p role="alert" className="flex items-start gap-2 border border-severity-warning bg-[var(--severity-warning-bg)] p-3 text-sm text-severity-warning"><AlertTriangle aria-hidden="true" className="mt-0.5" size={16} />This destination is a known pooled deposit address. Verify that sending funds back into the pool is intentional.</p> : null}
          <label className="field">Amount<input value={draft.amount} onChange={(event) => updateDraft({ amount: event.currentTarget.value })} inputMode="decimal" className="input font-mono tabular-nums" aria-describedby="amount-help" />{fieldErrors.amount ? <span className="normal-case tracking-normal text-[13px] text-severity-warning">{fieldErrors.amount}</span> : null}<span id="amount-help" className="normal-case tracking-normal text-[13px] text-ink-faint">Decimal string only{selectedAsset ? `; up to ${selectedAsset.decimals} fractional digits for ${selectedAsset.symbol}.` : "."}</span></label>
          <dl className="grid gap-2 border-y border-border-subtle py-3 text-sm sm:grid-cols-3"><div><dt className="text-ink-faint text-[11px] uppercase">UTC-day allowance</dt><dd className="mt-1 font-mono tabular-nums">{limits.data ? <Amount value={limits.data.remaining_usd} asset="USD" /> : "Loading…"}</dd></div><div><dt className="text-ink-faint text-[11px] uppercase">Daily cap</dt><dd className="mt-1 font-mono tabular-nums">{limits.data ? <Amount value={limits.data.daily_limit_usd} asset="USD" /> : "—"}</dd></div><div><dt className="text-ink-faint text-[11px] uppercase">Used</dt><dd className="mt-1 font-mono tabular-nums">{limits.data ? <Amount value={limits.data.used_usd} asset="USD" /> : "—"}</dd></div></dl>
          <div><button type="button" disabled={!readyToEstimate || estimating} onClick={() => void estimateWithdrawal()} className="btn btn-primary">{estimating ? <Loader2 aria-hidden="true" size={14} strokeWidth={1.75} className="animate-spin" /> : <ArrowRight aria-hidden="true" size={14} strokeWidth={1.75} />}{estimating ? "Calculating estimate…" : "Calculate estimate"}</button></div>
        </section> : null}
        {step === 2 ? <>{estimate ? <EstimateReport estimate={estimate} config={config.data} /> : null}{estimateError ? <p role="alert" className="card border-severity-warning text-sm text-severity-warning">{estimateError}</p> : null}<div className="flex gap-2"><button type="button" onClick={() => setStep(1)} className="btn btn-secondary"><ArrowLeft aria-hidden="true" size={14} strokeWidth={1.75} />Back to compose</button>{estimate?.can_proceed ? <button type="button" onClick={openConfirmation} className="btn btn-primary"><Check aria-hidden="true" size={14} strokeWidth={1.75} />Review confirmation</button> : null}</div></> : null}
      </>}
    </main>
    {estimate ? <ConfirmDialog open={step === 3 && !submission} onClose={() => setStep(2)} title="Authorise withdrawal" confirmLabel={`Withdraw ${estimate.amount} ${estimate.asset}`} requiresTotp ready={estimate.can_proceed && !isExpired} onConfirm={submit} apiText={<Confirmation estimate={estimate} expiring={isExpiringSoon} expired={isExpired} />} /> : null}
  </>;
}

function Confirmation({ estimate, expiring, expired }: Readonly<{ estimate: WithdrawalEstimateResponse; expiring: boolean; expired: boolean }>) {
  return <><p>Confirm the transfer exactly as payd estimated it.</p><dl className="mt-3 grid gap-3 text-sm"><div><dt className="text-ink-faint text-[11px] uppercase">Source</dt><dd className="mt-1 font-mono"><code className="select-all break-all">{estimate.from_address}</code></dd></div><div><dt className="text-ink-faint text-[11px] uppercase">Destination</dt><dd className="mt-1 font-mono"><code className="select-all break-all">{estimate.to_address}</code></dd></div><div><dt className="text-ink-faint text-[11px] uppercase">Asset / amount</dt><dd className="mt-1 font-mono tabular-nums"><Amount value={estimate.amount} asset={estimate.asset} /></dd></div><div><dt className="text-ink-faint text-[11px] uppercase">Base units / USD</dt><dd className="mt-1 font-mono tabular-nums"><code>{estimate.amount_raw}</code> · <Amount value={estimate.amount_usd} asset="USD" /></dd></div><div><dt className="text-ink-faint text-[11px] uppercase">Projected energy / cost</dt><dd className="mt-1 font-mono tabular-nums"><code>{estimate.projected_energy_source || "unknown"}</code> · <Amount value={estimate.projected_trx_cost} asset="TRX" /></dd></div></dl><p className="mt-3 font-medium">Only confirmed funds are spendable. Pending deposits are not.</p>{expired ? <p role="alert" className="mt-3 flex gap-2 text-severity-critical"><AlertTriangle aria-hidden="true" size={16} />Dashboard session expired. Log in again before submitting.</p> : null}{expiring ? <p role="alert" className="mt-3 flex gap-2 text-severity-warning"><AlertTriangle aria-hidden="true" size={16} />Dashboard session expires soon. Log in again before entering a payd code if it may expire during this action.</p> : null}</>;
}

function SubmissionPanel({ submission, onNew }: Readonly<{ submission: Submission; onNew: () => void }>) {
  if (submission.kind === "existing") return <section className="card border-severity-warning" role="alert"><h2 className="card-title text-severity-warning">Existing withdrawal returned</h2><p className="mt-2 text-sm">payd returned an existing withdrawal for this idempotency key. No payd code was checked, and no new withdrawal was created.</p><Link href={`/withdrawals/${encodeURIComponent(submission.withdrawal.id)}`} className="mt-3 inline-flex cursor-pointer items-center gap-1 text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">Open withdrawal <code className="font-mono">{submission.withdrawal.id}</code></Link></section>;
  if (submission.kind === "ambiguous") return <section className="card border-severity-critical text-severity-critical" role="alert"><div className="flex items-center gap-2"><AlertTriangle aria-hidden="true" size={18} /><h2 className="card-title text-severity-critical">Outcome unknown</h2></div><p className="mt-3 text-sm">Funds may or may not have moved. No transaction ID or last lookup error was returned with this interrupted request. If a row exists, open it and use its transaction ID and last lookup error to determine the outcome. The service will not attempt the transfer again. Recording an outcome is a decision record, not an action. Check the withdrawal list for a row created in the last minute before doing anything else. The request may have reached payd, consumed the payd code, and created the row.</p><p className="mt-2 text-xs">Last error: <code className="font-mono">{submission.lastError}</code></p><Link href="/withdrawals" className="mt-3 inline-flex cursor-pointer text-severity-critical underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">Check withdrawal list</Link></section>;
  return <section className="card border-severity-warning text-severity-warning" role="alert"><h2 className="card-title text-severity-warning">{submission.title}</h2><p className="mt-2 text-sm">{submission.detail}</p>{submission.allowNew ? <button type="button" onClick={onNew} className="btn btn-secondary mt-3"><Plus aria-hidden="true" size={14} strokeWidth={1.75} />Create a new withdrawal</button> : null}</section>;
}
