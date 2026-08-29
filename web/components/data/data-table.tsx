"use client";

import { X } from "lucide-react";
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
          className="btn btn-ghost"
          onClick={onClear}
        >
          <X aria-hidden="true" size={14} strokeWidth={1.75} />
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
  isRowActive,
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
  isRowActive?: (row: T) => boolean;
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
        <thead>
          <tr>
            {columns.map((column) => (
              <th
                key={column.id}
                className={`th whitespace-nowrap ${column.className ?? ""}`}
              >
                {column.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {loading ? (
            Array.from({ length: skeletonRows }, (_, index) => (
              <tr key={index}>
                <td colSpan={columns.length} className="td">
                  <div className="h-3 w-4/5 animate-pulse bg-border-subtle" />
                </td>
              </tr>
            ))
          ) : rows.length ? (
            rows.map((row) => {
              const active = isRowActive?.(row) ?? false;
              return <tr
                key={rowKey(row)}
                className={`${active ? "bg-accent-bg" : ""} ${onRowClick ? "cursor-pointer" : ""} row-hover`}
                style={active ? { boxShadow: "inset 2px 0 0 var(--accent)" } : undefined}
                onClick={onRowClick ? () => onRowClick(row) : undefined}
                onKeyDown={(event) => activate(row, event)}
                tabIndex={onRowClick ? 0 : undefined}
                role={onRowClick ? "link" : undefined}
              >
                {renderRow(row)}
              </tr>;
            })
          ) : (
            <tr>
              <td colSpan={columns.length} className="td p-3">
                {emptyState}
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}
