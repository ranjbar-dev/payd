"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertOctagon } from "lucide-react";

import { useTronscanBaseUrl } from "@/app/providers";
import { Amount } from "@/components/data/amount";
import { EmptyState } from "@/components/data/empty-state";
import { ErrorState } from "@/components/data/error-state";
import { TxidLink } from "@/components/data/links";
import { RefreshButton } from "@/components/data/refresh-button";
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

  return <tr className="row-hover"><td className="td">{withdrawal.txid ? <TxidLink txid={withdrawal.txid} tronscanBaseUrl={tronscanBaseUrl} /> : <span className="font-mono text-ink-faint">—</span>}</td><td className="td"><code className="line-clamp-2 block select-all break-words font-mono text-[11px] text-ink-secondary" title={withdrawal.last_lookup_error || "—"}>{withdrawal.last_lookup_error || "—"}</code></td><td className="td text-right font-mono tabular-nums"><Amount value={withdrawal.amount} asset={withdrawal.asset} /></td><td className="td text-right"><WithdrawalResolve withdrawal={withdrawal} /></td></tr>;
}

function TableSkeleton() {
  return <div className="overflow-x-auto"><table className="w-full border-collapse text-[13px]" aria-label="Loading withdrawals requiring an operator decision"><thead><tr><th className="th text-left">Persisted transaction ID</th><th className="th text-left">Last lookup error</th><th className="th text-right">Amount</th><th className="th text-right">Decision</th></tr></thead><tbody className="animate-pulse">{[0, 1, 2, 3].map((row) => <tr key={row}><td className="td"><div className="h-4 w-40 bg-raised" /></td><td className="td"><div className="h-4 w-full max-w-md bg-raised" /></td><td className="td"><div className="ml-auto h-4 w-24 bg-raised" /></td><td className="td"><div className="ml-auto h-8 w-20 bg-raised" /></td></tr>)}</tbody></table></div>;
}

function WorklistTable({ rows }: Readonly<{ rows: Withdrawal[] }>) {
  return <div className="overflow-x-auto"><table className="w-full border-collapse text-[13px]"><caption className="sr-only">Withdrawals requiring an operator decision, oldest first by created time</caption><thead><tr><th className="th text-left">Persisted transaction ID</th><th className="th text-left">Last lookup error</th><th className="th text-right">Amount</th><th className="th text-right">Decision</th></tr></thead><tbody>{rows.map((withdrawal) => <Row key={withdrawal.id} withdrawal={withdrawal} />)}</tbody></table></div>;
}

function Cards({ rows }: Readonly<{ rows: Withdrawal[] }>) {
  const tronscanBaseUrl = useTronscanBaseUrl();
  return <div className="grid gap-3 lg:hidden">{rows.map((withdrawal) => <article key={withdrawal.id} className="needs-operator border-2 border-severity-critical p-4"><dl className="grid gap-3 text-[13px]"><div><dt className="text-[11px] uppercase text-ink-faint">Persisted transaction ID</dt><dd className="mt-1">{withdrawal.txid ? <TxidLink txid={withdrawal.txid} tronscanBaseUrl={tronscanBaseUrl} /> : <span className="font-mono text-ink-faint">—</span>}</dd></div><div><dt className="text-[11px] uppercase text-ink-faint">Last lookup error</dt><dd className="mt-1"><code className="line-clamp-2 block select-all break-words font-mono text-[11px] text-ink-secondary" title={withdrawal.last_lookup_error || "—"}>{withdrawal.last_lookup_error || "—"}</code></dd></div><div><dt className="text-[11px] uppercase text-ink-faint">Amount</dt><dd className="mt-1 text-right font-mono tabular-nums"><Amount value={withdrawal.amount} asset={withdrawal.asset} /></dd></div></dl><div className="mt-4 flex justify-end"><WithdrawalResolve withdrawal={withdrawal} /></div></article>)}</div>;
}

export function WithdrawalNeedsOperator() {
  const query = new URLSearchParams({ status: "needs_operator", limit: "200" });
  const worklist = useQuery(paydQueryOptions({ queryKey: queryKeys.withdrawals.needsOperator(), queryFn: () => paydRequest<WithdrawalList>(["withdrawals"], {}, query), polling: { tier: "B" } }));
  const rows = [...(worklist.data?.items ?? [])].sort((left, right) => left.created_at - right.created_at);
  const error = isPaydError(worklist.error) ? worklist.error : null;

  return <main className="page"><header className="needs-operator border-2 border-severity-critical p-4" aria-labelledby="needs-operator-title"><p className="page-kicker text-severity-critical"><AlertOctagon aria-hidden="true" size={14} strokeWidth={1.75} />Withdrawals / critical alarm</p><h1 id="needs-operator-title" className="page-title mt-1 text-[var(--severity-critical-salience)]">needs_operator worklist</h1><p className="mt-2 max-w-3xl text-[13px] text-[var(--severity-critical-salience)]">Each row is money in an unknown state. The service will attempt nothing further. Resolution is a human decision recorded after checking the chain.</p><div className="mt-3"><RefreshButton /></div></header><section className="card border-2 border-severity-critical" aria-labelledby="operator-decisions-title"><div className="mb-3 flex items-center justify-between gap-3"><h2 id="operator-decisions-title" className="card-title">Operator decisions</h2><span className="font-mono text-[11px] tabular-nums text-severity-critical" data-count={rows.length}>{rows.length} open</span></div><div className="hidden lg:block">{worklist.isLoading ? <TableSkeleton /> : rows.length ? <WorklistTable rows={rows} /> : !error ? <EmptyState kind="worklist" title="No money is in an unknown state" description="There are no withdrawals requiring an operator decision." icon={<AlertOctagon aria-hidden="true" size={20} strokeWidth={1.75} />} /> : null}</div>{!worklist.isLoading && rows.length ? <Cards rows={rows} /> : null}{worklist.isLoading ? <div className="grid gap-3 lg:hidden" aria-label="Loading withdrawals requiring an operator decision">{[0, 1, 2].map((row) => <div key={row} className="h-48 animate-pulse bg-raised" />)}</div> : null}{!worklist.isLoading && !rows.length && !error ? <div className="lg:hidden"><EmptyState kind="worklist" title="No money is in an unknown state" description="There are no withdrawals requiring an operator decision." icon={<AlertOctagon aria-hidden="true" size={20} strokeWidth={1.75} />} /></div> : null}</section>{error ? <ErrorState error={error} copyByCode={copyByCode} lastUpdatedAt={worklist.dataUpdatedAt || undefined} pollingIntervalMs={LIST_INTERVAL} onRetry={() => void worklist.refetch()} /> : null}</main>;
}
