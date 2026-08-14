"use client";

import type { KeyboardEvent, ReactNode } from "react";

export type DataTableColumn = {
  id: string;
  label: ReactNode;
  className?: string;
};

export function TableFilters({
  children,
  active = false,
  onClear,
}: Readonly<{ children: ReactNode; active?: boolean; onClear: () => void }>) {
  return (
    <div className="mb-3 flex flex-wrap items-end gap-3">
      {children}
      {active ? (
        <button
          type="button"
          className="text-sm text-severity-progress underline underline-offset-2"
          onClick={onClear}
        >
          Clear all
        </button>
      ) : null}
    </div>
  );
}

export function DataTable<T>({
  columns,
  rows,
  rowKey,
  renderRow,
  defaultSort,
  caption,
  loading = false,
  skeletonRows = 6,
  onRowClick,
  emptyState,
}: Readonly<{
  columns: DataTableColumn[];
  rows: T[];
  rowKey: (row: T) => string;
  renderRow: (row: T) => ReactNode;
  defaultSort: string;
  caption: string;
  loading?: boolean;
  skeletonRows?: number;
  onRowClick?: (row: T) => void;
  emptyState?: ReactNode;
}>) {
  const activate = (row: T, event: KeyboardEvent<HTMLTableRowElement>) => {
    if (onRowClick && (event.key === "Enter" || event.key === " ")) {
      event.preventDefault();
      onRowClick(row);
    }
  };
  return (
    <div className="overflow-x-auto border border-border-subtle bg-panel">
      <table
        className="w-full min-w-max border-collapse text-left text-sm"
        data-default-sort={defaultSort}
      >
        <caption className="sr-only">
          {caption}. Default sort: {defaultSort}.
        </caption>
        <thead className="sticky top-0 z-10 bg-raised text-xs uppercase tracking-wide text-ink-secondary">
          <tr>
            {columns.map((column) => (
              <th
                key={column.id}
                className={`whitespace-nowrap px-3 py-2 font-medium ${column.className ?? ""}`}
              >
                {column.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {loading ? (
            Array.from({ length: skeletonRows }, (_, index) => (
              <tr key={index} className="h-11 border-t border-border-subtle">
                <td colSpan={columns.length} className="px-3">
                  <div className="h-3 w-4/5 bg-border-subtle" />
                </td>
              </tr>
            ))
          ) : rows.length ? (
            rows.map((row) => (
              <tr
                key={rowKey(row)}
                className={`h-11 border-t border-border-subtle ${onRowClick ? "cursor-pointer hover:bg-raised" : ""}`}
                onClick={onRowClick ? () => onRowClick(row) : undefined}
                onKeyDown={(event) => activate(row, event)}
                tabIndex={onRowClick ? 0 : undefined}
                role={onRowClick ? "link" : undefined}
              >
                {renderRow(row)}
              </tr>
            ))
          ) : (
            <tr>
              <td colSpan={columns.length} className="p-3">
                {emptyState}
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}
