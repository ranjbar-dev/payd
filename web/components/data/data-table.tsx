"use client";

import { SlidersHorizontal, X } from "lucide-react";
import { Children, useId, useState, type KeyboardEvent, type ReactNode } from "react";

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
  const [expanded, setExpanded] = useState(false);
  const filtersId = useId();
  const collapsible = Children.count(children) > 3;
  return (
    <div>
      {collapsible ? <button type="button" className="btn btn-secondary mb-3 lg:hidden" aria-expanded={expanded} aria-controls={filtersId} onClick={() => setExpanded(!expanded)}><SlidersHorizontal aria-hidden="true" size={14} strokeWidth={1.75} />{expanded ? "Hide filters" : "Show filters"}{active ? " · applied" : ""}</button> : null}
    <div id={filtersId} className="table-filters" data-collapsed={collapsible && !expanded}>
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
    if (event.target === event.currentTarget && onRowClick && (event.key === "Enter" || event.key === " ")) {
      event.preventDefault();
      onRowClick(row);
    }
  };
  return (
    <div className="table-scroll" role="region" aria-label={caption} tabIndex={0} aria-busy={loading}>
      <table
        className="w-full min-w-max border-collapse text-left text-[13px]"
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
                scope="col"
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
                {columns.map((column) => <td key={column.id} className="td"><div className="my-1 h-3 w-4/5 min-w-12 animate-pulse rounded-sm bg-border-subtle" /></td>)}
              </tr>
            ))
          ) : rows.length ? (
            rows.map((row) => {
              const active = isRowActive?.(row) ?? false;
              return <tr
                key={rowKey(row)}
                className={`${active ? "bg-accent-bg" : ""} ${onRowClick ? "cursor-pointer" : ""} row-hover`}
                style={active ? { boxShadow: "inset 2px 0 0 var(--accent)" } : undefined}
                onClick={onRowClick ? (event) => { if (!(event.target as HTMLElement).closest("a, button, input, select, textarea, summary")) onRowClick(row); } : undefined}
                onKeyDown={(event) => activate(row, event)}
                tabIndex={onRowClick ? 0 : undefined}
                aria-label={onRowClick ? `Open ${rowKey(row)}` : undefined}
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
