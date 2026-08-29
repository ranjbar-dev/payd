"use client";

import type { LucideIcon } from "lucide-react";
import {
  ArrowDownToLine,
  Banknote,
  FileBarChart,
  LayoutDashboard,
  Receipt,
  Server,
  Wallet,
  Webhook,
  Zap,
} from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";

const pages: ReadonlyArray<{ label: string; href: string; icon: LucideIcon }> = [
  { label: "Overview", href: "/", icon: LayoutDashboard },
  { label: "Orders", href: "/orders", icon: Receipt },
  { label: "Payments", href: "/payments", icon: ArrowDownToLine },
  { label: "Addresses", href: "/addresses", icon: Wallet },
  { label: "Withdrawals", href: "/withdrawals", icon: Banknote },
  { label: "Resources", href: "/resources", icon: Zap },
  { label: "Webhooks", href: "/webhooks", icon: Webhook },
  { label: "Reports", href: "/reports", icon: FileBarChart },
  { label: "System", href: "/system", icon: Server },
];

export function NavLinks() {
  const pathname = usePathname();

  return (
    <ul className="space-y-1">
      {pages.map(({ label, href, icon: Icon }) => {
        const active = href === "/" ? pathname === href : pathname === href || pathname.startsWith(`${href}/`);
        return (
          <li key={href}>
            <Link
              href={href}
              aria-current={active ? "page" : undefined}
              className={`flex items-center gap-2 rounded px-3 py-1.5 text-[13px] transition-colors duration-150 focus-visible:outline-offset-[-2px] ${active ? "bg-accent-bg text-ink shadow-[inset_2px_0_0_var(--accent)]" : "text-ink-secondary hover:bg-raised hover:text-ink"}`}
            >
              <Icon aria-hidden="true" size={16} strokeWidth={1.75} />
              {label}
            </Link>
          </li>
        );
      })}
    </ul>
  );
}
