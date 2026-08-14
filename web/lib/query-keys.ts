export type QueryFilters = Readonly<Record<string, boolean | number | string | null | undefined>>;

function filters(value: QueryFilters = {}): QueryFilters {
  return Object.fromEntries(Object.entries(value).filter(([, item]) => item !== undefined).sort(([left], [right]) => left.localeCompare(right)));
}

function resource(name: string) {
  return {
    all: [name] as const,
    detail: (id: string) => [name, "detail", id] as const,
    list: (value?: QueryFilters) => [name, "list", filters(value)] as const,
  };
}

export const queryKeys = {
  alarms: (name: string, value?: QueryFilters) => ["alarms", name, filters({ ...value, limit: 1 })] as const,
  assets: () => ["assets"] as const,
  audit: (value?: QueryFilters) => ["audit", filters(value)] as const,
  chain: {
    params: () => ["chain", "params"] as const,
    quota: () => ["chain", "quota"] as const,
    status: () => ["chain", "status"] as const,
  },
  config: () => ["config"] as const,
  energy: {
    purchases: (value?: QueryFilters) => ["energy", "purchases", filters(value)] as const,
    status: () => ["energy", "status"] as const,
  },
  ipn: {
    consumers: (value?: QueryFilters) => ["ipn", "consumers", filters(value)] as const,
    dead: (value?: QueryFilters) => ["ipn", "dead", filters(value)] as const,
  },
  orders: {
    ...resource("orders"),
    fundedTerminalAll: () => ["orders", "funded-terminal"] as const,
    fundedTerminal: (value?: QueryFilters) => ["orders", "funded-terminal", filters(value)] as const,
  },
  payments: {
    ...resource("payments"),
    unattributedAll: () => ["payments", "unattributed"] as const,
    unattributed: (value?: QueryFilters) => ["payments", "unattributed", filters(value)] as const,
    orphanedAll: () => ["payments", "orphaned"] as const,
    orphaned: (value?: QueryFilters) => ["payments", "orphaned", filters(value)] as const,
  },
  prices: (value?: QueryFilters) => ["prices", filters(value)] as const,
  reports: (name: "fees" | "volume", value?: QueryFilters) => ["reports", name, filters(value)] as const,
  readiness: () => ["readyz"] as const,
  resources: {
    grants: (value?: QueryFilters) => ["resources", "grants", filters(value)] as const,
    wallets: (value?: QueryFilters) => ["resources", "wallets", filters(value)] as const,
  },
  stats: () => ["stats"] as const,
  wallets: {
    ...resource("wallets"),
    withBalance: (value?: QueryFilters) => ["wallets", "with-balance", filters(value)] as const,
    withBalanceAll: () => ["wallets", "with-balance", "all"] as const,
    pooledAll: () => ["wallets", "pooled", "all"] as const,
    needsResources: () => ["wallets", "needs-resources"] as const,
  },
  whoami: () => ["whoami"] as const,
  withdrawals: {
    ...resource("withdrawals"),
    limits: () => ["withdrawals", "limits"] as const,
    needsOperator: () => ["withdrawals", "needs-operator"] as const,
  },
  workers: (value?: QueryFilters) => ["workers", filters(value)] as const,
} as const;
