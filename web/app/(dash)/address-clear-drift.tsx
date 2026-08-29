"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Check, Loader2 } from "lucide-react";
import Link from "next/link";
import { useState } from "react";

import { useTronscanBaseUrl } from "@/app/providers";
import { ConfirmDialog } from "@/components/forms/confirm-dialog";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { ClearDriftResponse } from "@/lib/payd/types";
import { queryKeys } from "@/lib/query-keys";

type Result = { kind: "acknowledged" } | { kind: "unknown" } | { kind: "error"; code: string };

function ambiguous(error: unknown): boolean {
  return !isPaydError(error) || (error.status >= 500 && error.status !== 503);
}

export function AddressClearDrift({ address, asset, chainRaw }: Readonly<{ address: string; asset: string; chainRaw: string }>) {
  const client = useQueryClient();
  const tronscanBaseUrl = useTronscanBaseUrl();
  const [open, setOpen] = useState(false);
  const [acknowledged, setAcknowledged] = useState(false);
  const [result, setResult] = useState<Result | null>(null);
  const invalidate = () => {
    void client.invalidateQueries({ queryKey: queryKeys.wallets.all });
    client.removeQueries({ queryKey: queryKeys.withdrawals.estimate(address) });
  };
  const mutation = useMutation({
    mutationFn: (totp: string) => paydRequest<ClearDriftResponse>(["wallets", address, "clear-drift"], { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ asset, chain_raw: chainRaw, totp }) }),
    onSuccess: () => { invalidate(); setResult({ kind: "acknowledged" }); setOpen(false); },
  });
  const error = isPaydError(mutation.error) ? mutation.error : null;
  const tronscan = `${tronscanBaseUrl.replace(/\/$/, "")}/#/address/${encodeURIComponent(address)}`;
  const show = () => { mutation.reset(); setAcknowledged(false); setOpen(true); };

  if (result?.kind === "acknowledged") return <p className="text-sm text-severity-warning">Drift acknowledgement was recorded for <code className="font-mono">{asset}</code>. Refresh the address state before considering withdrawals.</p>;
  if (result?.kind === "unknown") return <p role="alert" className="text-sm text-severity-critical">The acknowledgement outcome is unknown. Inspect the address state before any later action.</p>;
  if (result?.kind === "error") return <p role="alert" className="text-sm text-severity-warning">payd ended this acknowledgement with <code className="font-mono">{result.code}</code>. Inspect the address state before any later action.</p>;

  return <>
    <button type="button" className="btn btn-danger mt-3" disabled={mutation.isPending} onClick={show}>{mutation.isPending ? <Loader2 aria-hidden="true" size={14} strokeWidth={1.75} className="animate-spin" /> : <Check aria-hidden="true" size={14} strokeWidth={1.75} />}{mutation.isPending ? "Recording…" : `Acknowledge drift for ${asset}`}</button>
    <ConfirmDialog open={open} onClose={() => !mutation.isPending && setOpen(false)} title={`Acknowledge ${asset} balance drift`} confirmLabel="Record acknowledgement" destructive requiresTotp ready={acknowledged} error={error} onConfirm={async (totp) => {
      try { await mutation.mutateAsync(totp); } catch (cause) {
        invalidate();
        if (ambiguous(cause)) { setResult({ kind: "unknown" }); return { outcomeUnknown: true }; }
        setResult({ kind: "error", code: isPaydError(cause) ? cause.code : "upstream_unreachable" });
        setOpen(false);
      }
    }} apiText={<>
      <p className="flex items-start gap-2 border border-severity-critical bg-[var(--severity-critical-bg)] p-3 font-medium text-severity-critical"><AlertTriangle aria-hidden="true" className="mt-0.5 shrink-0" size={16} />This records an acknowledgement and does NOT correct any balance. It re-enables withdrawals from this address without making the ledger right.</p>
      <p className="mt-3">Investigate before clearing: drift usually means a payment the detector missed or a transfer nothing recorded. Review the <Link href={`/payments?address=${encodeURIComponent(address)}`} className="cursor-pointer text-severity-progress underline underline-offset-2 transition-colors hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">payment history</Link> and <a href={tronscan} target="_blank" rel="noreferrer" className="cursor-pointer text-severity-progress underline underline-offset-2 transition-colors hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">Tronscan address</a>.</p>
      <p className="mt-3">Current <code className="font-mono">chain_raw</code> for <code className="font-mono">{asset}</code>: <code className="select-all font-mono">{chainRaw}</code></p>
      <label className="mt-4 flex cursor-pointer items-start gap-2 text-sm text-ink-secondary transition-colors hover:text-ink focus-within:text-ink"><input type="checkbox" checked={acknowledged} onChange={(event) => setAcknowledged(event.currentTarget.checked)} className="mt-1" />I acknowledge the exact current <code className="font-mono">chain_raw</code> value shown above: <code className="font-mono">{chainRaw}</code>.</label>
    </>} />
  </>;
}
