"use client";

import { useQuery } from "@tanstack/react-query";
import { ExternalLink } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";

import { Amount } from "@/components/data/amount";
import { StatusBadge } from "@/components/data/status-badge";
import { Timestamp } from "@/components/data/timestamp";
import { paydRequest } from "@/lib/payd/browser-client";
import type { ChainQuotaResponse, ChainStatusResponse, ConfigResponse, Health, Readiness } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

import { ErrorNotice } from "./system-shared";

// WSYS-061/WOVW-012: reproduces overview-dashboard.tsx's readinessDetail
// code-to-text mapping for the same reason codes — that function is a local,
// unexported helper in that file, not importable from here. Two of its cases
// (`price_stale`, `energy_burn_ceiling`) drew on `/prices` and `/chain/params`,
// neither of which is in this page's declared "Consumes" list
// (docs/specs/15-system-and-audit.md); those two cases below render the same
// reason and consequence with a link to Resources (where those figures already
// live) instead of adding fetches outside this page's declared scope. Every
// reason CODE and its underlying meaning is unchanged from overview's mapping.
function readinessDetail(
  reason: string,
  chain: ChainStatusResponse | undefined,
  quota: ChainQuotaResponse | undefined,
  config: ConfigResponse | undefined,
): ReactNode {
  switch (reason) {
    case "database_unavailable":
      return "Database is unavailable; payment processing cannot read its operational state.";
    case "database_unwritable":
      return "Database is unwritable; payment processing cannot record state.";
    case "chain_lag":
      return <>Chain follower lag: {chain?.lag_blocks ?? "—"} blocks / 20.</>;
    case "solidified_stale":
      return <>Solidified height: {chain?.solidified_height ?? "—"}; latest block <Timestamp seconds={chain?.last_block_timestamp} />.</>;
    case "price_stale":
      return (
        <>
          The newest cached price is older than the configured staleness limit; order and withdrawal creation return
          503 while this holds. See <Link href="/resources" className="cursor-pointer text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">Resources</Link> for
          current price ages.
        </>
      );
    case "trongrid_quota_projection":
      return (
        <>
          TronGrid quota projection: {quota?.percent_used ?? "—"}% / 90%. See the{" "}
          <Link href="/system?tab=quota" className="cursor-pointer text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">Quota tab</Link>.
        </>
      );
    case "reorg_depth_exceeded":
      return <>Chain reorganisation suspected: {chain?.reorg_suspected ? "yes" : "reported by readiness"}.</>;
    case "energy_burn_ceiling":
      return (
        <>
          Energy burn ceiling: {config ? <Amount value={config.energy.max_burn_trx} asset="TRX" /> : "—"}. See{" "}
          <Link href="/resources" className="cursor-pointer text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">Resources</Link> for the current energy fee.
        </>
      );
    case "clock_skew":
      return "Clock skew can cause withdrawals to be rejected or expire immediately, indistinguishable from an RPC fault.";
    case "clock_unavailable":
      return "Clock availability failure can cause withdrawals to be rejected or expire immediately, indistinguishable from an RPC fault.";
    default:
      // WOVW-012b: an unrecognised reason code still renders, as its raw string,
      // at warning severity — never hidden.
      return reason;
  }
}

