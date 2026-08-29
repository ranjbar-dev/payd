"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

import { useScopes } from "@/app/providers";
import { CursorPager } from "@/components/data/cursor-pager";
import { DataTable, TableFilters } from "@/components/data/data-table";
import { EmptyState } from "@/components/data/empty-state";
import { EntityId } from "@/components/data/links";
import { Timestamp } from "@/components/data/timestamp";
import { paydRequest } from "@/lib/payd/browser-client";
import type { AuditEntry, AuditResponse } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

import { ErrorNotice, ScopeDisabled } from "./system-shared";

// WSYS-045
const SCOPE = "admin:read";

// Re-implemented locally rather than imported: reports-dashboard.tsx has the
// identical UTC-day-input idea but its own toUtcSeconds is not exported, and
// this codebase's coercion detector (web/lib/no-coercion.test.ts, G1-2) keys its
// Number()/parseInt()/parseFloat() allowlist by exact file:line, so each file
// needs its own Number()-free version. Unlike the report's from/to (required,
// WRPT-001), audit's from/to are optional (backend API-040): a blank input sends
// no parameter at all, rather than defaulting to a window.
function toUtcSeconds(value: string, end = false): string {
  if (!value) return "";
  const parsed = Date.parse(`${value}T${end ? "23:59:59" : "00:00:00"}Z`);
  return Number.isNaN(parsed) ? "" : String(Math.floor(parsed / 1000));
}

// `detail` is an opaque JSON string the backend stores verbatim (never
// interpreted for meaning) — pretty-printing it when it happens to parse is a
// display nicety, the same one ErrorState already applies to error `details`,
// not a semantic read of the payload.
function prettyDetail(raw: string): string {
  if (!raw) return "";
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}

// WSYS-043: the audit action namespace (`withdrawal.*` / `order.*`) is the
// backend's own naming convention (backend/internal/store/withdrawals.go,
// orders.go) for which entity `subject` names — there is no separate type field.
// Every other namespace (`wallet.*`, `resource.*`, `balance.*`) is left unlinked,
// matching WSYS-043's literal scope rather than guessing at an address route.
function entityLink(entry: AuditEntry): { href: string; kind: "Withdrawal" | "Order" } | null {
  if (entry.action.startsWith("withdrawal.")) return { href: `/withdrawals/${encodeURIComponent(entry.subject)}`, kind: "Withdrawal" };
  if (entry.action.startsWith("order.")) return { href: `/orders/${encodeURIComponent(entry.subject)}`, kind: "Order" };
  return null;
}

function AuditRow({ entry }: Readonly<{ entry: AuditEntry }>) {
  const link = entityLink(entry);
  // WSYS-044: withdrawal-related entries are visually distinguished — a left
  // border plus an explicit text label, not colour alone (UI-021's spirit).
  const withdrawalRelated = link?.kind === "Withdrawal";
  const pretty = prettyDetail(entry.detail);
  return (
    <>
      <td className={withdrawalRelated ? "td border-l-2 border-severity-progress text-right font-mono tabular-nums" : "td text-right font-mono tabular-nums"}>
        <Timestamp seconds={entry.created_at} />
        {withdrawalRelated ? <span className="mt-0.5 block text-xs font-medium text-severity-progress">Withdrawal</span> : null}
      </td>
      <td className="td font-mono text-xs">{entry.actor}</td>
      <td className="td font-mono text-xs">{entry.action}</td>
      <td className="td">
        {link ? (
          <Link href={link.href} className="cursor-pointer font-mono text-xs text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">{entry.subject}</Link>
        ) : (
          <EntityId value={entry.subject} />
        )}
      </td>
      <td className="td">{pretty ? <pre className="line-clamp-2 max-w-xs whitespace-pre-wrap text-[11px] text-ink-secondary" title={pretty}>{pretty}</pre> : <span className="text-ink-faint">—</span>}</td>
      <td className="td text-right font-mono tabular-nums text-xs">{entry.ip || "—"}</td>
    </>
  );
}

