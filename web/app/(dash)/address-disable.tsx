"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";
import { useState } from "react";

import { Amount } from "@/components/data/amount";
import { ConfirmDialog } from "@/components/forms/confirm-dialog";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { WalletDetail, WalletDisableResponse } from "@/lib/payd/types";
import { queryKeys } from "@/lib/query-keys";

export function AddressDisable({ wallet }: Readonly<{ wallet: WalletDetail }>) {
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const disable = useMutation({
    mutationFn: () => paydRequest<WalletDisableResponse>(["wallets", wallet.address, "disable"], { method: "POST" }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: queryKeys.wallets.all });
      void client.invalidateQueries({ queryKey: queryKeys.stats() });
    },
  });
  const order = wallet.assigned_order_id;
  const error = isPaydError(disable.error) ? disable.error : null;

  return <section className="border border-border-subtle bg-panel p-4"><h2 className="font-semibold">Address action</h2><div className="mt-3"><button type="button" className="border border-severity-warning px-3 py-2 text-sm disabled:cursor-not-allowed disabled:opacity-50" onClick={() => { disable.reset(); setOpen(true); }}>Disable address</button></div>{disable.error ? <p role="alert" className="mt-3 text-sm text-severity-warning">payd did not apply the action. Error code: <code className="select-all font-mono">{error?.code ?? "upstream_unreachable"}</code></p> : null}
    <ConfirmDialog open={open} onClose={() => setOpen(false)} title="Disable address" confirmLabel={`Disable ${wallet.address}`} onConfirm={async () => { try { await disable.mutateAsync(); setOpen(false); } catch (cause) { if (!isPaydError(cause) || cause.status >= 500) return { outcomeUnknown: true }; } }} error={error} apiText={<><p>Address <code className="select-all font-mono">{wallet.address}</code></p><p>Disabling permanently removes this address from rotation. Its history is retained and no funds are moved.</p><div className="inline-flex items-start gap-1 text-severity-warning"><AlertTriangle aria-hidden="true" size={15} className="mt-0.5 shrink-0" /><div><p>Funds stay at this address. Withdraw each balance explicitly.</p><ul className="mt-2 space-y-1">{wallet.balances.map((balance) => <li key={balance.asset} className="font-mono tabular-nums"><span className="text-ink-secondary">{balance.asset} confirmed: </span><Amount value={balance.confirmed} asset={balance.asset} variant="compact" /><span className="ml-3 text-ink-secondary">pending: </span><Amount value={balance.pending} asset={balance.asset} variant="compact" /></li>)}</ul></div></div>{order ? <p className="inline-flex items-start gap-1 text-severity-warning"><AlertTriangle aria-hidden="true" size={15} className="mt-0.5 shrink-0" />Assigned order <code className="select-all font-mono">{order}</code> is unaffected; the customer may still pay to this address.</p> : null}</>} />
  </section>;
}
