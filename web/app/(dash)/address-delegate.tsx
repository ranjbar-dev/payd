"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";

import { Amount } from "@/components/data/amount";
import { ConfirmDialog } from "@/components/forms/confirm-dialog";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { ResourceGrant, ResourceWalletResponse } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

type ResourceType = "ENERGY" | "BANDWIDTH";
type ResourceWallet = ResourceWalletResponse & { energy: { available: number }; bandwidth: { available: number } };
type Result = { kind: "grant"; grant: ResourceGrant } | { kind: "ambiguous" } | { kind: "error"; code: string };

function ambiguous(error: unknown): boolean {
  return !isPaydError(error) || (error.status >= 500 && error.status !== 503);
}

export function AddressDelegate({ address }: Readonly<{ address: string }>) {
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [resourceType, setResourceType] = useState<ResourceType | "">("");
  const [resourceAmount, setResourceAmount] = useState("");
  const [result, setResult] = useState<Result | null>(null);
  const wallet = useQuery({ ...paydQueryOptions({ queryKey: queryKeys.resources.wallets(), queryFn: () => paydRequest<ResourceWallet>(["resources", "wallet"]), polling: { tier: "D" } }), enabled: open, refetchOnMount: "always" });
  const mutation = useMutation({
    mutationFn: (totp: string) => paydRequest<ResourceGrant>(["wallets", address, "delegate"], { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ resource_type: resourceType, amount: JSON.parse(resourceAmount), totp }) }),
    onSuccess: (grant) => {
      void client.invalidateQueries({ queryKey: queryKeys.resources.grants() });
      void client.invalidateQueries({ queryKey: queryKeys.resources.wallets() });
      void client.invalidateQueries({ queryKey: queryKeys.wallets.all });
      setResult({ kind: "grant", grant });
      setOpen(false);
    },
  });
  const error = isPaydError(mutation.error) ? mutation.error : null;
  const ready = Boolean(resourceType && /^(?:[1-9]\d*)$/.test(resourceAmount) && wallet.isSuccess);
  const show = () => { mutation.reset(); setResourceType(""); setResourceAmount(""); setOpen(true); };

  if (result?.kind === "grant") return <Link href={`/resources?grant=${encodeURIComponent(result.grant.id)}`} className="text-severity-progress underline underline-offset-2">Open recorded grant <code className="font-mono">{result.grant.id}</code></Link>;
  if (result?.kind === "ambiguous") return <p role="alert" className="text-sm text-severity-critical">The broadcast outcome is unknown. Check the <Link href="/resources#grants" className="underline underline-offset-2">resource grants list</Link> for the recorded grant and its on-chain resolution.</p>;
  if (result?.kind === "error") return <p role="alert" className="text-sm text-severity-warning">payd ended this delegation request with <code className="font-mono">{result.code}</code>. Inspect the <Link href="/resources#grants" className="underline underline-offset-2">resource grants list</Link> before any later action.</p>;

  return <>
    <button type="button" className="border border-severity-critical bg-[var(--severity-critical-bg)] px-3 py-2 text-sm font-medium text-severity-critical focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2" onClick={show}>Delegate resources</button>
    <ConfirmDialog open={open} onClose={() => !mutation.isPending && setOpen(false)} title="Delegate resources" confirmLabel="Broadcast delegation" requiresTotp ready={ready} error={error} onConfirm={async (totp) => {
      try { await mutation.mutateAsync(totp); } catch (cause) {
        if (ambiguous(cause)) { setResult({ kind: "ambiguous" }); return { outcomeUnknown: true }; }
        setResult({ kind: "error", code: isPaydError(cause) ? cause.code : "upstream_unreachable" });
        setOpen(false);
      }
    }} apiText={<>
      <p className="font-medium text-severity-critical">The delegation broadcast is attempted exactly once and is never retried.</p>
      <p>This is a fund-moving action and requires <code className="font-mono">resources:write</code>.</p>
      <section className="mt-3 border border-border-subtle p-3"><p className="font-medium">Resource wallet before delegation</p>{wallet.data ? <dl className="mt-2 grid gap-2 text-sm sm:grid-cols-3"><div><dt className="text-ink-faint">Available energy</dt><dd className="font-mono tabular-nums">{wallet.data.energy.available}</dd></div><div><dt className="text-ink-faint">Available bandwidth</dt><dd className="font-mono tabular-nums">{wallet.data.bandwidth.available}</dd></div><div><dt className="text-ink-faint">Confirmed TRX</dt><dd><Amount value={wallet.data.trx_balance} asset="TRX" /></dd></div></dl> : wallet.isError ? <p role="alert" className="mt-2 text-severity-warning">Resource wallet is unavailable; delegation cannot be submitted.</p> : <p className="mt-2 text-ink-secondary">Loading current resource wallet.</p>}</section>
      <fieldset className="mt-4 grid gap-2"><legend className="text-sm font-medium">Resource type</legend><label className="flex items-center gap-2 text-sm"><input type="radio" name={`resource-${address}`} checked={resourceType === "ENERGY"} onChange={() => setResourceType("ENERGY")} />ENERGY</label><label className="flex items-center gap-2 text-sm"><input type="radio" name={`resource-${address}`} checked={resourceType === "BANDWIDTH"} onChange={() => setResourceType("BANDWIDTH")} />BANDWIDTH</label></fieldset>
      <label className="mt-4 grid gap-1 text-sm">Requested resource units<input required value={resourceAmount} onChange={(event) => setResourceAmount(event.currentTarget.value)} inputMode="numeric" pattern="[0-9]*" className="border border-border-strong bg-panel px-3 py-2 font-mono tabular-nums" /></label>
      <p className="mt-4 text-sm text-ink-secondary">Resource wallet stake is not managed here: the service never stakes or unstakes automatically, and unstaking has a 14-day period.</p>
    </>} />
  </>;
}
