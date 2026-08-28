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

export function ScopeBanner({ whoami }: { whoami: { keyName: string; scopes: string[] } | null }) {
  if (!whoami) return <p className="bg-amber-300 p-3 text-slate-950">Unable to verify payd key scopes.</p>;
  const missing = Object.keys(scopePages).filter((scope) => !whoami.scopes.includes(scope));
  if (missing.length === 0) return null;
  return <p className="bg-amber-300 p-3 text-slate-950">Missing payd scopes: {missing.map((scope) => `${scope} (${scopePages[scope]})`).join(", ")}</p>;
}
