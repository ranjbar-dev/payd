import Link from "next/link";

import { SessionExpiryNotice } from "../session-expiry";

import { AlarmNavigation } from "./alarm-navigation";

const pages = [
  ["Overview", "/"],
  ["Orders", "/orders"],
  ["Payments", "/payments"],
  ["Addresses", "/addresses"],
  ["Withdrawals", "/withdrawals"],
  ["Resources", "/resources"],
  ["Webhooks", "/webhooks"],
  ["Reports", "/reports"],
  ["System", "/system"],
] as const;

export function NavShell({ children, scopeBanner }: Readonly<{ children: React.ReactNode; scopeBanner: React.ReactNode }>) {
  return (
    <div className="min-h-screen bg-canvas pl-72">
      <aside className="fixed inset-y-0 left-0 flex w-72 flex-col border-r border-border-subtle bg-panel">
        <div className="border-b border-border-subtle px-5 py-4">
          <p className="font-mono text-xs font-semibold uppercase tracking-[0.18em] text-ink-secondary">payd</p>
          <p className="mt-1 text-xs text-ink-faint">Operations console</p>
        </div>
        <nav aria-label="Dashboard" className="px-3 py-3">
          <ul className="space-y-1">
            {pages.map(([label, href]) => (
              <li key={href}>
                <Link className="block rounded-sm px-3 py-2 text-sm text-ink-secondary hover:bg-raised hover:text-ink focus-visible:outline-offset-[-2px]" href={href}>{label}</Link>
              </li>
            ))}
          </ul>
        </nav>
        <AlarmNavigation />
      </aside>
      <main className="min-h-screen"><SessionExpiryNotice />{scopeBanner}{children}</main>
    </div>
  );
}
