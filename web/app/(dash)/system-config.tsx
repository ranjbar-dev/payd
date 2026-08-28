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

function Field({ label, children, href }: Readonly<{ label: string; children: ReactNode; href?: string }>) {
  return (
    <div>
      <dt className="text-xs uppercase tracking-wide text-ink-faint">{label}</dt>
      <dd className="mt-0.5">{href ? <Link href={href} className="text-severity-progress underline underline-offset-2">{children}</Link> : children}</dd>
    </div>
  );
}

function Group({ title, children }: Readonly<{ title: string; children: ReactNode }>) {
  return (
    <section className="border border-border-subtle bg-panel p-4">
      <h2 className="font-semibold">{title}</h2>
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
      <p className="border border-severity-warning bg-[var(--severity-warning-bg)] p-3 text-sm text-severity-warning" role="status">
        Read-only. This is exactly what <code className="font-mono text-xs">GET /config</code> returns — nothing here is
        inferred, defaulted, or added (WSYS-022, INV-5). Changing configuration is a YAML edit and a payd restart, not a
        control on this page (WNG-006).
      </p>
      {config.data ? (
        <div className="space-y-3">
          <Group title="Assets">
            {config.data.assets.map((asset) => (
              <Field key={asset.symbol} label={asset.symbol}>
                {asset.kind} · {asset.decimals} decimals · min deposit <Amount value={asset.min_deposit} asset={asset.symbol} />
                {!asset.verified ? " · unverified" : ""}
              </Field>
            ))}
          </Group>
          <Group title="Withdrawal">
            <Field label="Require payd TOTP">{config.data.withdrawal.require_totp ? "Yes" : "No"}</Field>
            <Field label="Daily limit (USD)" href="/withdrawals"><Amount value={config.data.withdrawal.daily_limit_usd} asset="USD" /></Field>
          </Group>
          <Group title="Chain depths">
            <Field label="Confirmations required">{config.data.tron.confirmations_required}</Field>
            <Field label="Reorg depth">{config.data.tron.reorg_depth}</Field>
          </Group>
          <Group title="Orders">
            <Field label="Default TTL"><Timestamp seconds={config.data.orders.default_ttl_seconds} variant="duration" /></Field>
          </Group>
          <Group title="Energy">
            <Field label="Enabled">{config.data.energy.enabled ? "Yes" : "No"}</Field>
            <Field label="Max burn ceiling" href="/resources"><Amount value={config.data.energy.max_burn_trx} asset="TRX" /></Field>
            <Field label="Balance warning"><Amount value={config.data.energy.balance_warn_trx} asset="TRX" /></Field>
          </Group>
          <Group title="Price">
            <Field label="Stale after"><Timestamp seconds={config.data.price.stale_after_seconds} variant="duration" /></Field>
          </Group>
          <Group title="Wallet pool">
            <Field label="Pool minimum free">{config.data.wallet.pool_min_free}</Field>
            <Field label="Pool maximum size" href="/addresses">{config.data.wallet.pool_max_size}</Field>
            <Field label="Cooldown"><Timestamp seconds={config.data.wallet.cooldown_seconds} variant="duration" /></Field>
          </Group>
          <Group title="Resources">
            <Field label="Bandwidth top-up" href="/resources"><Amount value={config.data.resources.bandwidth_topup_trx} asset="TRX" /></Field>
            <Field label="Minimum energy">{config.data.resources.min_energy}</Field>
            <Field label="Minimum bandwidth">{config.data.resources.min_bandwidth}</Field>
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
        <p className="text-ink-faint">Configuration has not loaded.</p>
      ) : null}
      <ErrorNotice error={config.isError ? config.error : null} updatedAt={config.dataUpdatedAt} onReload={() => void config.refetch()} />
    </section>
  );
}
