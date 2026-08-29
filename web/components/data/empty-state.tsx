import { Inbox } from "lucide-react";
import type { ReactNode } from "react";

export function EmptyState({
  kind,
  title,
  description,
  icon = <Inbox aria-hidden="true" size={20} strokeWidth={1.75} />,
}: Readonly<{
  kind: "worklist" | "search";
  title: string;
  description: string;
  icon?: ReactNode;
}>) {
  return (
    <div
      className="border border-border-subtle bg-panel px-4 py-6 text-center"
      role="status"
    >
      <div className="mb-2 flex justify-center text-ink-faint">{icon}</div>
      <p className="font-medium text-ink">{title}</p>
      <p className="mt-1 text-sm text-ink-secondary">{description}</p>
    </div>
  );
}
