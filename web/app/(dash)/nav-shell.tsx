import { SessionExpiryNotice } from "../session-expiry";
import { ChevronDown, Menu } from "lucide-react";

import { AlarmNavigation } from "./alarm-navigation";
import { NavLinks } from "./nav-links";
import { PageBreadcrumbs } from "./page-breadcrumbs";

export function NavShell({ children, scopeBanner }: Readonly<{ children: React.ReactNode; scopeBanner: React.ReactNode }>) {
  return (
    <div className="min-h-screen bg-canvas lg:pl-60">
      <a
        href="#main-content"
        className="sr-only rounded-sm focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-50 focus:border focus:border-[var(--focus-ring)] focus:bg-panel focus:px-3 focus:py-2 focus:text-sm focus:text-ink"
      >
        Skip to main content
      </a>
      <aside className="dashboard-sidebar flex flex-col border-b border-border-subtle bg-panel lg:fixed lg:inset-y-0 lg:left-0 lg:w-60 lg:overflow-y-auto lg:border-r lg:border-b-0">
        <div className="border-b border-border-subtle px-5 py-4">
          <p className="text-[22px] font-semibold tracking-tight text-ink">payd<span className="text-accent">.</span></p>
          <p className="mt-1 text-xs text-ink-faint">Operations console</p>
        </div>
        <nav aria-label="Dashboard" className="dashboard-nav hidden px-3 py-3 lg:block">
          <NavLinks />
        </nav>
        <details className="mobile-navigation lg:hidden">
          <summary className="flex min-h-11 items-center gap-2 px-4 text-ink-secondary transition-colors hover:bg-raised hover:text-ink"><Menu aria-hidden="true" size={16} strokeWidth={1.75} />Navigation<ChevronDown aria-hidden="true" size={14} className="ml-auto" /></summary>
          <nav aria-label="Dashboard" className="dashboard-nav px-3 pb-3"><NavLinks /></nav>
        </details>
        <AlarmNavigation />
      </aside>
      <div id="main-content" tabIndex={-1} className="min-h-screen min-w-0 focus:outline-none"><SessionExpiryNotice />{scopeBanner}<PageBreadcrumbs />{children}</div>
    </div>
  );
}
