import "server-only";

export const paydFetch = {
  retries: 0,
  request(url: URL, init: RequestInit, timeoutMs: number): Promise<Response> {
    return fetch(url, { ...init, cache: "no-store", signal: AbortSignal.timeout(timeoutMs) });
  },
} as const;
