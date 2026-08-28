import { AlertTriangle } from "lucide-react";

const scopePages: Record<string, string> = {
  "orders:read": "Orders, Payments, Webhooks, Reports",
  "orders:write": "Order and payment actions",
  "wallets:read": "Addresses, Resources, Reports, System",
  "wallets:write": "Address actions",
  "withdrawals:read": "Withdrawals and reports",
  "withdrawals:write": "Withdrawal actions",
  "resources:write": "Resource actions",
  "admin:read": "System",
};

function Banner({ children }: { children: React.ReactNode }) {
  return (
    <div
      role="alert"
      className="flex items-start gap-2 border border-severity-warning bg-[var(--severity-warning-bg)] px-4 py-3 text-sm text-severity-warning"
    >
      <AlertTriangle aria-hidden="true" size={16} className="mt-0.5 shrink-0" />
      <div className="text-ink">{children}</div>
    </div>
  );
}

export function ScopeBanner({ whoami }: { whoami: { keyName: string; scopes: string[] } | null }) {
  if (!whoami) return <Banner>Unable to verify payd key scopes. Controls that need a scope stay disabled until the key is confirmed.</Banner>;
  const missing = Object.keys(scopePages).filter((scope) => !whoami.scopes.includes(scope));
  if (missing.length === 0) return null;
  return (
    <Banner>
      <p className="font-medium">The payd key is missing {missing.length === 1 ? "a scope" : `${missing.length} scopes`}.</p>
      <ul className="mt-1 space-y-0.5">
        {missing.map((scope) => (
          <li key={scope}>
            <code className="font-mono text-severity-warning">{scope}</code>
            <span className="text-ink-secondary"> — {scopePages[scope]}</span>
          </li>
        ))}
      </ul>
    </Banner>
  );
}
