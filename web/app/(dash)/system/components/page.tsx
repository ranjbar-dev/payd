"use client";

import { useState } from "react";

import { AlarmCounter } from "@/components/data/alarm-counter";
import { Amount } from "@/components/data/amount";
import { CursorPager } from "@/components/data/cursor-pager";
import { DataTable, TableFilters } from "@/components/data/data-table";
import { EmptyState } from "@/components/data/empty-state";
import { ErrorState } from "@/components/data/error-state";
import { AddressLink, EntityId, TxidLink } from "@/components/data/links";
import { StatusBadge } from "@/components/data/status-badge";
import { Timestamp } from "@/components/data/timestamp";
import { ConfirmDialog } from "@/components/forms/confirm-dialog";

const SAMPLE_ROWS = [
  {
    id: "01J9VFR7G8XQ9A2M8C4D6E0F1G",
    address: "TXYZabCDefGhijKLMNoPQRsTUVwXyZ8j9K",
    amount: "100.000001",
    status: "needs_operator",
    created: 1893456000,
  },
];

export default function ComponentsPage() {
  const [dialogOpen, setDialogOpen] = useState(false);
  const [cursor, setCursor] = useState<string | null>("opaque-next-cursor");
  return (
    <main className="mx-auto max-w-7xl space-y-8 p-6">
      <header>
        <p className="font-mono text-xs uppercase tracking-[0.2em] text-ink-faint">
          System / Components
        </p>
        <h1 className="mt-2 text-2xl font-semibold">Operations UI reference</h1>
        <p className="mt-2 text-ink-secondary">
          Static examples only — this page makes no API calls.
        </p>
      </header>
      <section className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        <AlarmCounter label="Unattributed payments" count={0} />
        <AlarmCounter label="Dead IPNs" count={2} />
        <AlarmCounter label="Failed withdrawals" count={1} />
        <AlarmCounter label="needs_operator" count={1} severity="critical" />
      </section>
      <section className="space-y-3">
        <h2 className="text-lg font-semibold">Dense data</h2>
        <TableFilters active onClear={() => undefined}>
          <label className="text-sm text-ink-secondary">
            Status{" "}
            <select className="ml-1 border border-border-strong bg-panel px-2 py-1">
              <option>needs_operator</option>
            </select>
          </label>
        </TableFilters>
        <DataTable
          columns={[
            { id: "id", label: "Withdrawal" },
            { id: "address", label: "Destination" },
            { id: "amount", label: "Amount", className: "text-right" },
            { id: "status", label: "Status" },
            { id: "created", label: "Created" },
          ]}
          rows={SAMPLE_ROWS}
          rowKey={(row) => row.id}
          defaultSort="newest first"
          caption="Withdrawal component example"
          onRowClick={() => undefined}
          renderRow={(row) => (
            <>
              <td className="px-3 py-2">
                <EntityId value={row.id} />
              </td>
              <td className="px-3 py-2">
                <AddressLink
                  address={row.address}
                  href={`/addresses/${row.address}`}
                />
              </td>
              <td className="px-3 py-2 text-right">
                <Amount value={row.amount} asset="USDT" />
              </td>
              <td className="px-3 py-2">
                <StatusBadge status={row.status} />
              </td>
              <td className="px-3 py-2">
                <Timestamp seconds={row.created} />
              </td>
            </>
          )}
        />
        <CursorPager
          nextCursor={cursor}
          hasResults
          onNext={() => setCursor(null)}
          onStart={() => setCursor(null)}
          onLimitChange={() => undefined}
        />
      </section>
      <section className="grid gap-4 lg:grid-cols-2">
        <div className="space-y-3">
          <h2 className="text-lg font-semibold">States</h2>
          <EmptyState
            kind="worklist"
            title="No unattributed payments"
            description="Rows appear here when payd cannot attribute an incoming payment to an order."
          />
          <EmptyState
            kind="search"
            title="No payments match these filters"
            description="Change or clear filters to search for a different payment."
          />
          <ErrorState
            error={{
              code: "upstream_timeout",
              details: { request_id: "demo-timeout" },
            }}
            copyByCode={{
              upstream_timeout: "The upstream service did not respond in time.",
            }}
            lastUpdatedAt={Date.now() - 120000}
            pollingIntervalMs={30000}
            onRetry={() => undefined}
          >
            <div className="border border-border-subtle p-3 text-sm">
              Last good data remains visible here.
            </div>
          </ErrorState>
        </div>
        <div className="space-y-3">
          <h2 className="text-lg font-semibold">Values and links</h2>
          <p>
            <Amount value="14.50" asset="USD" variant="usd-snapshot" />
          </p>
          <p>
            <Amount asset="USD" variant="usd-live" unavailable />
          </p>
          <p>
            <Timestamp seconds={240} variant="duration" /> worker lag
          </p>
          <p>
            <Timestamp seconds={1893456000} variant="utc-day" /> quota day
          </p>
          <p>
            Full ID: <EntityId value={SAMPLE_ROWS[0].id} full />
          </p>
          <p>
            <TxidLink
              txid="d1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
              tronscanBaseUrl="https://nile.tronscan.org"
            />
          </p>
          <button
            type="button"
            className="border border-severity-warning bg-[var(--severity-warning-bg)] px-3 py-2"
            onClick={() => setDialogOpen(true)}
          >
            Open confirmation example
          </button>
        </div>
      </section>
      <ConfirmDialog
        open={dialogOpen}
        onClose={() => setDialogOpen(false)}
        title="Authorise withdrawal"
        apiText={
          <>
            <p>API preview</p>
            <p>
              <Amount value="100.00" asset="USDT" /> to{" "}
              <AddressLink
                address="TXYZabCDefGhijKLMNoPQRsTUVwXyZ8j9K"
                href="/addresses/TXYZabCDefGhijKLMNoPQRsTUVwXyZ8j9K"
              />
            </p>
          </>
        }
        confirmLabel="Withdraw 100.00 USDT"
        requiresTotp
        ready
        onConfirm={async () => {
          setDialogOpen(false);
        }}
      />
    </main>
  );
}