export function SystemAudit() {
  const scopes = useScopes();
  const hasScope = scopes.includes(SCOPE);
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const actor = searchParams.get("audit_actor") ?? "";
  const action = searchParams.get("audit_action") ?? "";
  const subject = searchParams.get("audit_subject") ?? "";
  const fromDate = searchParams.get("audit_from") ?? "";
  const toDate = searchParams.get("audit_to") ?? "";
  const cursor = searchParams.get("audit_cursor") ?? "";
  const limit = searchParams.get("audit_limit") === "200" ? 200 : 50;
  const fromSeconds = toUtcSeconds(fromDate);
  const toSeconds = toUtcSeconds(toDate, true);
  const query = new URLSearchParams({ limit: String(limit) });
  if (actor) query.set("actor", actor);
  if (action) query.set("action", action);
  if (subject) query.set("subject", subject);
  if (fromSeconds) query.set("from", fromSeconds);
  if (toSeconds) query.set("to", toSeconds);
  if (cursor) query.set("cursor", cursor);
  const audit = useQuery(paydQueryOptions({ queryKey: queryKeys.audit(Object.fromEntries(query)), queryFn: () => paydRequest<AuditResponse>(["audit"], {}, query), polling: { tier: "D" }, enabled: hasScope }));
  const setParams = (next: Record<string, string>) => {
    const value = new URLSearchParams(searchParams);
    Object.entries(next).forEach(([key, item]) => (item ? value.set(key, item) : value.delete(key)));
    if (!("audit_cursor" in next)) value.delete("audit_cursor");
    router.replace(`${pathname}${value.size ? `?${value}` : ""}`);
  };

  if (!hasScope) return <ScopeDisabled scope={SCOPE} label="The audit tab" />;

  const rows = audit.data?.entries ?? [];
  const active = Boolean(actor || action || subject || fromDate || toDate);

  return (
    <section className="card space-y-3">
      <h2 className="card-title">Audit log</h2>
      <p className="text-sm text-ink-secondary">
        Every dashboard action reaches payd with one shared API key, so the <code className="font-mono text-xs">actor</code>{" "}
        recorded below is the dashboard, not the human who clicked (backend AUTH-050, WDR-024, WSYS-042).
        Attribution to a specific operator comes from the dashboard&apos;s own application logs, not this table.
      </p>
      <TableFilters active={active} onClear={() => setParams({ audit_actor: "", audit_action: "", audit_subject: "", audit_from: "", audit_to: "" })}>
        <label className="grid gap-1 text-xs text-ink-secondary">Actor<input value={actor} onChange={(event) => setParams({ audit_actor: event.currentTarget.value })} className="input h-8" /></label>
        <label className="grid gap-1 text-xs text-ink-secondary">Action<input value={action} onChange={(event) => setParams({ audit_action: event.currentTarget.value })} className="input h-8" /></label>
        <label className="grid gap-1 text-xs text-ink-secondary">Subject<input value={subject} onChange={(event) => setParams({ audit_subject: event.currentTarget.value })} className="input h-8" /></label>
        <label className="grid gap-1 text-xs text-ink-secondary">From (UTC)<input type="date" value={fromDate} onChange={(event) => setParams({ audit_from: event.currentTarget.value })} className="input h-8" /></label>
        <label className="grid gap-1 text-xs text-ink-secondary">To (UTC)<input type="date" value={toDate} onChange={(event) => setParams({ audit_to: event.currentTarget.value })} className="input h-8" /></label>
      </TableFilters>
      <DataTable
        columns={[
          { id: "created", label: "When" },
          { id: "actor", label: "Actor" },
          { id: "action", label: "Action" },
          { id: "subject", label: "Subject" },
          { id: "detail", label: "Detail" },
          { id: "ip", label: "Source IP" },
        ]}
        rows={rows}
        rowKey={(entry) => String(entry.id)}
        renderRow={(entry) => <AuditRow entry={entry} />}
        defaultSort="Backend newest-first order (never re-sorted client-side — WSYS-041/UI-043)"
        caption="Audit log"
        loading={audit.isLoading}
        emptyState={<EmptyState kind="search" title={active ? "No audit entries match these filters" : "No audit entries yet"} description={active ? "Widen or clear the filters above." : "Entries appear as dashboard-proxied actions reach payd."} />}
      />
      <p className="flex items-start gap-1 text-xs text-ink-secondary">
        <AlertTriangle aria-hidden="true" size={12} className="mt-0.5 shrink-0 text-severity-progress" />
        Rows marked <strong className="mx-1">Withdrawal</strong> link to the withdrawal — backend WDR-024 writes every
        withdrawal request, approval, and outcome here, and those are the entries an incident review needs (WSYS-044).
        There is no export, deletion, or edit control for audit records on this page (WSYS-046, WRPT-042).
      </p>
      <ErrorNotice error={audit.isError ? audit.error : null} updatedAt={audit.dataUpdatedAt} onReload={() => void audit.refetch()} />
      <CursorPager
        nextCursor={audit.data?.next_cursor}
        hasResults={rows.length > 0}
        limit={limit}
        onNext={(next) => setParams({ audit_cursor: next })}
        onStart={() => setParams({ audit_cursor: "" })}
        onLimitChange={(next) => setParams({ audit_limit: String(next) })}
      />
    </section>
  );
}
