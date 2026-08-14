import "server-only";

export const paydFetch = {
  retries: 0,
  async request(url: URL, init: RequestInit, timeoutMs: number): Promise<Response> {
    // BFF-020: never follow a redirect that would issue a second POST.
    const response = await fetch(url, { ...init, cache: "no-store", redirect: "manual", signal: AbortSignal.timeout(timeoutMs) });
    if (response.status >= 300 && response.status < 400) throw new Error("payd redirect");
    return response;
  },
} as const;