export function SystemHealth({ paydHost }: Readonly<{ paydHost: string }>) {
  const health = useQuery(paydQueryOptions({ queryKey: queryKeys.health(), queryFn: () => paydRequest<Health>(["healthz"]), polling: { tier: "D" } }));
  // /readyz answers 200 {status:"ready"} or 503 {status:"degraded", reasons:[...]}
  // — the 503 is expected data here, not a transport failure, so it is accepted.
  const readiness = useQuery(paydQueryOptions({ queryKey: queryKeys.readiness(), queryFn: () => paydRequest<Readiness>(["readyz"], {}, undefined, [503]), polling: { tier: "D" } }));
  const chain = useQuery(paydQueryOptions({ queryKey: queryKeys.chain.status(), queryFn: () => paydRequest<ChainStatusResponse>(["chain", "status"]), polling: { tier: "D" } }));
  const quota = useQuery(paydQueryOptions({ queryKey: queryKeys.chain.quota(), queryFn: () => paydRequest<ChainQuotaResponse>(["chain", "quota"]), polling: { tier: "D" } }));
  const config = useQuery(paydQueryOptions({ queryKey: queryKeys.config(), queryFn: () => paydRequest<ConfigResponse>(["config"]), polling: { tier: "D" }, staleTime: Infinity, refetchOnWindowFocus: false, refetchOnReconnect: false }));

  return (
    <section className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="card">
          <h2 className="card-title">/healthz — liveness</h2>
          <p className="mt-1 text-sm text-ink-secondary">
            200 for as long as the process is running and able to serve HTTP. It says nothing about the database,
            chain follower, or price freshness (backend OPS-002).
          </p>
          {health.data ? <dl className="mt-3"><div><dt className="text-ink-faint text-[11px] uppercase tracking-wide">Status</dt><dd className="mt-1"><StatusBadge status={health.data.status} /></dd></div></dl> : !health.isError ? <div className="mt-3 h-4 w-24 animate-pulse bg-raised" aria-label="Loading liveness" /> : null}
          <ErrorNotice error={health.isError ? health.error : null} updatedAt={health.dataUpdatedAt} onReload={() => void health.refetch()} />
        </div>
        <div className="card">
          <h2 className="card-title">/readyz — readiness</h2>
          <p className="mt-1 text-sm text-ink-secondary">
            Whether the processor is healthy enough to be trusted right now — worker and chain state, not just process
            liveness (backend OPS-001).
          </p>
          {readiness.data ? (
            <div className="mt-3 space-y-2">
              <dl><div><dt className="text-ink-faint text-[11px] uppercase tracking-wide">Status</dt><dd className="mt-1"><StatusBadge status={readiness.data.status} /></dd></div></dl>
              {readiness.data.reasons?.length ? (
                <ul className="space-y-2 text-sm">
                  {readiness.data.reasons.map((reason) => (
                    <li key={reason} className="border-l-2 border-severity-warning pl-2 text-severity-warning">
                      {readinessDetail(reason, chain.data, quota.data, config.data)}
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="text-ink-secondary">All readiness checks are passing.</p>
              )}
            </div>
          ) : (
            !readiness.isError ? <div className="mt-3 h-4 w-24 animate-pulse bg-raised" aria-label="Loading readiness" /> : null
          )}
          <ErrorNotice error={readiness.isError ? readiness.error : null} updatedAt={readiness.dataUpdatedAt} onReload={() => void readiness.refetch()} />
        </div>
      </div>

      <div className="card text-sm">
        <h2 className="card-title">/metrics — Prometheus</h2>
        <p className="mt-1 text-ink-secondary">
          Plain Prometheus text exposition. This dashboard does not fetch, parse, or render it — Prometheus is the
          right consumer for a time series (WNG-009, WSYS-062). It requires a valid payd API key on the request;
          opening the link below with no key returns 401.
        </p>
        <p className="mt-2">
          <a href={`http://${paydHost}/metrics`} target="_blank" rel="noreferrer" className="inline-flex cursor-pointer items-center gap-1 font-mono text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">
            http://{paydHost}/metrics <ExternalLink aria-hidden="true" size={14} strokeWidth={1.75} />
          </a>
        </p>
        <p className="mt-1 text-xs text-ink-faint">
          This link uses only the configured payd host, never its port or any credential (WSYS-054). If payd is not
          listening on the default port shown here, adjust the port yourself before opening it.
        </p>
      </div>

      <div className="card text-sm">
        <h2 className="card-title">API reference</h2>
        <p className="mt-1 text-ink-secondary">The served OpenAPI document is the authority when these docs and the API disagree (WSYS-063).</p>
        <p className="mt-2">
          <a href="/api/payd/openapi.yaml" target="_blank" rel="noreferrer" className="inline-flex cursor-pointer items-center gap-1 text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">
            Open /openapi.yaml <ExternalLink aria-hidden="true" size={14} strokeWidth={1.75} />
          </a>
        </p>
      </div>
    </section>
  );
}
