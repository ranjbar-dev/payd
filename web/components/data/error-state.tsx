"use client";

import type { ReactNode } from "react";
import { AlertTriangle } from "lucide-react";

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
        <div className="flex items-center gap-2">
          <AlertTriangle aria-hidden="true" size={20} strokeWidth={1.75} className="text-severity-critical" />
          <p className="font-medium text-ink">
            {copyByCode[error.code] ?? "An unrecognised error was returned."}
          </p>
        </div>
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
            className="btn btn-secondary mt-3"
            onClick={onRetry}
          >
            Retry read
          </button>
        ) : null}
      </div>
    </div>
  );
}
