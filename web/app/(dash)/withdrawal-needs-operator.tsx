"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";

import { useTronscanBaseUrl } from "@/app/providers";
import { Amount } from "@/components/data/amount";
import { DataTable } from "@/components/data/data-table";
import { EmptyState } from "@/components/data/empty-state";
import { ErrorState } from "@/components/data/error-state";
import { TxidLink } from "@/components/data/links";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { Withdrawal, WithdrawalList } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

import { WithdrawalResolve } from "./withdrawal-resolve";

const LIST_INTERVAL = 30_000;
const copyByCode: Record<string, string> = {
  unauthorized: "This dashboard session or its upstream scope is not authorised.",
  rate_limited: "Refresh has slowed because payd is rate limited.",
  upstream_unreachable: "payd could not be reached; the worklist could not refresh, but the last good rows remain visible.",
  upstream_timeout: "payd did not answer in time; the worklist could not refresh, but the last good rows remain visible.",
};

function Row({ withdrawal }: Readonly<{ withdrawal: Withdrawal }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  return <><td className="px-3 py-2"><code className="select-all break-all font-mono">{withdrawal.txid || "—"}</code>{withdrawal.txid ? <span className="ml-2"><TxidLink txid={withdrawal.txid} tronscanBaseUrl={tronscanBaseUrl} /></span> : null}</td><td className="px-3 py-2"><code className="select-all break-words font-mono text-xs">{withdrawal.last_lookup_error || "—"}</code></td><td className="px-3 py-2"><Amount value={withdrawal.amount} asset={withdrawal.asset} /></td><td className="px-3 py-2"><WithdrawalResolve withdrawal={withdrawal} /></td></>;
}

function Cards({ rows }: Readonly<{ rows: Withdrawal[] }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  return <div className="grid gap-2 lg:hidden">{rows.map((withdrawal) => <article key={withdrawal.id} className="border-2 border-severity-critical bg-panel p-3"><p className="text-xs uppercase tracking-wide text-ink-faint">Persisted transaction ID</p><code className="select-all break-all font-mono text-xs">{withdrawal.txid || "—"}</code>{withdrawal.txid ? <span className="ml-2"><TxidLink txid={withdrawal.txid} tronscanBaseUrl={tronscanBaseUrl} /></span> : null}<p className="mt-2 text-xs uppercase tracking-wide text-ink-faint">Last lookup error</p><code className="select-all break-words font-mono text-xs">{withdrawal.last_lookup_error || "—"}</code><p className="mt-2"><Amount value={withdrawal.amount} asset={withdrawal.asset} /></p><div className="mt-3"><WithdrawalResolve withdrawal={withdrawal} /></div></article>)}</div>;
}

export function WithdrawalNeedsOperator() {
  const query = new URLSearchParams({ status: "needs_operator", limit: "200" });
  const worklist = useQuery(paydQueryOptions({ queryKey: queryKeys.withdrawals.needsOperator(), queryFn: () => paydRequest<WithdrawalList>(["withdrawals"], {}, query), polling: { tier: "B" } }));
  const rows = [...(worklist.data?.items ?? [])].sort((left, right) => left.created_at - right.created_at);
  const error = isPaydError(worklist.error) ? worklist.error : null;

  return <main className="mx-auto max-w-7xl space-y-4 p-4 lg:p-6"><header className="border-2 border-severity-critical bg-[var(--severity-critical-bg)] p-4"><div className="flex items-center gap-2 text-severity-critical"><AlertTriangle aria-hidden="true" size={20} /><h1 className="text-2xl font-semibold">needs_operator worklist</h1></div><p className="mt-2 text-sm">Each row is money in an unknown state. The service will attempt nothing further. Resolution is a human decision recorded after checking the chain.</p></header><div className="hidden lg:block"><DataTable columns={[{ id: "txid", label: "Persisted transaction ID" }, { id: "error", label: "Last lookup error" }, { id: "amount", label: "Amount" }, { id: "action", label: "Action" }]} rows={rows} rowKey={(withdrawal) => withdrawal.id} defaultSort="Oldest first by created time" caption="Withdrawals requiring an operator decision" loading={worklist.isLoading} emptyState={<EmptyState kind="worklist" title="No money is in an unknown state" description="There are no withdrawals requiring an operator decision." />} renderRow={(withdrawal) => <Row withdrawal={withdrawal} />} /></div>{!worklist.isLoading && rows.length ? <Cards rows={rows} /> : null}{error ? <ErrorState error={error} copyByCode={copyByCode} lastUpdatedAt={worklist.dataUpdatedAt || undefined} pollingIntervalMs={LIST_INTERVAL} onRetry={() => void worklist.refetch()} /> : null}</main>;
}
