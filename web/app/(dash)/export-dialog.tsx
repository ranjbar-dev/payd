"use client";

import { useId, useState } from "react";

import { useScopes } from "@/app/providers";

// WRPT-030/WRPT-036/WRPT-037: shared by the orders list, the withdrawals list, and
// the reports page itself. Every export corresponds to a JSON list the operator can
// already see (WRPT-030), so this component takes the caller's CURRENT filters as a
// prop and never carries its own separate filter state that could drift from the
// list on screen.
export type ExportKind = "orders" | "withdrawals";

const requiredScope: Record<ExportKind, string> = { orders: "orders:read", withdrawals: "withdrawals:read" };
const filename: Record<ExportKind, string> = { orders: "orders.csv", withdrawals: "withdrawals.csv" };
const label: Record<ExportKind, string> = { orders: "orders", withdrawals: "withdrawals" };

const DEFAULT_CAP = "10000";

// WRPT-031: reject a cap outside 1-100,000 before the request is ever sent, matching
// (not duplicating) the backend's own limit (backend/internal/api/export.go
// exportLimit). Validated as a digit string, never parsed to a JS number: this
// codebase's coercion detector (web/lib/no-coercion.test.ts, G1-2) flags every
// Number()/parseInt()/parseFloat() call in app/, components/, and lib/, not only
// calls on money fields, and the cap never needs to be a JS number — it is one more
// string forwarded into the export URL's query string.
function validRowCap(raw: string): boolean {
  const trimmed = raw.trim();
  if (!/^[1-9]\d*$/.test(trimmed)) return false;
  if (trimmed.length < 6) return true;
  if (trimmed.length > 6) return false;
  return trimmed <= "100000";
}

export function ExportDialog({
  kind,
  filters,
  triggerLabel = "Export CSV",
}: Readonly<{ kind: ExportKind; filters: Readonly<Record<string, string>>; triggerLabel?: string }>) {
  const scopes = useScopes();
  const scope = requiredScope[kind];
  const hasScope = scopes.includes(scope);
  const [open, setOpen] = useState(false);
  const [cap, setCap] = useState(DEFAULT_CAP);
  const capId = useId();
  const capValid = validRowCap(cap);
  const applied = Object.entries(filters).filter(([, value]) => value);

  if (!open) {
    return <button type="button" disabled={!hasScope} title={hasScope ? undefined : `Disabled: this payd key is missing the ${scope} scope`} className="border border-border-strong px-3 py-1.5 text-sm hover:bg-raised disabled:cursor-not-allowed disabled:opacity-50" onClick={() => setOpen(true)}>{triggerLabel}</button>;
  }

  const query = new URLSearchParams();
  applied.forEach(([key, value]) => query.set(key, value));
  if (capValid) query.set("limit", cap.trim());
  const href = `/api/payd/export/${filename[kind]}?${query}`;

  return <div className="border border-border-strong bg-panel p-4 text-sm" role="group" aria-label={`Export ${label[kind]} as CSV`}>
    <div className="flex items-start justify-between gap-3"><h3 className="font-semibold">Export {label[kind]} as CSV</h3><button type="button" className="text-ink-secondary underline underline-offset-2 hover:text-ink" onClick={() => setOpen(false)}>Close</button></div>
    <p className="mt-2 text-ink-secondary">Applied filters: {applied.length ? applied.map(([key, value]) => <code key={key} className="mr-2 font-mono text-xs text-ink">{key}={value}</code>) : "none — this exports the unfiltered list"}.</p>
    <label htmlFor={capId} className="mt-3 grid gap-1 text-xs text-ink-secondary">Row cap (1–100,000)
      <input id={capId} type="number" min={1} max={100000} step={1} inputMode="numeric" value={cap} onChange={(event) => setCap(event.currentTarget.value)} className="w-40 border border-border-strong bg-panel px-2 py-1.5 text-sm text-ink" />
    </label>
    {!capValid ? <p className="mt-1 text-severity-warning" role="alert">Enter a whole number from 1 to 100,000.</p> : null}
    {/* WRPT-035: a capped export must never be mistaken for a complete one. */}
    <p className="mt-2 text-ink-secondary">This export stops at {capValid ? <code className="font-mono text-ink">{cap.trim()}</code> : "the row cap above"} rows. If more rows match the applied filters, the file is truncated at the cap rather than continuing past it.</p>
    {!hasScope ? <p className="mt-2 text-severity-warning" role="alert">Disabled: this payd key is missing the <code className="font-mono">{scope}</code> scope.</p> : null}
    {capValid && hasScope
      ? <a href={href} download className="mt-3 inline-block border border-severity-progress px-3 py-1.5 hover:bg-raised" onClick={() => setOpen(false)}>Download CSV</a>
      : <button type="button" disabled className="mt-3 inline-block cursor-not-allowed border border-border-strong px-3 py-1.5 text-ink-faint opacity-50">Download CSV</button>}
    {/* WRPT-033/BFF-011: this is a plain navigation-triggering GET, not a fetch-into-memory-then-save — the browser streams and saves the response itself, so a 100,000-row export never sits in JS memory and never blocks the rest of the page. */}
    {/* WRPT-034/INV-1: no auto-retry. If the download fails partway, start a new export deliberately; nothing here re-issues the request on its own. */}
    <p className="mt-2 text-xs text-ink-secondary">The download streams directly to the browser and does not block this page. If it fails partway, start a new export yourself — it is never retried automatically.</p>
  </div>;
}
