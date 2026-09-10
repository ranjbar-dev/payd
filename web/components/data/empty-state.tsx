import { CheckCircle2, Inbox } from "lucide-react";
import type { ReactNode } from "react";

export function EmptyState({
  kind,
  title,
  description,
  icon,
}: Readonly<{
  kind: "worklist" | "search";
  title: string;
  description: string;
  icon?: ReactNode;
}>) {
  return (
    <div
      className="empty-state bg-panel px-4 py-8 text-center"
      role="status"
    >
      <div className={`mb-3 flex justify-center ${kind === "worklist" ? "text-severity-success" : "text-ink-secondary"}`}>{icon ?? (kind === "worklist" ? <CheckCircle2 aria-hidden="true" size={20} strokeWidth={1.75} /> : <Inbox aria-hidden="true" size={20} strokeWidth={1.75} />)}</div>
      <p className="font-medium text-ink">{title}</p>
      <p className="mx-auto mt-2 max-w-prose text-sm leading-relaxed text-ink-secondary">{description}</p>
    </div>
  );
}
