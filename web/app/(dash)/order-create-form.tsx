"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Loader2, Plus } from "lucide-react";
import Link from "next/link";
import { useState } from "react";

import { Amount } from "@/components/data/amount";
import { ErrorState } from "@/components/data/error-state";
import { isPaydError, paydRequest } from "@/lib/payd/browser-client";
import type { AssetsResponse, IpnConsumerPage, Order, OrderList } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

type CreateFailure = { status: number; code: string; details: Record<string, unknown> };

function csrfToken() { return document.cookie.split(";").map((item) => item.trim()).find((item) => item.startsWith("payd_csrf="))?.slice("payd_csrf=".length); }

async function createOrder(body: Record<string, unknown>): Promise<{ status: number; order: Order }> {
  const headers = new Headers({ "content-type": "application/json" });
  const token = csrfToken();
  if (token) headers.set("x-csrf-token", token);
  const response = await fetch("/api/payd/orders", { method: "POST", headers, body: JSON.stringify(body), credentials: "same-origin", cache: "no-store" });
  const value: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    const error = typeof value === "object" && value !== null && "error" in value ? (value as { error?: { code?: unknown; details?: unknown } }).error : null;
    throw { status: response.status, code: typeof error?.code === "string" ? error.code : "upstream_unreachable", details: error?.details && typeof error.details === "object" && !Array.isArray(error.details) ? error.details as Record<string, unknown> : {} } satisfies CreateFailure;
  }
  return { status: response.status, order: value as Order };
}

function precisionValid(value: string, decimals: number) {
  const parts = value.split(".");
  return /^(0|[1-9][0-9]*)(\.[0-9]+)?$/.test(value) && (parts[1] === undefined || parts[1].length <= decimals);
}

function CreateError({ error, requested, stored }: Readonly<{ error: unknown; requested: { asset: string; value: string; consumer: string; mode: "amount" | "amount_usd" }; stored: Order | null }>) {
  const failure = error as CreateFailure | null;
  if (!failure) return null;
  const linkClass = "cursor-pointer text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]";
  const fields = Array.isArray(failure.details.fields) ? failure.details.fields.filter((field): field is string => typeof field === "string") : [];
  if (failure.code === "external_ref_conflict") return <section className="border border-severity-warning bg-[var(--severity-warning-bg)] p-3" role="alert"><p className="font-medium">Creation did not succeed: this external reference belongs to a different order request.</p><p className="mt-1 text-sm">Error code: <code className="select-all font-mono">external_ref_conflict</code></p>{stored ? <><div className="mt-3 grid gap-2 text-sm sm:grid-cols-3"><strong>Field</strong><strong>Requested</strong><strong>Stored</strong>{fields.map((field) => <><span key={`${field}:field`} className="font-mono">{field}</span><span key={`${field}:requested`} className="font-mono">{field === "asset" ? requested.asset : field === "consumer" ? requested.consumer : requested.value}</span><span key={`${field}:stored`} className="font-mono">{field === "asset" ? stored.asset : field === "consumer" ? stored.consumer : stored.amount}</span></>)}</div><Link href={`/orders/${encodeURIComponent(stored.id)}`} className={`mt-3 inline-block ${linkClass}`}>Open the existing order</Link></> : <p className="mt-2 text-sm">The conflicting order could not be loaded. <Link href="/orders" className={linkClass}>Search orders by this external reference</Link>.</p>}</section>;
  if (failure.code === "address_pool_exhausted") return <p role="alert" className="border border-severity-warning bg-[var(--severity-warning-bg)] p-3">The address pool is at <code className="font-mono">wallet.pool_max_size</code> with no free address. <Link href="/addresses" className={linkClass}>Open addresses</Link>. Error code: <code className="select-all font-mono">address_pool_exhausted</code></p>;
  if (failure.code === "price_unavailable") return <p role="alert" className="border border-severity-warning bg-[var(--severity-warning-bg)] p-3">Creation was refused because the price is stale or unavailable. <Link href="/resources" className={linkClass}>Open the prices card</Link>. Error code: <code className="select-all font-mono">price_unavailable</code></p>;
  if (failure.code === "unknown_consumer") return <p role="alert" className="border border-severity-warning bg-[var(--severity-warning-bg)] p-3">The selected consumer <code className="font-mono">{requested.consumer || "(none)"}</code> is unknown or disabled. <Link href="/webhooks" className={linkClass}>Open Webhooks</Link>. Error code: <code className="select-all font-mono">unknown_consumer</code></p>;
  if (failure.code === "invalid_order") return <p role="alert" className="border border-severity-warning bg-[var(--severity-warning-bg)] p-3">The selected asset or amount is invalid. Check the asset and decimal amount, then submit a new request. Error code: <code className="select-all font-mono">invalid_order</code></p>;
  return <p role="alert" className="border border-severity-warning bg-[var(--severity-warning-bg)] p-3">payd did not create the order. Error code: <code className="select-all font-mono">{failure.code}</code>{Object.keys(failure.details).length ? <pre className="mt-2 overflow-auto text-xs">{JSON.stringify(failure.details, null, 2)}</pre> : null}</p>;
}

