"use client";

import { Component } from "lucide-react";
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
    <main className="page">
      <header>
        <p className="page-kicker">
          <Component aria-hidden="true" size={14} strokeWidth={1.75} />
          System / Components
        </p>
        <h1 className="page-title mt-1">Operations UI reference</h1>
        <p className="mt-2 text-ink-secondary">
          Static examples only — this page makes no API calls.
        </p>
      </header>

      <section className="card">
        <h2 className="card-title">AlarmCounter</h2>
        <div className="mt-3 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          <AlarmCounter label="Unattributed payments" count={0} />
          <AlarmCounter label="Dead IPNs" count={2} />
          <AlarmCounter label="Failed withdrawals" count={1} />
          <AlarmCounter label="needs_operator" count={1} severity="critical" />
        </div>
      </section>

      <section className="card">
        <h2 className="card-title">TableFilters</h2>
        <div className="mt-3">
          <TableFilters active onClear={() => undefined}>
            <label className="text-sm text-ink-secondary">
              Status{" "}
              <select className="input ml-1 w-auto">
                <option>needs_operator</option>
              </select>
            </label>
          </TableFilters>
        </div>
      </section>

      <section className="card">
        <h2 className="card-title">DataTable</h2>
        <div className="mt-3">
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
                <td className="td">
                  <EntityId value={row.id} />
                </td>
                <td className="td">
                  <AddressLink
                    address={row.address}
                    href={`/addresses/${row.address}`}
                  />
                </td>
                <td className="td text-right">
                  <Amount value={row.amount} asset="USDT" />
                </td>
                <td className="td">
                  <StatusBadge status={row.status} />
                </td>
                <td className="td">
                  <Timestamp seconds={row.created} />
                </td>
              </>
            )}
          />
        </div>
      </section>

      <section className="card">
        <h2 className="card-title">CursorPager</h2>
        <div className="mt-3">
          <CursorPager
            nextCursor={cursor}
            hasResults
            onNext={() => setCursor(null)}
            onStart={() => setCursor(null)}
            onLimitChange={() => undefined}
          />
        </div>
      </section>

      <section className="grid gap-3 lg:grid-cols-2">
        <div className="card">
          <h2 className="card-title">EmptyState</h2>
          <div className="mt-3 space-y-3">
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
          </div>
        </div>
        <div className="card">
          <h2 className="card-title">ErrorState</h2>
          <div className="mt-3">
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
        </div>
      </section>

      <section className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        <div className="card">
          <h2 className="card-title">Amount</h2>
          <div className="mt-3 space-y-2">
            <p>
              <Amount value="14.50" asset="USD" variant="usd-snapshot" />
            </p>
            <p>
              <Amount asset="USD" variant="usd-live" unavailable />
            </p>
          </div>
        </div>
        <div className="card">
          <h2 className="card-title">Timestamp</h2>
          <div className="mt-3 space-y-2">
            <p>
              <Timestamp seconds={240} variant="duration" /> worker lag
            </p>
            <p>
              <Timestamp seconds={1893456000} variant="utc-day" /> quota day
            </p>
          </div>
        </div>
        <div className="card">
          <h2 className="card-title">EntityId</h2>
          <dl className="mt-3 grid gap-2 text-sm">
            <div>
              <dt className="text-[11px] uppercase tracking-wide text-ink-faint">Full ID</dt>
              <dd className="mt-1"><EntityId value={SAMPLE_ROWS[0].id} full /></dd>
            </div>
          </dl>
        </div>
        <div className="card">
          <h2 className="card-title">TxidLink</h2>
          <div className="mt-3">
            <TxidLink
              txid="d1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
              tronscanBaseUrl="https://nile.tronscan.org"
            />
          </div>
        </div>
        <div className="card">
          <h2 className="card-title">ConfirmDialog</h2>
          <div className="mt-3">
            <button
              type="button"
              className="btn btn-secondary"
              onClick={() => setDialogOpen(true)}
            >
              Open confirmation example
            </button>
          </div>
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
