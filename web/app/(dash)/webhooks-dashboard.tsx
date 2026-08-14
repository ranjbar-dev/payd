"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

import { TableFilters } from "@/components/data/data-table";
import { ErrorState } from "@/components/data/error-state";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { DeadIpnPage, IpnConsumerPage } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

import { WebhookConsumers } from "./webhook-consumers";
import { WebhookDeadLetters } from "./webhook-dead-letters";
import { WebhookEventReference } from "./webhook-event-reference";
import { WebhookReplay } from "./webhook-replay";

const LIST_INTERVAL = 30_000;
const copyByCode = { unauthorized: "This dashboard session or its upstream scope is not authorised.", rate_limited: "Refresh has slowed because payd is rate limited.", upstream_unreachable: "payd could not be reached; showing the last available data when present.", upstream_timeout: "payd did not answer in time; showing the last available data when present." };

function QueryError({ error, updatedAt, onRetry }: Readonly<{ error: unknown; updatedAt: number; onRetry: () => void }>) {
  if (!error) return null;
  const payd = isPaydError(error) ? error : null;
  return <ErrorState error={{ code: payd?.code ?? "upstream_unreachable", details: payd?.details }} copyByCode={copyByCode} lastUpdatedAt={updatedAt || undefined} pollingIntervalMs={LIST_INTERVAL} onRetry={onRetry} />;
}

export function WebhooksDashboard() {
  const client = useQueryClient();
  const router = useRouter();
  const pathname = usePathname();
  const search = useSearchParams();
  const consumer = search.get("consumer") ?? "";
  const deadCursor = search.get("cursor") ?? "";
  const consumers = useQuery(paydQueryOptions({ queryKey: queryKeys.ipn.consumers({ limit: 50 }), queryFn: () => paydRequest<IpnConsumerPage>(["ipn", "consumers"], {}, new URLSearchParams({ limit: "50" })), polling: { tier: "B" } }));
  const deadQuery = new URLSearchParams({ limit: "50" });
  if (consumer) deadQuery.set("consumer", consumer);
  if (deadCursor) deadQuery.set("cursor", deadCursor);
  const dead = useQuery(paydQueryOptions({ queryKey: queryKeys.ipn.dead(Object.fromEntries(deadQuery)), queryFn: () => paydRequest<DeadIpnPage>(["ipn", "dead"], {}, deadQuery), polling: { tier: "B" } }));
  const setParams = (next: Record<string, string>) => { const value = new URLSearchParams(search); Object.entries(next).forEach(([key, item]) => item ? value.set(key, item) : value.delete(key)); if (!("cursor" in next)) value.delete("cursor"); router.replace(`${pathname}${value.size ? `?${value}` : ""}`); };

  return <main className="mx-auto max-w-7xl space-y-6 p-4 lg:p-6"><header><p className="font-mono text-xs uppercase tracking-[0.2em] text-ink-faint">Operations / Webhooks</p><h1 className="mt-1 text-2xl font-semibold">Webhooks / IPN</h1><p className="mt-1 text-sm text-ink-secondary">Consumer configuration is supplied by payd. This page observes delivery and provides the narrow, idempotent notification-redelivery exception.</p></header>
    <WebhookConsumers consumers={consumers.data?.consumers ?? []} />
    <QueryError error={consumers.isError ? consumers.error : null} updatedAt={consumers.dataUpdatedAt} onRetry={() => void consumers.refetch()} />
    <section className="space-y-3"><TableFilters active={Boolean(consumer)} onClear={() => setParams({ consumer: "" })}><label className="grid gap-1 text-xs text-ink-secondary">Dead-letter consumer<select value={consumer} onChange={(event) => setParams({ consumer: event.currentTarget.value })} className="border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink"><option value="">All consumers</option>{(consumers.data?.consumers ?? []).map((item) => <option key={item.name} value={item.name}>{item.name}</option>)}</select></label></TableFilters><WebhookDeadLetters page={dead.data} consumer={consumer} onCursor={(cursor) => setParams({ cursor })} onRetrySuccess={async () => { await Promise.all([dead.refetch(), client.invalidateQueries({ queryKey: queryKeys.stats() })]); }} /></section>
    <QueryError error={dead.isError ? dead.error : null} updatedAt={dead.dataUpdatedAt} onRetry={() => void dead.refetch()} />
    <WebhookReplay consumers={consumers.data?.consumers ?? []} />
    <WebhookEventReference />
  </main>;
}
