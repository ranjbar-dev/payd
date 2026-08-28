"use client";

import type { ReactNode } from "react";

export type PaydError = { code: string; message?: string; details?: unknown };

export function ErrorState({
  error,
  copyByCode,
  lastUpdatedAt,
  pollingIntervalMs,
  onRetry,
  children,
}: Readonly<{
  error: PaydError;
  copyByCode: Record<string, string>;
  lastUpdatedAt?: number;
  pollingIntervalMs?: number;
  onRetry?: () => void;
  children?: ReactNode;
}>) {
  const stale =
    lastUpdatedAt != null &&
    pollingIntervalMs != null &&
    Date.now() - lastUpdatedAt > pollingIntervalMs * 3;
  return (
    <div className="space-y-3">
      {children}
      {stale ? (
        <p className="text-sm text-severity-warning" role="status">
          Showing stale data; last updated{" "}
          {Math.floor((Date.now() - lastUpdatedAt) / 60000)}m ago.
        </p>
      ) : null}
      <div
        className="border border-severity-warning bg-[var(--severity-warning-bg)] p-3"
        role="alert"
      >
        <p className="font-medium">
          {copyByCode[error.code] ?? "An unrecognised error was returned."}
        </p>
        <p className="mt-1 text-sm text-ink-secondary">
          Error code:{" "}
          <code className="select-all font-mono text-ink">{error.code}</code>
        </p>
        {error.details != null ? (
          <pre className="mt-2 overflow-auto border-t border-border-subtle pt-2 text-xs text-ink-secondary">
            {JSON.stringify(error.details, null, 2)}
          </pre>
        ) : null}
        {onRetry ? (
          <button
            type="button"
            className="mt-3 border border-border-strong px-3 py-1.5 text-sm hover:bg-raised"
            onClick={onRetry}
          >
            Retry read
          </button>
        ) : null}
      </div>
    </div>
  );
}
