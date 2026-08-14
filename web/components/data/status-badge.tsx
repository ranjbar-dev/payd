import { AlertOctagon, AlertTriangle } from "lucide-react";

type Severity =
  | "neutral"
  | "progress"
  | "success"
  | "muted"
  | "warning"
  | "critical";

const STATUS: Record<string, Severity> = {
  pending: "neutral",
  seen: "neutral",
  free: "neutral",
  assigned: "neutral",
  cooling: "neutral",
  requested: "neutral",
  partial: "progress",
  awaiting_resources: "progress",
  awaiting_energy: "progress",
  signing: "progress",
  broadcast: "progress",
  quoted: "progress",
  purchased: "progress",
  paid: "success",
  confirmed: "success",
  delivered: "success",
  delegated: "success",
  expired: "muted",
  cancelled: "muted",
  disabled: "muted",
  expired_funded: "warning",
  cancelled_funded: "warning",
  unattributed: "warning",
  orphaned: "warning",
  dead: "warning",
  rejected: "warning",
  failed: "warning",
  needs_operator: "critical",
  drift_detected: "critical",
};

const STATUS_TITLES: Record<string, string> = {
  rejected: "Rejected: no on-chain attempt was made.",
  failed: "Failed: an on-chain attempt was made and confirmed absent.",
};

export function StatusBadge({
  status,
  resolution,
}: Readonly<{ status: string; resolution?: string | null }>) {
  const resolvedFunded =
    (status === "expired_funded" || status === "cancelled_funded") &&
    resolution != null;
  const severity = resolvedFunded ? "muted" : (STATUS[status] ?? "neutral");
  const Icon =
    severity === "critical"
      ? AlertOctagon
      : severity === "warning"
        ? AlertTriangle
        : null;

  return (
    <span
      className={`status-badge ${status === "needs_operator" ? "needs-operator" : ""}`}
      data-severity={severity}
      aria-label={`${status}, ${severity} status`}
      title={STATUS_TITLES[status]}
    >
      {Icon ? <Icon aria-hidden="true" size={13} strokeWidth={2.5} /> : null}
      {status}
    </span>
  );
}
