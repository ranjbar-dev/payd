"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, ExternalLink } from "lucide-react";

import { useTronscanBaseUrl } from "@/app/providers";
import { Amount } from "@/components/data/amount";
import { DataTable } from "@/components/data/data-table";
import { EmptyState } from "@/components/data/empty-state";
import { EntityId } from "@/components/data/links";
import { paydRequest } from "@/lib/payd/browser-client";
import type { AssetsResponse } from "@/lib/payd/types";
import { paydQueryOptions } from "@/lib/query";
import { queryKeys } from "@/lib/query-keys";

import { ErrorNotice } from "./system-shared";

type Asset = AssetsResponse["assets"][number];

// WSYS-033: no shared "Tronscan contract link" component exists in components/
// data/ — TxidLink is specifically a transaction link (`/#/transaction/`), the
// wrong path for a contract address. address-clear-drift.tsx already builds an
// ad hoc Tronscan address link inline for the same reason; this mirrors that
// established local pattern rather than repurposing TxidLink for a path it
// doesn't build. EntityId (a shared component) still does the truncate+copy
// half.
function ContractLink({ contract, tronscanBaseUrl }: Readonly<{ contract: string; tronscanBaseUrl: string }>) {
  if (!contract) return <span className="text-ink-faint">Native asset — no contract</span>;
  const href = `${tronscanBaseUrl.replace(/\/$/, "")}/#/contract/${encodeURIComponent(contract)}`;
  return (
    <span className="inline-flex items-center gap-1">
      <EntityId value={contract} />
      <a href={href} target="_blank" rel="noreferrer" className="inline-flex cursor-pointer text-severity-progress transition-colors duration-150 hover:text-ink focus-visible:outline-2 focus-visible:outline-[var(--focus-ring)]" aria-label={`Open ${contract} on Tronscan`} title="Open on Tronscan">
        <ExternalLink aria-hidden="true" size={14} strokeWidth={1.75} />
      </a>
    </span>
  );
}

function AssetRow({ asset, tronscanBaseUrl }: Readonly<{ asset: Asset; tronscanBaseUrl: string }>) {
  return (
    <>
      <td className="td font-mono font-medium">
        {asset.symbol}
        {!asset.verified ? <span className="ml-2 inline-flex items-center gap-1 text-severity-warning" title="Not marked verified by payd's configuration"><AlertTriangle aria-hidden="true" size={13} strokeWidth={1.75} />unverified</span> : null}
      </td>
      <td className="td font-mono">{asset.kind}</td>
      <td className="td"><ContractLink contract={asset.contract} tronscanBaseUrl={tronscanBaseUrl} /></td>
      <td className="td text-right font-mono tabular-nums">{asset.decimals}</td>
      <td className="td text-right"><Amount value={asset.min_deposit} asset={asset.symbol} /></td>
      <td className="td">{asset.verified ? "Verified" : <span className="text-severity-warning">Unverified</span>}</td>
    </>
  );
}

export function SystemAssets() {
  const tronscanBaseUrl = useTronscanBaseUrl();
  // WSYS-034: the same query key used by every amount input and dust indicator
  // elsewhere in the dashboard (order-create-form.tsx, payments-dashboard.tsx,
  // withdrawal-wizard.tsx), so this session fetches /assets once and every
  // consumer, including this tab, shares the cached result rather than
  // re-fetching per form.
  const assets = useQuery(paydQueryOptions({ queryKey: queryKeys.assets(), queryFn: () => paydRequest<AssetsResponse>(["assets"]), polling: { tier: "D" } }));
  const rows = assets.data?.assets ?? [];

  return (
    <section className="card space-y-3">
      <h2 className="card-title">Configured assets</h2>
      <p className="text-sm text-ink-secondary">
        Decimals govern input precision everywhere in the dashboard (backend API-034) — every amount input and dust
        indicator reads them from this same response instead of a hardcoded copy.
      </p>
      <DataTable
        columns={[
          { id: "symbol", label: "Symbol" },
          { id: "kind", label: "Kind" },
          { id: "contract", label: "Contract" },
          { id: "decimals", label: "Decimals" },
          { id: "min-deposit", label: "Minimum deposit" },
          { id: "verified", label: "Verified" },
        ]}
        rows={rows}
        rowKey={(asset) => asset.symbol}
        renderRow={(asset) => <AssetRow asset={asset} tronscanBaseUrl={tronscanBaseUrl} />}
        defaultSort="Backend asset order"
        caption="Configured assets"
        loading={assets.isLoading}
        emptyState={<EmptyState kind="search" title="No assets configured" description="No asset is configured on this payd instance." />}
      />
      <ErrorNotice error={assets.isError ? assets.error : null} updatedAt={assets.dataUpdatedAt} onReload={() => void assets.refetch()} />
    </section>
  );
}
