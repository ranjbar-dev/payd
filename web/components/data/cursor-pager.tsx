"use client";

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
        className="border border-border-strong px-3 py-1.5 hover:bg-raised"
        onClick={onStart}
      >
        Back to start
      </button>
      {onLimitChange ? (
        <label className="ml-auto text-ink-secondary">
          Rows{" "}
          <select
            className="ml-1 border border-border-strong bg-panel px-2 py-1"
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
          className="border border-border-strong px-3 py-1.5 hover:bg-raised"
          onClick={() => onNext(nextCursor)}
        >
          Load more
        </button>
      ) : (
        <span className="text-ink-faint">
          {hasResults ? "End of results" : "No results"}
        </span>
      )}
    </nav>
  );
}
