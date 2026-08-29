"use client";

import { useQueryClient } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { useState } from "react";

/**
 * Manual data reload for a page or modal header. Invalidates every active query
 * so the visible screen refetches from payd without a full-page reload
 * (DAT-035: last good data stays until the refetch resolves). Reads only — this
 * is not a retry of any mutation or fund-moving action (DAT-034 / WDR-000).
 */
export function RefreshButton({ className = "btn btn-secondary" }: Readonly<{ className?: string }>) {
  const client = useQueryClient();
  const [busy, setBusy] = useState(false);
  return (
    <button
      type="button"
      className={className}
      disabled={busy}
      aria-label="Reload data from payd"
      onClick={() => {
        setBusy(true);
        void client.invalidateQueries().finally(() => setBusy(false));
      }}
    >
      <RefreshCw aria-hidden="true" size={14} strokeWidth={1.75} className={busy ? "animate-spin" : undefined} />
      {busy ? "Reloading…" : "Refresh"}
    </button>
  );
}
