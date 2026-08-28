import { QueryCache, QueryClient, type QueryFunction, type QueryKey, type UseQueryOptions } from "@tanstack/react-query";

import { isPaydError } from "@/lib/payd/browser-client";

export type QueryTier = "A" | "B" | "C" | "D";

type LivePolling = { tier: "A"; entity: "detail"; isLive: () => boolean; withdrawal?: boolean };
type StandardPolling = { tier?: "B" | "C" | "D" };
export type Polling = LivePolling | StandardPolling;

let rateLimitedUntil = 0;
const rateLimitListeners = new Set<() => void>();

function notifyRateLimit(): void {
  rateLimitListeners.forEach((listener) => listener());
}

function pollingInterval(tier: QueryTier, withdrawal: boolean): number | false {
  if (tier === "A") return withdrawal ? 10_000 : 5_000;
  if (tier === "B") return 30_000;
  if (tier === "C") return 60_000;
  return false;
}

function isPollingAvailable(): boolean {
  return typeof document === "undefined" || (document.visibilityState === "visible" && navigator.onLine);
}

function setRateLimit(): void {
  rateLimitedUntil = Math.max(rateLimitedUntil, Date.now() + 120_000);
  notifyRateLimit();
}

export function getRateLimitUntil(): number | null {
  if (!rateLimitedUntil || rateLimitedUntil <= Date.now()) {
    rateLimitedUntil = 0;
    return null;
  }
  return rateLimitedUntil;
}

export function subscribeRateLimit(listener: () => void): () => void {
  rateLimitListeners.add(listener);
  return () => rateLimitListeners.delete(listener);
}

export function refetchIntervalFor(polling: Polling = {}): number | false {
  const tier = polling.tier ?? "D";
  if (tier === "D") return false;
  if (!isPollingAvailable()) return false;
  if (getRateLimitUntil()) return 60_000;
  if (polling.tier === "A") return polling.isLive() ? pollingInterval("A", Boolean(polling.withdrawal)) : false;
  return pollingInterval(tier, false);
}

type PaydQueryOptions<TData, TKey extends QueryKey> = Omit<UseQueryOptions<TData, Error, TData, TKey>, "queryFn" | "queryKey" | "refetchInterval" | "refetchIntervalInBackground"> & {
  queryFn: QueryFunction<TData, TKey>;
  queryKey: TKey;
  polling?: Polling;
};

export function paydQueryOptions<TData, TKey extends QueryKey>({ polling, ...options }: PaydQueryOptions<TData, TKey>) {
  return {
    ...options,
    refetchInterval: () => refetchIntervalFor(polling),
    refetchIntervalInBackground: false,
  };
}

export function createPaydQueryClient(): QueryClient {
  return new QueryClient({
    queryCache: new QueryCache({
      onError: (error) => {
        if (isPaydError(error) && error.code === "rate_limited") setRateLimit();
      },
    }),
    defaultOptions: {
      mutations: { retry: false },
      queries: {
        retry: false,
        refetchInterval: false,
        refetchIntervalInBackground: false,
        refetchOnReconnect: "always",
        refetchOnWindowFocus: "always",
      },
    },
  });
}
