"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { ChevronRight } from "lucide-react";

const labels: Record<string, string> = {
  orders: "Orders", payments: "Payments", addresses: "Addresses",
  withdrawals: "Withdrawals", resources: "Resources", webhooks: "Webhooks",
  reports: "Reports", system: "System", new: "Create", events: "Events",
  "funded-terminal": "Funded terminal", unattributed: "Unattributed",
  orphaned: "Orphaned", "needs-resources": "Needs resources",
  "needs-operator": "Needs operator", fees: "Fees", components: "Components",
};

/** UI-035/UI-076: retain a way back from details, including missing entities. */
export function PageBreadcrumbs() {
  const segments = usePathname().split("/").filter(Boolean);
  if (segments.length < 2) return null;
  return <nav aria-label="Breadcrumb" className="mx-auto max-w-7xl px-4 pt-4 lg:px-6">
    <ol className="flex flex-wrap items-center gap-2 text-xs text-ink-secondary">
      {segments.map((segment, index) => {
        const current = index === segments.length - 1;
        const label = labels[segment] ?? "Detail";
        return <li key={index} className="flex items-center gap-2">
          {index > 0 ? <ChevronRight aria-hidden="true" size={14} strokeWidth={1.75} /> : null}
          {current ? <span aria-current="page" className="text-ink">{label}</span> : <Link className="inline-flex min-h-8 items-center rounded underline underline-offset-4 transition-colors hover:text-accent" href={`/${segments.slice(0, index + 1).join("/")}`}>{label}</Link>}
        </li>;
      })}
    </ol>
  </nav>;
}
