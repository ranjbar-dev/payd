import { getEnv } from "@/lib/env";
import { findAllowedRoute, upstreamPath } from "@/lib/payd/allowlist";
import { paydFetch } from "@/lib/payd/client";
import { setSessionWhoami, verifyCsrf, verifySession } from "@/lib/session";

type Context = { params: Promise<{ path: string[] }> };

const csrfCookie = "payd_csrf";
const sessionCookie = "payd_session";

function cookie(request: Request, name: string): string | undefined {
  return request.headers.get("cookie")?.split(";").map((item) => item.trim()).find((item) => item.startsWith(`${name}=`))?.slice(name.length + 1);
}

function error(status: number, code: string, message: string, details?: Record<string, unknown>): Response {
  return Response.json({ error: { code, message, ...(details ? { details } : {}) } }, { status, headers: { "Cache-Control": "no-store" } });
}

function responseFrom(upstream: Response): Response {
  const headers = new Headers({ "Cache-Control": "no-store" });
  for (const header of ["content-type", "content-disposition"]) {
    const value = upstream.headers.get(header);
    if (value) headers.set(header, value);
  }
  return new Response(upstream.body, { status: upstream.status, headers });
}

async function mutationBody(request: Request): Promise<{ body?: string; totp?: string } | null> {
  const raw = (await request.text()).trim();
  // A parameterless action (POST /wallets/{address}/disable, POST /ipn/{id}/retry)
  // legitimately sends no body. Forward it as-is; only a present-but-malformed
  // body is a client error.
  if (raw === "") return {};
  let value: unknown;
  try { value = JSON.parse(raw); } catch { return null; }
  if (typeof value !== "object" || value === null || Array.isArray(value)) return null;
  const body = { ...(value as Record<string, unknown>) };
  const totp = body.totp;
  delete body.totp;
  if (totp !== undefined && (typeof totp !== "string" || !/^\d{6}$/.test(totp))) return null;
  return { body: JSON.stringify(body), ...(typeof totp === "string" ? { totp } : {}) };
}

function safePath(path: string[]): boolean {
  return path.length > 0 && path.every((segment) => segment !== "" && segment !== "." && segment !== ".." && !segment.includes("\\"));
}

function audit(method: string, path: string[], outcome: string): void {
  if (method === "POST") console.info(JSON.stringify({ timestamp: new Date().toISOString(), action: "proxy_mutation", target: path.join("/"), outcome }));
}

export async function proxyPaydRequest(request: Request, { params }: Context): Promise<Response> {
  const { path } = await params;
  const method = request.method.toUpperCase();
  const session = verifySession(cookie(request, sessionCookie));
  if (!session) return error(401, "unauthorized", "Unauthorized");
  const route = safePath(path) ? findAllowedRoute(method, path) : undefined;
  if (!route) return error(404, "not_found", "Not found");
  if (method !== "GET" && method !== "POST") return error(405, "method_not_allowed", "Method not allowed");
  if (method === "POST" && !verifyCsrf(session, cookie(request, csrfCookie), request.headers.get("x-csrf-token"))) {
    audit(method, path, "csrf_rejected");
    return error(403, "csrf_rejected", "Forbidden");
  }

  let body: string | undefined;
  let totp: string | undefined;
  if (method === "POST") {
    const mutation = await mutationBody(request);
    if (!mutation) {
      audit(method, path, "invalid_json");
      return error(400, "invalid_request", "Invalid JSON request body");
    }
    body = mutation.body;
    totp = mutation.totp;
  }

  const url = new URL(upstreamPath(route, path), getEnv().PAYD_BASE_URL);
  url.search = new URL(request.url).search;
  // Public routes take no key and are not rate limited; sending one anyway would
  // put /readyz on the same budget as everything else for no benefit.
  const headers = new Headers(route.public ? {} : { "X-API-Key": getEnv().PAYD_API_KEY });
  if (body !== undefined) headers.set("content-type", "application/json");
  const idempotencyKey = request.headers.get("idempotency-key");
  if (method === "POST" && idempotencyKey) headers.set("Idempotency-Key", idempotencyKey);
  if (totp) headers.set("X-TOTP", totp);

  try {
    // WRPT-033: a 100,000-row CSV export streams for longer than the ordinary request
    // budget; AbortSignal.timeout aborts an in-progress stream, not just connection
    // setup, so this route gets its own ceiling rather than truncating a large export.
    const timeoutMs = path[0] === "export" ? 120_000 : method === "POST" && path.length === 1 && path[0] === "withdrawals" ? 30_000 : 15_000;
    const upstream = await paydFetch.request(url, { method, headers, ...(body === undefined ? {} : { body }) }, timeoutMs);
    if (method === "GET" && path.length === 2 && path[0] === "auth" && path[1] === "whoami" && upstream.ok) {
      try {
        const identity: unknown = await upstream.clone().json();
        if (typeof identity === "object" && identity !== null) {
          const item = identity as Record<string, unknown>;
          if (typeof item.key_name === "string" && Array.isArray(item.scopes) && item.scopes.every((scope) => typeof scope === "string")) {
            setSessionWhoami(session.id, item.key_name, item.scopes);
          }
        }
      } catch { /* Preserve payd's response untouched. */ }
    }
    audit(method, path, String(upstream.status));
    return responseFrom(upstream);
  } catch (reason) {
    const timeout = reason instanceof DOMException && reason.name === "TimeoutError";
    audit(method, path, timeout ? "timeout" : "unreachable");
    if (timeout && method === "POST") return error(504, "upstream_timeout", "Upstream timed out", { outcome_unknown: true });
    return error(502, "upstream_unreachable", "Upstream unavailable");
  }
}

export const GET = proxyPaydRequest;
export const POST = proxyPaydRequest;
