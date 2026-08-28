"use client";

import { useMutation } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { DataTable } from "@/components/data/data-table";
import { EmptyState } from "@/components/data/empty-state";
import { StatusBadge } from "@/components/data/status-badge";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { IpnConsumerPage, IpnTestResponse } from "@/lib/payd/types";

export function WebhookConsumers({ consumers }: Readonly<{ consumers: IpnConsumerPage["consumers"] }>) {
  const previous = useRef(new Map<string, number>());
  const [testing, setTesting] = useState("");
  const test = useMutation({
    mutationFn: (consumer: string) => paydRequest<IpnTestResponse>(["ipn", "test"], { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ consumer }) }),
  });
  const rising = new Set(consumers.flatMap(({ name, pending: queued }) => (previous.current.get(name) ?? queued) < queued ? [name] : []));
  useEffect(() => { consumers.forEach((consumer) => previous.current.set(consumer.name, consumer.pending)); }, [consumers]);
  const error = isPaydError(test.error) ? test.error : null;
  const testDetails = error?.details as { status_code?: number; latency_ms?: number } | undefined;

  return <section className="space-y-3"><div><h2 className="text-lg font-semibold">Consumers</h2><p className="mt-1 text-sm text-ink-secondary">Configured consumers are read-only here. A test sends one signed <code className="font-mono">test.ping</code> directly with the production signature implementation; it writes no outbox row, is not a business event, and never appears in a queue.</p></div>
    <DataTable columns={[{ id: "name", label: "Consumer" }, { id: "enabled", label: "State" }, { id: "global", label: "Global events" }, { id: "pending", label: "Pending" }, { id: "dead", label: "Dead" }, { id: "test", label: "Connectivity" }]} rows={consumers} rowKey={(consumer) => consumer.name} defaultSort="Backend consumer name order" caption="Webhook consumers" renderRow={(consumer) => <><td className="px-3 py-2 font-mono">{consumer.name}</td><td className="px-3 py-2"><StatusBadge status={consumer.enabled ? "enabled" : "disabled"} />{!consumer.enabled ? <p className="mt-1 text-xs text-ink-secondary">Pending rows stay queued and resume when configuration re-enables this consumer.</p> : null}</td><td className="px-3 py-2 font-mono">{consumer.receives_global ? "yes" : "no"}</td><td className="px-3 py-2 font-mono tabular-nums">{consumer.pending}{rising.has(consumer.name) ? <span className="ml-2 inline-flex items-center gap-1 text-xs text-severity-warning"><AlertTriangle aria-hidden="true" size={13} />Rising queue — this consumer may be slow or down.</span> : null}</td><td className="px-3 py-2 font-mono tabular-nums">{consumer.dead}</td><td className="px-3 py-2"><button type="button" className="border border-border-strong px-2 py-1 text-xs hover:bg-raised disabled:opacity-50" disabled={test.isPending} onClick={() => { test.reset(); setTesting(consumer.name); test.mutate(consumer.name); }}>{test.isPending && testing === consumer.name ? "Testing…" : "Send test ping"}</button></td></>} emptyState={<EmptyState kind="search" title="No consumers configured" description="Configure one in YAML and restart payd; it will then appear here." />} />
    {testing && test.data ? <p role="status" className="border border-severity-success bg-[var(--severity-success-bg)] p-3 text-sm">Test ping to <code className="font-mono">{testing}</code>: status <code className="font-mono">{test.data.status_code}</code>, <code className="font-mono">{test.data.latency_ms} ms</code>.</p> : null}
    {testing && error ? <p role="alert" className="border border-severity-warning bg-[var(--severity-warning-bg)] p-3 text-sm"><AlertTriangle aria-hidden="true" className="mr-1 inline" size={15} />Test ping to <code className="font-mono">{testing}</code> failed: HTTP status <code className="select-all font-mono">{error.status}</code>; error <code className="select-all font-mono">{error.code}</code>{testDetails?.status_code != null ? <>; delivery status <code className="font-mono">{testDetails.status_code}</code></> : null}{testDetails?.latency_ms != null ? <>; latency <code className="font-mono">{testDetails.latency_ms} ms</code></> : null}. {error.details && Object.keys(error.details).length ? <pre className="mt-2 overflow-auto border-t border-border-subtle pt-2 text-xs">{JSON.stringify(error.details, null, 2)}</pre> : null}</p> : null}
  </section>;
}
