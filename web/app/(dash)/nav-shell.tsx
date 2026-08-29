import { SessionExpiryNotice } from "../session-expiry";

import { AlarmNavigation } from "./alarm-navigation";
import { NavLinks } from "./nav-links";

export function NavShell({ children, scopeBanner }: Readonly<{ children: React.ReactNode; scopeBanner: React.ReactNode }>) {
  return (
    <div className="min-h-screen bg-canvas pl-60">
      <a
        href="#main-content"
        className="sr-only rounded-sm focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-50 focus:border focus:border-[var(--focus-ring)] focus:bg-panel focus:px-3 focus:py-2 focus:text-sm focus:text-ink"
      >
        Skip to main content
      </a>
      <aside className="fixed inset-y-0 left-0 flex w-60 flex-col border-r border-border-subtle bg-panel">
        <div className="border-b border-border-subtle px-5 py-4">
          <p className="font-mono text-xs font-semibold uppercase tracking-[0.18em] text-ink-secondary">payd</p>
          <p className="mt-1 text-xs text-ink-faint">Operations console</p>
        </div>
        <nav aria-label="Dashboard" className="px-3 py-3">
          <NavLinks />
        </nav>
        <AlarmNavigation />
      </aside>
      <main id="main-content" tabIndex={-1} className="min-h-screen focus:outline-none"><SessionExpiryNotice />{scopeBanner}{children}</main>
    </div>
  );
}
