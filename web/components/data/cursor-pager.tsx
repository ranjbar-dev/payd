"use client";

import { ChevronLeft, ChevronRight } from "lucide-react";

export function CursorPager({
  nextCursor,
  hasResults,
  limit = 50,
  onNext,
  onStart,
  onLimitChange,
}: Readonly<{
  nextCursor?: string | null;
  hasResults: boolean;
  limit?: 50 | 200;
  onNext: (cursor: string) => void;
  onStart: () => void;
  onLimitChange?: (limit: 50 | 200) => void;
}>) {
  return (
    <nav
      className="flex flex-wrap items-center gap-2 text-sm"
      aria-label="Cursor pagination"
    >
      <button
        type="button"
        className="btn btn-secondary"
        onClick={onStart}
      >
        <ChevronLeft aria-hidden="true" size={14} strokeWidth={1.75} />
        Prev
      </button>
      {onLimitChange ? (
        <label className="field ml-auto">
          Rows{" "}
          <select
            className="input w-auto"
            value={limit}
            onChange={(event) =>
              onLimitChange(event.currentTarget.value === "200" ? 200 : 50)
            }
          >
            <option value="50">50</option>
            <option value="200">200</option>
          </select>
        </label>
      ) : null}
      {nextCursor ? (
        <button
          type="button"
          className="btn btn-secondary"
          onClick={() => onNext(nextCursor)}
        >
          Next
          <ChevronRight aria-hidden="true" size={14} strokeWidth={1.75} />
        </button>
      ) : (
        <span className="text-ink-faint">
          {hasResults ? "End of results" : "No results"}
        </span>
      )}
    </nav>
  );
}