export function OrderCreateForm() {
  const client = useQueryClient();
  const assets = useQuery(paydQueryOptions({ queryKey: queryKeys.assets(), queryFn: () => paydRequest<AssetsResponse>(["assets"]), polling: { tier: "D" } }));
  const consumers = useQuery(paydQueryOptions({ queryKey: queryKeys.ipn.consumers({ limit: 200 }), queryFn: () => paydRequest<IpnConsumerPage>(["ipn", "consumers"], {}, new URLSearchParams({ limit: "200" })), polling: { tier: "D" } }));
  const [asset, setAsset] = useState("");
  const [mode, setMode] = useState<"amount" | "amount_usd">("amount");
  const [value, setValue] = useState("");
  const [externalRef, setExternalRef] = useState("");
  const [consumer, setConsumer] = useState("");
  const [ttl, setTtl] = useState(1800);
  const [metadata, setMetadata] = useState("{}");
  const [stored, setStored] = useState<Order | null>(null);
  const selected = assets.data?.assets.find((item) => item.symbol === asset);
  const validValue = selected ? precisionValid(value, selected.decimals) : false;
  const mutation = useMutation({ mutationFn: createOrder, onSuccess: ({ order }) => { client.setQueryData(queryKeys.orders.detail(order.id), order); void client.invalidateQueries({ queryKey: queryKeys.orders.all }); void client.invalidateQueries({ queryKey: queryKeys.stats() }); } });

  const submit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!selected || !validValue || !consumer) return;
    let parsed: unknown;
    try { parsed = JSON.parse(metadata); } catch { return; }
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) return;
    mutation.reset(); setStored(null);
    const body: Record<string, unknown> = { asset, external_ref: externalRef, consumer, ttl_seconds: ttl, metadata: { ...parsed as Record<string, unknown>, created_by: "dashboard" }, [mode]: value };
    try { await mutation.mutateAsync(body); } catch (error) { const failure = error as CreateFailure; if (failure.code === "external_ref_conflict" && externalRef) { const found = await paydRequest<OrderList>(["orders"], {}, new URLSearchParams({ external_ref: externalRef, limit: "50" })).catch(() => null); setStored(found?.orders[0] ?? null); } }
  };
  const metadataValid = (() => { try { const parsed: unknown = JSON.parse(metadata); return typeof parsed === "object" && parsed !== null && !Array.isArray(parsed); } catch { return false; } })();
  const requested = { asset, value, consumer, mode };

  return <main className="page max-w-3xl">
    <header>
      <p className="page-kicker"><Plus aria-hidden="true" size={14} strokeWidth={1.75} />Operations / Orders / New</p>
      <h1 className="page-title mt-1">Create order</h1>
      <p className="mt-2 flex items-start gap-1.5 text-[13px] text-severity-warning"><AlertTriangle aria-hidden="true" className="mt-0.5 shrink-0" size={14} strokeWidth={1.75} />A dashboard-created order has no consumer service expecting its IPNs. Select the consumer deliberately.</p>
    </header>

    {assets.isLoading || consumers.isLoading ? <section className="card animate-pulse" aria-label="Loading order form options"><div className="h-3 w-28 rounded bg-raised" /><div className="mt-2 h-8 rounded bg-raised" /><div className="mt-4 h-3 w-32 rounded bg-raised" /><div className="mt-2 h-8 rounded bg-raised" /></section> : null}
    {assets.error && isPaydError(assets.error) ? <ErrorState error={assets.error} copyByCode={{}} /> : null}
    {consumers.error && isPaydError(consumers.error) ? <ErrorState error={consumers.error} copyByCode={{}} /> : null}
    {mutation.data ? <section className="card border-severity-success" role="status"><p className="font-medium">{mutation.data.status === 200 ? "An existing order was returned; no new order was created." : "A new order was created."}</p><Link href={`/orders/${encodeURIComponent(mutation.data.order.id)}`} className="mt-2 inline-flex cursor-pointer text-severity-progress transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">Open order {mutation.data.order.id}</Link></section> : null}
    <CreateError error={mutation.error} requested={requested} stored={stored} />

    <form className="space-y-4" onSubmit={(event) => void submit(event)}>
      <section className="card">
        <h2 className="card-title mb-3">Order definition</h2>
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="field">Asset<select required value={asset} onChange={(event) => setAsset(event.currentTarget.value)} className="input"><option value="">Select asset</option>{assets.data?.assets.map((item) => <option key={item.symbol} value={item.symbol}>{item.symbol}</option>)}</select></label>
          <fieldset className="field"><legend>Amount type</legend><div className="flex h-8 items-center gap-4 text-[13px] normal-case tracking-normal text-ink"><label className="inline-flex cursor-pointer items-center gap-1.5 hover:text-ink-secondary"><input type="radio" checked={mode === "amount"} onChange={() => setMode("amount")} />Asset amount</label><label className="inline-flex cursor-pointer items-center gap-1.5 hover:text-ink-secondary"><input type="radio" checked={mode === "amount_usd"} onChange={() => setMode("amount_usd")} />USD amount</label></div></fieldset>
          <label className="field">{mode === "amount" ? "Amount" : "Amount USD"}<input type="text" inputMode="decimal" pattern="(0|[1-9][0-9]*)(\.[0-9]+)?" value={value} onChange={(event) => setValue(event.currentTarget.value)} className="input font-mono" />{selected && !validValue && value ? <span className="flex items-center gap-1 normal-case tracking-normal text-[13px] text-severity-critical"><AlertTriangle aria-hidden="true" size={14} strokeWidth={1.75} />Use a decimal string with at most {selected.decimals} fractional digits.</span> : null}</label>
          <label className="field">TTL seconds<input type="number" min="0" value={ttl} onChange={(event) => setTtl(event.currentTarget.valueAsNumber || 0)} className="input font-mono" /></label>
        </div>
      </section>

      <section className="card">
        <h2 className="card-title mb-3">Delivery and reference</h2>
        <div className="grid gap-3">
          <label className="field">External reference<input value={externalRef} onChange={(event) => setExternalRef(event.currentTarget.value)} className="input" /><span className="normal-case tracking-normal text-[13px] text-ink-secondary">Idempotency key: an exact match on asset, expected amount, and consumer returns the existing order; any mismatch returns <code className="font-mono">409 external_ref_conflict</code>.</span></label>
          <label className="field">Consumer<select required value={consumer} onChange={(event) => setConsumer(event.currentTarget.value)} className="input"><option value="">Select consumer</option>{consumers.data?.consumers.map((item) => <option key={item.name} value={item.name} disabled={!item.enabled}>{item.name}{item.enabled ? "" : " (disabled)"}</option>)}</select></label>
        </div>
      </section>

      <section className="card">
        <h2 className="card-title mb-3">Metadata</h2>
        <label className="field">Raw JSON object<textarea value={metadata} onChange={(event) => setMetadata(event.currentTarget.value)} rows={8} className="input h-auto py-2 font-mono" />{!metadataValid ? <span className="flex items-center gap-1 normal-case tracking-normal text-[13px] text-severity-critical"><AlertTriangle aria-hidden="true" size={14} strokeWidth={1.75} />Metadata must be a JSON object.</span> : null}</label>
      </section>

      <div className="flex justify-end"><button type="submit" disabled={!selected || !validValue || !consumer || !metadataValid || mutation.isPending} className="btn btn-primary">{mutation.isPending ? <Loader2 aria-hidden="true" size={14} strokeWidth={1.75} className="animate-spin" /> : <Plus aria-hidden="true" size={14} strokeWidth={1.75} />}{mutation.isPending ? "Creating…" : "Create order"}</button></div>
    </form>
  </main>;
}
