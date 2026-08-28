"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";

import { Amount } from "@/components/data/amount";
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
  const fields = Array.isArray(failure.details.fields) ? failure.details.fields.filter((field): field is string => typeof field === "string") : [];
  if (failure.code === "external_ref_conflict") return <section className="border border-severity-warning bg-[var(--severity-warning-bg)] p-3" role="alert"><p className="font-medium">Creation did not succeed: this external reference belongs to a different order request.</p><p className="mt-1 text-sm">Error code: <code className="select-all font-mono">external_ref_conflict</code></p>{stored ? <><div className="mt-3 grid gap-2 text-sm sm:grid-cols-3"><strong>Field</strong><strong>Requested</strong><strong>Stored</strong>{fields.map((field) => <><span key={`${field}:field`} className="font-mono">{field}</span><span key={`${field}:requested`} className="font-mono">{field === "asset" ? requested.asset : field === "consumer" ? requested.consumer : requested.value}</span><span key={`${field}:stored`} className="font-mono">{field === "asset" ? stored.asset : field === "consumer" ? stored.consumer : stored.amount}</span></>)}</div><Link href={`/orders/${encodeURIComponent(stored.id)}`} className="mt-3 inline-block underline underline-offset-2">Open the existing order</Link></> : <p className="mt-2 text-sm">The conflicting order could not be loaded. <Link href="/orders" className="underline underline-offset-2">Search orders by this external reference</Link>.</p>}</section>;
  if (failure.code === "address_pool_exhausted") return <p role="alert" className="border border-severity-warning bg-[var(--severity-warning-bg)] p-3">The address pool is at <code className="font-mono">wallet.pool_max_size</code> with no free address. <Link href="/addresses" className="underline underline-offset-2">Open addresses</Link>. Error code: <code className="select-all font-mono">address_pool_exhausted</code></p>;
  if (failure.code === "price_unavailable") return <p role="alert" className="border border-severity-warning bg-[var(--severity-warning-bg)] p-3">Creation was refused because the price is stale or unavailable. <Link href="/resources" className="underline underline-offset-2">Open the prices card</Link>. Error code: <code className="select-all font-mono">price_unavailable</code></p>;
  if (failure.code === "unknown_consumer") return <p role="alert" className="border border-severity-warning bg-[var(--severity-warning-bg)] p-3">The selected consumer <code className="font-mono">{requested.consumer || "(none)"}</code> is unknown or disabled. <Link href="/webhooks" className="underline underline-offset-2">Open Webhooks</Link>. Error code: <code className="select-all font-mono">unknown_consumer</code></p>;
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

  return <main className="mx-auto max-w-3xl space-y-4 p-4 lg:p-6"><header><p className="font-mono text-xs uppercase tracking-[0.2em] text-ink-faint">Operations / Orders / New</p><h1 className="mt-1 text-2xl font-semibold">Create order</h1><p className="mt-1 text-sm text-severity-warning">A dashboard-created order has no consumer service expecting its IPNs. Select the consumer deliberately.</p></header>
    {mutation.data ? <section className="border border-severity-success bg-panel p-3" role="status"><p className="font-medium">{mutation.data.status === 200 ? "An existing order was returned; no new order was created." : "A new order was created."}</p><Link href={`/orders/${encodeURIComponent(mutation.data.order.id)}`} className="mt-2 inline-block underline underline-offset-2">Open order {mutation.data.order.id}</Link></section> : null}
    <CreateError error={mutation.error} requested={requested} stored={stored} />
    <form className="space-y-4 border border-border-subtle bg-panel p-4" onSubmit={(event) => void submit(event)}><label className="grid gap-1 text-sm">Asset<select required value={asset} onChange={(event) => setAsset(event.currentTarget.value)} className="border border-border-strong bg-panel px-2 py-2"><option value="">Select asset</option>{assets.data?.assets.map((item) => <option key={item.symbol} value={item.symbol}>{item.symbol}</option>)}</select></label><div className="flex gap-4 text-sm"><label><input type="radio" checked={mode === "amount"} onChange={() => setMode("amount")} /> Asset amount</label><label><input type="radio" checked={mode === "amount_usd"} onChange={() => setMode("amount_usd")} /> USD amount</label></div><label className="grid gap-1 text-sm">{mode === "amount" ? "Amount" : "Amount USD"}<input type="text" inputMode="decimal" pattern="(0|[1-9][0-9]*)(\\.[0-9]+)?" value={value} onChange={(event) => setValue(event.currentTarget.value)} className="font-mono border border-border-strong bg-panel px-2 py-2" />{selected && !validValue && value ? <span className="text-xs text-severity-warning">Use a decimal string with at most {selected.decimals} fractional digits.</span> : null}</label><label className="grid gap-1 text-sm">External reference <input value={externalRef} onChange={(event) => setExternalRef(event.currentTarget.value)} className="border border-border-strong bg-panel px-2 py-2" /><span className="text-xs text-ink-secondary">Idempotency key: an exact asset, expected amount, and consumer match returns the existing order; a mismatch returns a conflict.</span></label><label className="grid gap-1 text-sm">Consumer<select required value={consumer} onChange={(event) => setConsumer(event.currentTarget.value)} className="border border-border-strong bg-panel px-2 py-2"><option value="">Select consumer</option>{consumers.data?.consumers.map((item) => <option key={item.name} value={item.name} disabled={!item.enabled}>{item.name}{item.enabled ? "" : " (disabled)"}</option>)}</select></label><label className="grid gap-1 text-sm">TTL seconds <input type="number" min="0" value={ttl} onChange={(event) => setTtl(event.currentTarget.valueAsNumber || 0)} className="border border-border-strong bg-panel px-2 py-2" /></label><label className="grid gap-1 text-sm">Metadata (raw JSON object)<textarea value={metadata} onChange={(event) => setMetadata(event.currentTarget.value)} rows={8} className="font-mono border border-border-strong bg-panel px-2 py-2" />{!metadataValid ? <span className="text-xs text-severity-warning">Metadata must be a JSON object.</span> : null}</label><button type="submit" disabled={!selected || !validValue || !consumer || !metadataValid || mutation.isPending} className="border border-severity-progress px-3 py-2 disabled:opacity-50">{mutation.isPending ? "Creating…" : "Create order"}</button></form>
  </main>;
}
