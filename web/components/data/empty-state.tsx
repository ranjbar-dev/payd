import { CheckCircle2, Search } from "lucide-react";

export function EmptyState({
  kind,
  title,
  description,
}: Readonly<{
  kind: "worklist" | "search";
  title: string;
  description: string;
}>) {
  const Icon = kind === "worklist" ? CheckCircle2 : Search;
  return (
    <div
      className={`border px-4 py-6 text-center ${kind === "worklist" ? "border-severity-success bg-[var(--severity-success-bg)]" : "border-border-subtle bg-panel"}`}
      role="status"
    >
      <Icon aria-hidden="true" className="mx-auto mb-2" size={20} />
      <p className="font-medium text-ink">{title}</p>
      <p className="mt-1 text-sm text-ink-secondary">{description}</p>
    </div>
  );
}
