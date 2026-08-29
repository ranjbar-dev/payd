import { AlertOctagon, AlertTriangle } from "lucide-react";

export function AlarmCounter({
  label,
  count,
  severity = "warning",
}: Readonly<{
  label: string;
  count: number;
  severity?: "warning" | "critical";
}>) {
  const Icon = severity === "critical" ? AlertOctagon : AlertTriangle;
  return (
    <div
      className="alarm-counter flex items-center justify-between border border-border-subtle bg-panel px-3 py-2"
      data-severity={severity}
      data-count={count}
      aria-label={`${label}: ${count}`}
    >
      <span className="flex items-center gap-2">
        <Icon aria-hidden="true" size={15} strokeWidth={1.75} />
        {label}
      </span>
      <strong className="font-mono tabular-nums">{count}</strong>
    </div>
  );
}
