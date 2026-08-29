"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, RotateCcw, Send } from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useState } from "react";

import { ConfirmDialog } from "@/components/forms/confirm-dialog";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { IpnReplayResponse } from "@/lib/payd/types";
import { queryKeys } from "@/lib/query-keys";

const CALL_LIMIT = 200;

function unix(value: string) { return value ? Math.floor(new Date(value).getTime() / 1000) : undefined; }
function utc(value: string) { const seconds = unix(value); return seconds == null ? "unbounded" : new Date(seconds * 1000).toISOString().replace("T", " ").replace(".000Z", " UTC"); }

export function WebhookReplay({ consumers }: Readonly<{ consumers: readonly { name: string }[] }>) {
  const router = useRouter();
  const pathname = usePathname();
  const search = useSearchParams();
  const client = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const consumer = search.get("replay_consumer") ?? "";
  const from = search.get("replay_from") ?? "";
  const to = search.get("replay_to") ?? "";
  const dryRun = search.get("replay_dry_run") !== "false";
  const signature = JSON.stringify({ consumer, from, to });
  const [checked, setChecked] = useState<{ signature: string; count: number } | null>(null);
  const replay = useMutation({
    mutationFn: (live: boolean) => paydRequest<IpnReplayResponse>(["ipn", "replay"], { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ ...(consumer ? { consumer } : {}), ...(unix(from) == null ? {} : { from: unix(from) }), ...(unix(to) == null ? {} : { to: unix(to) }), dry_run: !live }) }),
    onSuccess: async (_result, live) => { if (live) await Promise.all([client.invalidateQueries({ queryKey: queryKeys.ipn.dead() }), client.invalidateQueries({ queryKey: queryKeys.stats() })]); },
  });
  const set = (key: string, value: string) => { const next = new URLSearchParams(search); value ? next.set(key, value) : next.delete(key); router.replace(`${pathname}${next.size ? `?${next}` : ""}`); };
  const canReplay = !dryRun && checked?.signature === signature && checked.count > 0;
  const calls = checked ? Math.ceil(checked.count / CALL_LIMIT) : 0;
  const error = isPaydError(replay.error) ? replay.error : null;

  return <section className="card space-y-3" aria-labelledby="webhook-replay-heading"><div><h2 id="webhook-replay-heading" className="card-title">Bulk replay</h2><p className="mt-1 text-[13px] text-ink-secondary">Recovery is capped at {CALL_LIMIT} events per call. This page never loops: every additional replay is a separate operator action.</p></div>
    <p className="border-l-2 border-severity-progress bg-inset p-3 text-[13px]"><strong>IPN redelivery is safe because consumers treat <code className="font-mono">event_id</code> as an idempotency key. This is the only retry in the system.</strong> Replay sends notifications again; it never changes a withdrawal or any underlying payment state.</p>
    <div className="grid gap-3 md:grid-cols-4"><label className="field">Consumer filter<select value={consumer} onChange={(event) => set("replay_consumer", event.currentTarget.value)} className="input cursor-pointer transition-colors duration-150 hover:border-ink-faint focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]"><option value="">All consumers</option>{consumers.map((item) => <option key={item.name} value={item.name}>{item.name}</option>)}</select></label><label className="field">From (local time)<input type="datetime-local" value={from} onChange={(event) => set("replay_from", event.currentTarget.value)} className="input cursor-pointer transition-colors duration-150 hover:border-ink-faint focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]" /></label><label className="field">To (local time)<input type="datetime-local" value={to} onChange={(event) => set("replay_to", event.currentTarget.value)} className="input cursor-pointer transition-colors duration-150 hover:border-ink-faint focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]" /></label><label className="flex cursor-pointer items-end gap-2 pb-2 text-[13px] text-ink-secondary transition-colors duration-150 hover:text-ink focus-within:outline-2 focus-within:outline-[var(--focus-ring)]"><input type="checkbox" checked={dryRun} onChange={(event) => set("replay_dry_run", event.currentTarget.checked ? "" : "false")} />Dry run (default)</label></div>
    <p className="text-[11px] text-ink-secondary">Resolved range (UTC, inclusive): <code className="font-mono">{utc(from)}</code> to <code className="font-mono">{utc(to)}</code>.</p>
    <div className="flex flex-wrap gap-2"><button type="button" className="btn btn-secondary" disabled={replay.isPending} onClick={() => { replay.reset(); replay.mutate(false, { onSuccess: (result) => setChecked({ signature, count: result.count }) }); }}><Send aria-hidden="true" size={14} strokeWidth={1.75} />Count matching notifications</button>{!dryRun ? <button type="button" className="btn btn-secondary" disabled={!canReplay || replay.isPending} onClick={() => { replay.reset(); setConfirming(true); }}><RotateCcw aria-hidden="true" size={14} strokeWidth={1.75} />Replay acknowledged range</button> : null}</div>
    {checked?.signature === signature ? <p role="status" className="mt-3 border border-severity-success bg-[var(--severity-success-bg)] p-3 text-sm">Dry run found <code className="font-mono">{checked.count}</code> matching dead notification{checked.count === 1 ? "" : "s"}. This request can process at most <code className="font-mono">{CALL_LIMIT}</code>; {calls} explicit call{calls === 1 ? " is" : "s are"} needed for this counted batch. If a broader range has more than {CALL_LIMIT}, narrow the range and start each further call yourself.</p> : null}
    {replay.data && !dryRun ? <p role="status" className="mt-3 border border-severity-success bg-[var(--severity-success-bg)] p-3 text-sm">Requeued <code className="font-mono">{replay.data.count}</code> notification{replay.data.count === 1 ? "" : "s"}. Dispatch remains asynchronous.</p> : null}
    {error ? <p role="alert" className="mt-3 border border-severity-warning bg-[var(--severity-warning-bg)] p-3 text-sm"><AlertTriangle aria-hidden="true" className="mr-1 inline" size={15} />Replay was not applied. Error code: <code className="select-all font-mono">{error.code}</code>{error.details && Object.keys(error.details).length ? <pre className="mt-2 overflow-auto text-xs">{JSON.stringify(error.details, null, 2)}</pre> : null}</p> : null}
    <ConfirmDialog open={confirming} onClose={() => setConfirming(false)} title="Replay IPN notifications" confirmLabel={`Replay ${checked?.count ?? 0} notifications`} ready={canReplay} error={error} onConfirm={async () => { try { await replay.mutateAsync(true); setConfirming(false); } catch { /* Failed mutations remain manual operations. */ } }} apiText={<><p>Consumer: <code className="font-mono">{consumer || "all consumers"}</code></p><p>UTC range, inclusive: <code className="font-mono">{utc(from)}</code> to <code className="font-mono">{utc(to)}</code></p><p>Count from dry run: <code className="font-mono">{checked?.count ?? 0}</code></p><p className="mt-2">Consumers will receive these events again and must treat <code className="font-mono">event_id</code> as an idempotency key.</p></>} />
  </section>;
}
