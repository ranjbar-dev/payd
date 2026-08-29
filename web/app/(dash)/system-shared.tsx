"use client";

import { ErrorState } from "@/components/data/error-state";
import { Timestamp } from "@/components/data/timestamp";
import { isPaydError } from "@/lib/payd/browser-client";

// Shared by every System tab (Workers/Quota/Config/Assets/Audit/Session/Health)
// so the read-error copy reads identically no matter which tab is showing it.
export const copyByCode: Record<string, string> = {
  unauthorized: "This dashboard session or its upstream scope is not authorised.",
  rate_limited: "Refresh has slowed because payd is rate limited.",
  upstream_unreachable: "payd could not be reached; the last available data, if any, remains visible.",
  upstream_timeout: "payd did not answer in time; the last available data, if any, remains visible.",
};

// INV-1: `ErrorState`'s built-in "Retry read" button is the one retry control
// this page ever shows — a deliberate human click re-issuing a GET, never an
// automatic resend of anything. Every tab wires its own `refetch()` into
// `onReload` below rather than auto-retrying on error.
export function ErrorNotice({
  error,
  updatedAt,
  pollingIntervalMs,
  onReload,
}: Readonly<{
  error: unknown;
  updatedAt?: number;
  pollingIntervalMs?: number;
  onReload: () => void;
}>) {
  if (!error) return null;
  const paydError = isPaydError(error) ? error : null;
  return (
    <ErrorState
      error={{ code: paydError?.code ?? "upstream_unreachable", details: paydError?.details }}
      copyByCode={copyByCode}
      lastUpdatedAt={updatedAt || undefined}
      pollingIntervalMs={pollingIntervalMs}
      onRetry={onReload}
    />
  );
}

// UI-016: backend durations (seconds_since_tick, expected_interval_seconds, the
// config tab's *_seconds fields) render humanised, never as a raw number of
// seconds.
export function Duration({ seconds }: Readonly<{ seconds: number | null | undefined }>) {
  return seconds == null ? <span className="text-ink-faint">—</span> : <Timestamp seconds={seconds} variant="duration" />;
}

// AUTH-032: a UI control (here, a whole tab's content) whose backend route
// requires a scope this payd key lacks renders disabled with the scope named,
// rather than hidden. The tab itself stays visible and selectable in the nav;
// only its content is replaced by this notice, and no query for that tab's data
// is attempted at all.
export function ScopeDisabled({ scope, label }: Readonly<{ scope: string; label: string }>) {
  return (
    <div className="card text-sm text-ink-secondary" role="status">
      <p className="card-title text-ink">{label} is disabled.</p>
      <p className="mt-1">
        This payd key is missing the <code className="font-mono text-ink">{scope}</code> scope, which this tab
        requires. Nothing has been requested from payd for it.
      </p>
    </div>
  );
}

// Duplicated deliberately, not imported: the identical helper already exists as a
// page-local, non-exported function in lib/payd/browser-client.ts,
// app/(dash)/withdrawal-wizard.tsx, and app/(dash)/order-create-form.tsx — this
// codebase's established convention for this exact snippet is a local copy per
// consumer, not a shared export.
export function csrfToken(): string | undefined {
  return document.cookie.split(";").map((item) => item.trim()).find((item) => item.startsWith("payd_csrf="))?.slice("payd_csrf=".length);
}
