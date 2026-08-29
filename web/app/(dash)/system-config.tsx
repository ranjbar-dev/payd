"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import type { ReactNode } from "react";

import { useScopes } from "@/app/providers";
import { Amount } from "@/components/data/amount";
import { Timestamp } from "@/components/data/timestamp";
import { paydRequest } from "@/lib/payd/browser-client";
import type { ConfigResponse } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

import { ErrorNotice, ScopeDisabled } from "./system-shared";

// WSYS-024
const SCOPE = "admin:read";

function Field({ label, children, href, numeric = false }: Readonly<{ label: string; children: ReactNode; href?: string; numeric?: boolean }>) {
  return (
    <div>
      <dt className="text-ink-faint text-[11px] uppercase tracking-wide">{label}</dt>
      <dd className={numeric ? "mt-0.5 text-right font-mono tabular-nums" : "mt-0.5"}>{href ? <Link href={href} className="cursor-pointer text-severity-progress underline underline-offset-2 transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]">{children}</Link> : children}</dd>
    </div>
  );
}

function Group({ title, children }: Readonly<{ title: string; children: ReactNode }>) {
  return (
    <section className="card">
      <h2 className="card-title">{title}</h2>
      <dl className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">{children}</dl>
    </section>
  );
}

export function SystemConfig() {
  const scopes = useScopes();
  const hasScope = scopes.includes(SCOPE);
  // WOVW-060/WSYS-020a: the same query key and cache settings as the Overview
  // page's own /config fetch, so a session that already visited either page never
  // issues a second request for it — /config's allowlisted thresholds change only
  // on restart, so this is fetched once per session and cached, never polled.
  const config = useQuery(paydQueryOptions({
    queryKey: queryKeys.config(),
    queryFn: () => paydRequest<ConfigResponse>(["config"]),
    polling: { tier: "D" },
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    enabled: hasScope,
  }));

  if (!hasScope) return <ScopeDisabled scope={SCOPE} label="The config tab" />;

  return (
    <section className="space-y-4">
      <p className="card border-severity-warning bg-[var(--severity-warning-bg)] text-sm text-severity-warning" role="status">
        Read-only. This is exactly what <code className="font-mono text-xs">GET /config</code> returns — nothing here is
        inferred, defaulted, or added (WSYS-022, INV-5). Changing configuration is a YAML edit and a payd restart, not a
        control on this page (WNG-006).
      </p>
      {config.data ? (
        <div className="space-y-3">
          <Group title="Assets">
            {config.data.assets.map((asset) => (
              <Field key={asset.symbol} label={asset.symbol} numeric>
                {asset.kind} · {asset.decimals} decimals · min deposit <Amount value={asset.min_deposit} asset={asset.symbol} />
                {!asset.verified ? " · unverified" : ""}
              </Field>
            ))}
          </Group>
          <Group title="Withdrawal">
            <Field label="Require payd TOTP">{config.data.withdrawal.require_totp ? "Yes" : "No"}</Field>
            <Field label="Daily limit (USD)" href="/withdrawals" numeric><Amount value={config.data.withdrawal.daily_limit_usd} asset="USD" /></Field>
          </Group>
          <Group title="Chain depths">
            <Field label="Confirmations required" numeric>{config.data.tron.confirmations_required}</Field>
            <Field label="Reorg depth" numeric>{config.data.tron.reorg_depth}</Field>
          </Group>
          <Group title="Orders">
            <Field label="Default TTL" numeric><Timestamp seconds={config.data.orders.default_ttl_seconds} variant="duration" /></Field>
          </Group>
          <Group title="Energy">
            <Field label="Enabled">{config.data.energy.enabled ? "Yes" : "No"}</Field>
            <Field label="Max burn ceiling" href="/resources" numeric><Amount value={config.data.energy.max_burn_trx} asset="TRX" /></Field>
            <Field label="Balance warning" numeric><Amount value={config.data.energy.balance_warn_trx} asset="TRX" /></Field>
          </Group>
          <Group title="Price">
            <Field label="Stale after" numeric><Timestamp seconds={config.data.price.stale_after_seconds} variant="duration" /></Field>
          </Group>
          <Group title="Wallet pool">
            <Field label="Pool minimum free" numeric>{config.data.wallet.pool_min_free}</Field>
            <Field label="Pool maximum size" href="/addresses" numeric>{config.data.wallet.pool_max_size}</Field>
            <Field label="Cooldown" numeric><Timestamp seconds={config.data.wallet.cooldown_seconds} variant="duration" /></Field>
          </Group>
          <Group title="Resources">
            <Field label="Bandwidth top-up" href="/resources" numeric><Amount value={config.data.resources.bandwidth_topup_trx} asset="TRX" /></Field>
            <Field label="Minimum energy" numeric>{config.data.resources.min_energy}</Field>
            <Field label="Minimum bandwidth" numeric>{config.data.resources.min_bandwidth}</Field>
          </Group>
          <Group title="Webhook consumers">
            {config.data.consumers.length ? (
              config.data.consumers.map((name) => (
                <Field key={name} label={name} href={`/webhooks?consumer=${encodeURIComponent(name)}`}>Configured</Field>
              ))
            ) : (
              <p className="text-ink-secondary sm:col-span-3">No consumers configured.</p>
            )}
          </Group>
        </div>
      ) : !config.isError ? (
        <div className="card animate-pulse" aria-label="Loading effective configuration"><div className="h-3 w-36 bg-raised" /><div className="mt-4 h-16 bg-raised" /></div>
      ) : null}
      <ErrorNotice error={config.isError ? config.error : null} updatedAt={config.dataUpdatedAt} onReload={() => void config.refetch()} />
    </section>
  );
}
