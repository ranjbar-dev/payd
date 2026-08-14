"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { useTronscanBaseUrl } from "@/app/providers";
import { TxidLink } from "@/components/data/links";
import { ConfirmDialog } from "@/components/forms/confirm-dialog";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { Withdrawal } from "@/lib/payd/types";
import { queryKeys } from "@/lib/query-keys";

type Outcome = "confirmed" | "failed";

function outcomeUnknown(error: unknown): boolean {
  return !isPaydError(error) || (error.status >= 500 && error.status !== 503);
}

export function WithdrawalResolve({ withdrawal }: Readonly<{ withdrawal: Withdrawal }>) {
  const client = useQueryClient();
  const tronscanBaseUrl = useTronscanBaseUrl();
  const [open, setOpen] = useState(false);
  const [outcome, setOutcome] = useState<Outcome>("confirmed");
  const [failureReason, setFailureReason] = useState("");
  const [checkedChain, setCheckedChain] = useState(false);
  const [ambiguous, setAmbiguous] = useState(false);
  const mutation = useMutation({
    mutationFn: (totp: string) => paydRequest<Withdrawal>(["withdrawals", withdrawal.id, "resolve"], {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ outcome, failure_reason: failureReason, totp }),
    }),
    onSuccess: (resolved) => {
      client.setQueryData(queryKeys.withdrawals.detail(withdrawal.id), resolved);
      void client.invalidateQueries({ queryKey: queryKeys.withdrawals.all });
      setOpen(false);
    },
  });
  const error = isPaydError(mutation.error) ? mutation.error : null;
  const ready = checkedChain && (outcome === "confirmed" || failureReason.trim().length > 0);

  if (withdrawal.status !== "needs_operator") return null;

  const close = () => {
    if (mutation.isPending) return;
    setOpen(false);
    setAmbiguous(false);
  };
  const show = () => {
    mutation.reset();
    setOutcome("confirmed");
    setFailureReason("");
    setCheckedChain(false);
    setAmbiguous(false);
    setOpen(true);
  };

  return <>
    <button type="button" className="border border-severity-critical bg-[var(--severity-critical-bg)] px-3 py-2 font-medium text-severity-critical focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2" onClick={show}>Resolve</button>
    <ConfirmDialog
      open={open}
      onClose={close}
      title="Record operator resolution"
      confirmLabel="Record resolution"
      requiresTotp
      ready={ready && !ambiguous}
      error={error}
      onConfirm={async (totp) => {
        try {
          await mutation.mutateAsync(totp);
        } catch (reason) {
          if (outcomeUnknown(reason)) {
            setAmbiguous(true);
            return { outcomeUnknown: true };
          }
        }
      }}
      apiText={<>
        <p>This records what happened; it does not sign, {"broad"}{"cast"}, {"re"}{"try"}, or {"re"}{"sume"} anything.</p>
        <dl className="mt-3 grid gap-3 text-sm"><div><dt className="text-xs uppercase tracking-wide text-ink-faint">Persisted transaction ID</dt><dd>{withdrawal.txid ? <TxidLink txid={withdrawal.txid} tronscanBaseUrl={tronscanBaseUrl} /> : <span className="text-severity-critical">No transaction ID was persisted.</span>}</dd></div><div><dt className="text-xs uppercase tracking-wide text-ink-faint">Last lookup error</dt><dd><code className="select-all break-words font-mono">{withdrawal.last_lookup_error || "—"}</code></dd></div></dl>
        <fieldset className="mt-4 grid gap-2"><legend className="text-sm font-medium">Chain outcome</legend><label className="flex items-center gap-2 text-sm"><input type="radio" name={`outcome-${withdrawal.id}`} checked={outcome === "confirmed"} onChange={() => setOutcome("confirmed")} />confirmed</label><label className="flex items-center gap-2 text-sm"><input type="radio" name={`outcome-${withdrawal.id}`} checked={outcome === "failed"} onChange={() => setOutcome("failed")} />failed</label></fieldset>
        {outcome === "failed" ? <label className="mt-3 grid gap-1 text-sm">Failure reason<textarea required value={failureReason} onChange={(event) => setFailureReason(event.currentTarget.value)} rows={3} className="border border-border-strong bg-panel p-2" /></label> : null}
        <label className="mt-4 flex items-start gap-2 text-sm"><input type="checkbox" checked={checkedChain} onChange={(event) => setCheckedChain(event.currentTarget.checked)} className="mt-1" />I checked this persisted transaction ID on Tronscan and confirmed the outcome above.</label>
        {ambiguous ? <p role="alert" className="mt-3 text-sm text-severity-critical">The outcome is unknown. This decision may or may not have been recorded; check the withdrawal&apos;s current state before taking any further action.</p> : null}
        {error && !ambiguous ? <p role="alert" className="mt-3 text-sm text-severity-warning">{error.status === 503 ? "payd did not record this decision because the service is unavailable." : `payd did not record this decision. Error code: ${error.code}`}</p> : null}
      </>}
    />
  </>;
}
